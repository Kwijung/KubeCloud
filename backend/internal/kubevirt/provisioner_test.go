package kubevirt

import (
	"context"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type recordingApplier struct {
	resources        []map[string]any
	deletedResources []map[string]any
	patchedResources []map[string]any
	runStrategies    []string
}

func (a *recordingApplier) Apply(_ context.Context, resource map[string]any) error {
	a.resources = append(a.resources, resource)
	return nil
}

func (a *recordingApplier) Delete(_ context.Context, resource map[string]any) error {
	a.deletedResources = append(a.deletedResources, resource)
	return nil
}

func (a *recordingApplier) Patch(_ context.Context, resource map[string]any) error {
	a.patchedResources = append(a.patchedResources, resource)
	return nil
}

func (a *recordingApplier) SetVirtualMachineRunStrategy(_ context.Context, _ string, _ string, runStrategy string) error {
	a.runStrategies = append(a.runStrategies, runStrategy)
	return nil
}

func TestProvisionerAppliesDataVolumeThenVirtualMachine(t *testing.T) {
	applier := &recordingApplier{}
	provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{
			ID:        "platform-web-01",
			ProjectID: "platform",
			Name:      "web-01",
			ImageID:   "platform-cirros",
			FlavorID:  "small",
		},
		Image: domain.Image{
			SourceURL: "https://example.invalid/cirros.img",
		},
		Flavor: domain.Flavor{
			CPU:           2,
			MemoryBytes:   4 * 1024 * 1024 * 1024,
			RootDiskBytes: 20 * 1024 * 1024 * 1024,
		},
	}

	if err := provisioner.CreateVirtualMachine(context.Background(), plan); err != nil {
		t.Fatalf("create virtual machine: %v", err)
	}

	if len(applier.resources) != 2 {
		t.Fatalf("expected two applied resources, got %d", len(applier.resources))
	}
	if applier.resources[0]["kind"] != "DataVolume" {
		t.Fatalf("expected DataVolume first, got %#v", applier.resources[0]["kind"])
	}
	if applier.resources[1]["kind"] != "VirtualMachine" {
		t.Fatalf("expected VirtualMachine second, got %#v", applier.resources[1]["kind"])
	}
}

func TestProvisionerDeletesVirtualMachineThenDataVolume(t *testing.T) {
	applier := &recordingApplier{}
	provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
	vm := domain.VirtualMachine{
		ID:        "platform-portal-02",
		ProjectID: "platform",
		Name:      "portal-02",
		ImageID:   "platform-cirros",
		FlavorID:  "small",
	}

	if err := provisioner.DeleteVirtualMachine(context.Background(), vm); err != nil {
		t.Fatalf("delete virtual machine: %v", err)
	}

	if len(applier.deletedResources) != 2 {
		t.Fatalf("expected two deleted resources, got %d", len(applier.deletedResources))
	}
	if applier.deletedResources[0]["kind"] != "VirtualMachine" {
		t.Fatalf("expected VirtualMachine deleted first, got %#v", applier.deletedResources[0]["kind"])
	}
	if applier.deletedResources[1]["kind"] != "DataVolume" {
		t.Fatalf("expected DataVolume deleted second, got %#v", applier.deletedResources[1]["kind"])
	}
}

func TestProvisionerStartsAndStopsVirtualMachine(t *testing.T) {
	applier := &recordingApplier{}
	provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
	vm := domain.VirtualMachine{ID: "platform-portal-02", ProjectID: "platform"}

	if err := provisioner.StopVirtualMachine(context.Background(), vm); err != nil {
		t.Fatalf("stop virtual machine: %v", err)
	}
	if err := provisioner.StartVirtualMachine(context.Background(), vm); err != nil {
		t.Fatalf("start virtual machine: %v", err)
	}
	if len(applier.runStrategies) != 2 || applier.runStrategies[0] != "Halted" || applier.runStrategies[1] != "Always" {
		t.Fatalf("unexpected run strategies: %#v", applier.runStrategies)
	}
}

func TestProvisionerSetsDNSOnlyForSubnetVMs(t *testing.T) {
	base := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
	}
	templateSpecOf := func(t *testing.T, plan domain.VirtualMachineProvisioningPlan) map[string]any {
		t.Helper()
		applier := &recordingApplier{}
		provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
		provisioner.SetSubnetDNSServers([]string{"10.0.0.2", "8.8.8.8"})
		if err := provisioner.CreateVirtualMachine(context.Background(), plan); err != nil {
			t.Fatalf("create virtual machine: %v", err)
		}
		return applier.resources[1]["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	}

	withSubnet := base
	withSubnet.Subnet = &domain.Subnet{ID: "platform-app-web", NADName: "kloudia-net-platform-app-web"}
	spec := templateSpecOf(t, withSubnet)
	if spec["dnsPolicy"] != "None" {
		t.Fatalf("expected dnsPolicy None for a subnet VM, got %#v", spec["dnsPolicy"])
	}
	nameservers := spec["dnsConfig"].(map[string]any)["nameservers"].([]any)
	if len(nameservers) != 2 || nameservers[0] != "10.0.0.2" || nameservers[1] != "8.8.8.8" {
		t.Fatalf("unexpected nameservers: %#v", nameservers)
	}

	spec = templateSpecOf(t, base)
	if _, ok := spec["dnsPolicy"]; ok {
		t.Fatalf("pod-network VM must keep cluster DNS, got dnsPolicy %#v", spec["dnsPolicy"])
	}
}

func TestProvisionerKeepsClusterDNSWhenNoServersConfigured(t *testing.T) {
	applier := &recordingApplier{}
	provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
		Subnet:         &domain.Subnet{ID: "platform-app-web", NADName: "kloudia-net-platform-app-web"},
	}
	if err := provisioner.CreateVirtualMachine(context.Background(), plan); err != nil {
		t.Fatalf("create virtual machine: %v", err)
	}
	spec := applier.resources[1]["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if _, ok := spec["dnsConfig"]; ok {
		t.Fatalf("expected no dnsConfig without configured servers, got %#v", spec["dnsConfig"])
	}
}

func TestProvisionerUsesConfiguredDiskAccessMode(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
	}
	accessModes := func(t *testing.T, configure func(*Provisioner)) []any {
		t.Helper()
		applier := &recordingApplier{}
		provisioner := NewProvisioner("kloudia-vms", "test-delete", applier)
		configure(provisioner)
		if err := provisioner.CreateVirtualMachine(context.Background(), plan); err != nil {
			t.Fatalf("create virtual machine: %v", err)
		}
		return applier.resources[0]["spec"].(map[string]any)["pvc"].(map[string]any)["accessModes"].([]any)
	}

	if modes := accessModes(t, func(*Provisioner) {}); len(modes) != 1 || modes[0] != "ReadWriteOnce" {
		t.Fatalf("expected the ReadWriteOnce default, got %#v", modes)
	}
	modes := accessModes(t, func(p *Provisioner) { p.SetDiskAccessMode("ReadWriteMany") })
	if len(modes) != 1 || modes[0] != "ReadWriteMany" {
		t.Fatalf("expected ReadWriteMany, got %#v", modes)
	}
}
