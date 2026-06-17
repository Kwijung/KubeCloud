package kubevirt

import (
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestBuildVPCResource(t *testing.T) {
	n := domain.Network{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app"}
	res := BuildVPCResource(n)
	if res["apiVersion"] != "kubeovn.io/v1" || res["kind"] != "Vpc" {
		t.Fatalf("unexpected gvk: %#v", res)
	}
	meta := res["metadata"].(map[string]any)
	if meta["name"] != "kloudia-vpc-platform-app" {
		t.Fatalf("unexpected vpc name: %#v", meta["name"])
	}
	if _, hasNS := meta["namespace"]; hasNS {
		t.Fatalf("Vpc is cluster-scoped and must not set namespace")
	}
}

func TestBuildSubnetResourceWiresVPCAndProvider(t *testing.T) {
	s := domain.Subnet{
		ID: "platform-app-web", ProjectID: "platform", NetworkID: "platform-app",
		CIDR: "10.0.0.0/24", Gateway: "10.0.0.1",
		OVNSubnetName: "kloudia-subnet-platform-app-web", NADName: "kloudia-net-platform-app-web",
	}
	res := BuildSubnetResource("kloudia-vms", "kloudia-vpc-platform-app", s)
	spec := res["spec"].(map[string]any)
	if spec["vpc"] != "kloudia-vpc-platform-app" {
		t.Fatalf("expected vpc ref, got %#v", spec["vpc"])
	}
	if spec["cidrBlock"] != "10.0.0.0/24" {
		t.Fatalf("expected cidrBlock, got %#v", spec["cidrBlock"])
	}
	if spec["provider"] != "kloudia-net-platform-app-web.kloudia-vms.ovn" {
		t.Fatalf("expected provider, got %#v", spec["provider"])
	}
}

func TestBuildNADResourceIsNamespacedWithProvider(t *testing.T) {
	s := domain.Subnet{NADName: "kloudia-net-platform-app-web"}
	res := BuildNADResource("kloudia-vms", s)
	if res["apiVersion"] != "k8s.cni.cncf.io/v1" || res["kind"] != "NetworkAttachmentDefinition" {
		t.Fatalf("unexpected gvk: %#v", res)
	}
	meta := res["metadata"].(map[string]any)
	if meta["namespace"] != "kloudia-vms" || meta["name"] != "kloudia-net-platform-app-web" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	spec := res["spec"].(map[string]any)
	cfg := spec["config"].(string)
	if !contains(cfg, "kube-ovn") || !contains(cfg, "kloudia-net-platform-app-web.kloudia-vms.ovn") {
		t.Fatalf("expected kube-ovn config with provider, got %q", cfg)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || index(haystack, needle) >= 0)
}

func index(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestBuildPublicEgressResources(t *testing.T) {
	s := domain.Subnet{
		ID: "platform-app-web", ProjectID: "platform", NetworkID: "platform-app",
		CIDR: "10.0.0.0/24", Type: "public",
		OVNSubnetName: "kloudia-subnet-platform-app-web",
	}
	res := BuildPublicEgressResources("kloudia-vms", "kloudia-vpc-platform-app", "ovn-vpc-external-network", s)
	if res.NatGateway["kind"] != "VpcNatGateway" {
		t.Fatalf("expected VpcNatGateway, got %#v", res.NatGateway["kind"])
	}
	if res.EIP["kind"] != "IptablesEIP" {
		t.Fatalf("expected IptablesEIP, got %#v", res.EIP["kind"])
	}
	snatSpec := res.SnatRule["spec"].(map[string]any)
	if snatSpec["internalCIDR"] != "10.0.0.0/24" {
		t.Fatalf("expected SNAT internalCIDR to match subnet, got %#v", snatSpec["internalCIDR"])
	}
}
