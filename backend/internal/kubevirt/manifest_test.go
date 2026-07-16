package kubevirt

import (
	"strings"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestBuildResourcesUsesRunStrategyAndDataVolumeSource(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{
			ID:        "platform-web-01",
			ProjectID: "platform",
			Name:      "web-01",
			ImageID:   "platform-cirros",
			FlavorID:  "small",
			Status:    "Provisioning",
		},
		Image: domain.Image{
			ID:        "platform-cirros",
			ProjectID: "platform",
			SourceURL: "https://example.invalid/cirros.img",
		},
		Flavor: domain.Flavor{
			ID:            "small",
			CPU:           1,
			MemoryBytes:   512 * 1024 * 1024,
			RootDiskBytes: 2 * 1024 * 1024 * 1024,
		},
	}

	resources := BuildResources("kloudia-vms", "test-delete", plan)

	if resources.DataVolume["apiVersion"] != "cdi.kubevirt.io/v1beta1" {
		t.Fatalf("expected CDI DataVolume apiVersion, got %#v", resources.DataVolume["apiVersion"])
	}
	dvSpec := resources.DataVolume["spec"].(map[string]any)
	source := dvSpec["source"].(map[string]any)
	httpSource := source["http"].(map[string]any)
	if httpSource["url"] != "https://example.invalid/cirros.img" {
		t.Fatalf("expected image source URL, got %#v", httpSource["url"])
	}
	pvc := dvSpec["pvc"].(map[string]any)
	if pvc["storageClassName"] != "test-delete" {
		t.Fatalf("expected storage class test-delete, got %#v", pvc["storageClassName"])
	}

	vmSpec := resources.VirtualMachine["spec"].(map[string]any)
	if vmSpec["runStrategy"] != "Always" {
		t.Fatalf("expected runStrategy Always, got %#v", vmSpec["runStrategy"])
	}
	template := vmSpec["template"].(map[string]any)
	templateSpec := template["spec"].(map[string]any)
	domainSpec := templateSpec["domain"].(map[string]any)
	cpu := domainSpec["cpu"].(map[string]any)
	if cpu["cores"] != int64(1) {
		t.Fatalf("expected 1 core, got %#v", cpu["cores"])
	}
	resourceRequests := domainSpec["resources"].(map[string]any)
	requests := resourceRequests["requests"].(map[string]any)
	if requests["memory"] != "512Mi" {
		t.Fatalf("expected 512Mi memory, got %#v", requests["memory"])
	}
}

func TestBuildResourcesAddsCloudInitNoCloudWhenProvided(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
		CloudInit: &domain.CloudInitConfig{
			Username:     "cloudia",
			Password:     "cloudia",
			SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey cloudia@test",
		},
	}

	resources := BuildResources("kloudia-vms", "test-delete", plan)

	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	volumes := templateSpec["volumes"].([]any)
	if len(volumes) != 2 {
		t.Fatalf("expected rootdisk and cloudinitdisk volumes, got %#v", volumes)
	}
	cloudInit := volumes[1].(map[string]any)["cloudInitNoCloud"].(map[string]any)
	userData := cloudInit["userData"].(string)
	for _, want := range []string{
		"#cloud-config",
		"name: cloudia",
		"shell: /bin/sh",
		"ssh_pwauth: true",
		"cloudia:cloudia",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey cloudia@test",
	} {
		if !strings.Contains(userData, want) {
			t.Fatalf("expected userData to contain %q, got:\n%s", want, userData)
		}
	}
	disks := templateSpec["domain"].(map[string]any)["devices"].(map[string]any)["disks"].([]any)
	if len(disks) != 2 {
		t.Fatalf("expected rootdisk and cloudinitdisk disks, got %#v", disks)
	}
	if disks[1].(map[string]any)["name"] != "cloudinitdisk" {
		t.Fatalf("expected cloudinitdisk device, got %#v", disks[1])
	}
}

func TestBuildResourcesUsesCirrOSDefaultUserWithoutDuplicateUserBlock(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
		CloudInit: &domain.CloudInitConfig{
			Username: "cirros",
			Password: "gocubsgo",
		},
	}

	resources := BuildResources("kloudia-vms", "test-delete", plan)

	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	volumes := templateSpec["volumes"].([]any)
	cloudInit := volumes[1].(map[string]any)["cloudInitNoCloud"].(map[string]any)
	userData := cloudInit["userData"].(string)
	if strings.Contains(userData, "name: cirros") {
		t.Fatalf("expected CirrOS default user to avoid duplicate user block, got:\n%s", userData)
	}
	if !strings.Contains(userData, "cirros:gocubsgo") {
		t.Fatalf("expected userData to set CirrOS password, got:\n%s", userData)
	}
}

func TestBuildResourcesAddsMultusNICWhenSubnetPresent(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
		Subnet:         &domain.Subnet{ID: "platform-app-web", NADName: "kloudia-net-platform-app-web"},
	}

	resources := BuildResources("kloudia-vms", "test-delete", plan)

	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	networks := templateSpec["networks"].([]any)
	if len(networks) != 1 {
		t.Fatalf("expected exactly one network, got %#v", networks)
	}
	net0 := networks[0].(map[string]any)
	multus := net0["multus"].(map[string]any)
	if multus["networkName"] != "kloudia-vms/kloudia-net-platform-app-web" {
		t.Fatalf("expected multus networkName, got %#v", multus["networkName"])
	}
	ifaces := templateSpec["domain"].(map[string]any)["devices"].(map[string]any)["interfaces"].([]any)
	if len(ifaces) != 1 {
		t.Fatalf("expected one interface, got %#v", ifaces)
	}
	if _, hasBridge := ifaces[0].(map[string]any)["bridge"]; !hasBridge {
		t.Fatalf("expected bridge binding, got %#v", ifaces[0])
	}
}

func TestBuildResourcesOmitsNetworksWhenNoSubnet(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-02", ProjectID: "platform", Name: "web-02"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
	}
	resources := BuildResources("kloudia-vms", "test-delete", plan)
	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if _, hasNetworks := templateSpec["networks"]; hasNetworks {
		t.Fatalf("expected no networks block when subnet is absent")
	}
}
