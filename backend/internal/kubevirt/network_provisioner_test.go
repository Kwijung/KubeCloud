package kubevirt

import (
	"context"
	"strings"
	"testing"
	"time"

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

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", subnet, nil); err != nil {
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

func publicSubnet(id string, cidr string, minute int) domain.Subnet {
	return domain.Subnet{
		ID:            id,
		OVNSubnetName: "kloudia-subnet-" + id,
		NADName:       "kloudia-net-" + id,
		CIDR:          cidr,
		Gateway:       cidr[:len(cidr)-4] + "1",
		Type:          "public",
		CreatedAt:     time.Date(2026, 10, 19, 9, minute, 0, 0, time.UTC),
	}
}

func assertKinds(t *testing.T, what string, got []map[string]any, want []string) {
	t.Helper()
	if len(got) != len(want) {
		var kinds []string
		for _, r := range got {
			kinds = append(kinds, kindOf(r))
		}
		t.Fatalf("%s: expected %v, got %v", what, want, kinds)
	}
	for i, w := range want {
		if k := kindOf(got[i]); k != w {
			t.Fatalf("%s index %d: expected kind %q, got %q", what, i, w, k)
		}
	}
}

// routesOf returns "src-cidr->nextHop" for each egress policy in a Vpc patch, or nil when they are cleared.
func routesOf(t *testing.T, patch map[string]any) []string {
	t.Helper()
	raw := patch["spec"].(map[string]any)["policyRoutes"]
	if raw == nil {
		return nil
	}
	var out []string
	for _, r := range raw.([]any) {
		m := r.(map[string]any)
		if m["action"] != "reroute" {
			t.Fatalf("expected reroute policy, got %#v", m)
		}
		out = append(out, strings.TrimPrefix(m["match"].(string), "ip4.src == ")+"->"+m["nextHopIP"].(string))
	}
	return out
}

func TestCreateSubnetPublicAnchorCreatesGatewayThenRoutes(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	a := publicSubnet("a", "10.0.0.0/24", 0)

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", a, []domain.Subnet{a}); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}

	assertKinds(t, "applied", applier.resources, []string{
		"Subnet", "NetworkAttachmentDefinition", "VpcNatGateway", "IptablesEIP", "IptablesSnatRule",
	})
	assertKinds(t, "patched", applier.patchedResources, []string{"Vpc"})
	if got := routesOf(t, applier.patchedResources[0]); len(got) != 1 || got[0] != "10.0.0.0/24->10.0.0.254" {
		t.Fatalf("unexpected routes: %v", got)
	}
}

func TestCreateSubnetPublicNonAnchorReusesGatewayAndAddsRoute(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	a := publicSubnet("a", "10.0.0.0/24", 0)
	b := publicSubnet("b", "10.0.2.0/24", 1)

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", b, []domain.Subnet{b, a}); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}

	assertKinds(t, "applied", applier.resources, []string{
		"Subnet", "NetworkAttachmentDefinition", "IptablesEIP", "IptablesSnatRule",
	})
	got := routesOf(t, applier.patchedResources[0])
	if len(got) != 2 || got[0] != "10.0.0.0/24->10.0.0.254" || got[1] != "10.0.2.0/24->10.0.0.254" {
		t.Fatalf("expected both public cidrs routed via the anchor's lanIp, got %v", got)
	}
}

func TestCreateSubnetPrivateLeavesRoutesAlone(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	s := domain.Subnet{OVNSubnetName: "kloudia-subnet-p", NADName: "kloudia-net-p", CIDR: "10.0.3.0/24", Type: "private"}

	if err := p.CreateSubnet(context.Background(), "kloudia-vpc-x", s, nil); err != nil {
		t.Fatalf("CreateSubnet: %v", err)
	}
	if len(applier.patchedResources) != 0 {
		t.Fatalf("private subnet must not touch VPC routes, got %d patches", len(applier.patchedResources))
	}
}

func TestDeleteSubnetPublicNonAnchorKeepsGatewayAndDropsRoute(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)
	a := publicSubnet("a", "10.0.0.0/24", 0)
	b := publicSubnet("b", "10.0.2.0/24", 1)

	if err := p.DeleteSubnet(context.Background(), "kloudia-vpc-x", b, []domain.Subnet{a}); err != nil {
		t.Fatalf("DeleteSubnet: %v", err)
	}

	assertKinds(t, "deleted", applier.deletedResources, []string{
		"IptablesSnatRule", "IptablesEIP", "NetworkAttachmentDefinition", "Subnet",
	})
	if got := routesOf(t, applier.patchedResources[0]); len(got) != 1 || got[0] != "10.0.0.0/24->10.0.0.254" {
		t.Fatalf("expected only the remaining public cidr routed, got %v", got)
	}
}

func TestDeleteSubnetLastPublicClearsRoutesAndGateway(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)

	if err := p.DeleteSubnet(context.Background(), "kloudia-vpc-x", publicSubnet("a", "10.0.0.0/24", 0), nil); err != nil {
		t.Fatalf("DeleteSubnet: %v", err)
	}

	assertKinds(t, "deleted", applier.deletedResources, []string{
		"IptablesSnatRule", "IptablesEIP", "VpcNatGateway", "NetworkAttachmentDefinition", "Subnet",
	})
	assertKinds(t, "patched", applier.patchedResources, []string{"Vpc"})
	if got := routesOf(t, applier.patchedResources[0]); got != nil {
		t.Fatalf("expected routes cleared, got %v", got)
	}
}

func TestDeleteNetworkRemovesGatewayThenVpc(t *testing.T) {
	applier := &recordingApplier{}
	p := NewNetworkProvisioner("kloudia-vms", "ovn-vpc-external-network", applier)

	if err := p.DeleteNetwork(context.Background(), domain.Network{OVNVPCName: "kloudia-vpc-x"}); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	assertKinds(t, "deleted", applier.deletedResources, []string{"VpcNatGateway", "Vpc"})
}

func TestNetworkProvisionerNilApplierErrors(t *testing.T) {
	p := &NetworkProvisioner{}
	network := domain.Network{OVNVPCName: "kloudia-vpc-x"}

	err := p.CreateNetwork(context.Background(), network)
	if err == nil {
		t.Fatal("expected error from nil applier, got nil")
	}
}
