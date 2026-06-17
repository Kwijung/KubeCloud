package kubevirt

import (
	"context"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

// kindOf extracts the "kind" string from a resource map.
func kindOf(r map[string]any) string {
	k, _ := r["kind"].(string)
	return k
}

func TestCreateNetworkAppliesVpc(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	network := domain.Network{OVNVPCName: "kloudia-vpc-x"}

	if err := p.CreateNetwork(context.Background(), network); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	if len(applier.resources) != 1 {
		t.Fatalf("expected 1 applied resource, got %d", len(applier.resources))
	}
	if kindOf(applier.resources[0]) != "Vpc" {
		t.Fatalf("expected kind Vpc, got %q", kindOf(applier.resources[0]))
	}
}

func TestCreateSubnetPrivateAppliesSubnetAndNAD(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	subnet := domain.Subnet{
		OVNSubnetName: "kloudia-subnet-x",
		NADName:       "kloudia-net-x",
		CIDR:          "10.0.0.0/24",
		Type:          "private",
	}

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", subnet); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}

	if len(applier.resources) != 2 {
		t.Fatalf("expected 2 applied resources, got %d", len(applier.resources))
	}
	if kindOf(applier.resources[0]) != "Subnet" {
		t.Fatalf("expected Subnet first, got %q", kindOf(applier.resources[0]))
	}
	if kindOf(applier.resources[1]) != "NetworkAttachmentDefinition" {
		t.Fatalf("expected NetworkAttachmentDefinition second, got %q", kindOf(applier.resources[1]))
	}

	egressKinds := map[string]bool{
		"VpcNatGateway":    true,
		"IptablesEIP":      true,
		"IptablesSnatRule": true,
	}
	for i, r := range applier.resources {
		if egressKinds[kindOf(r)] {
			t.Fatalf("unexpected egress resource %q at index %d for private subnet", kindOf(r), i)
		}
	}
}

func TestCreateSubnetPublicAppliesEgress(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	subnet := domain.Subnet{
		OVNSubnetName: "kloudia-subnet-x",
		NADName:       "kloudia-net-x",
		CIDR:          "10.0.0.0/24",
		Type:          "public",
	}

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", subnet); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}

	wantOrder := []string{
		"Subnet",
		"NetworkAttachmentDefinition",
		"VpcNatGateway",
		"IptablesEIP",
		"IptablesSnatRule",
	}
	if len(applier.resources) != len(wantOrder) {
		t.Fatalf("expected %d applied resources, got %d", len(wantOrder), len(applier.resources))
	}
	for i, want := range wantOrder {
		if got := kindOf(applier.resources[i]); got != want {
			t.Fatalf("index %d: expected kind %q, got %q", i, want, got)
		}
	}
}

func TestDeleteSubnetPublicRemovesPerSubnetResourcesNotGateway(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	subnet := domain.Subnet{
		OVNSubnetName: "kloudia-subnet-x",
		NADName:       "kloudia-net-x",
		CIDR:          "10.0.0.0/24",
		Type:          "public",
	}

	if err := p.DeleteSubnet(context.Background(), "kloudia-vpc-x", subnet); err != nil {
		t.Fatalf("DeleteSubnet: %v", err)
	}

	wantOrder := []string{
		"IptablesSnatRule",
		"IptablesEIP",
		"NetworkAttachmentDefinition",
		"Subnet",
	}
	if len(applier.deletedResources) != len(wantOrder) {
		t.Fatalf("expected %d deleted resources, got %d", len(wantOrder), len(applier.deletedResources))
	}
	for i, want := range wantOrder {
		if got := kindOf(applier.deletedResources[i]); got != want {
			t.Fatalf("index %d: expected kind %q, got %q", i, want, got)
		}
	}

	for i, r := range applier.deletedResources {
		if kindOf(r) == "VpcNatGateway" {
			t.Fatalf("VpcNatGateway must NOT be deleted, but found at index %d", i)
		}
	}
}

func TestNetworkProvisionerNilApplierErrors(t *testing.T) {
	p := &NetworkProvisioner{}
	network := domain.Network{OVNVPCName: "kloudia-vpc-x"}

	err := p.CreateNetwork(context.Background(), network)
	if err == nil {
		t.Fatal("expected error from nil applier, got nil")
	}
}
