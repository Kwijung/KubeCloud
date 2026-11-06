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

func TestBuildNATGatewayResourceAnchorsOnSubnetWithLanIP(t *testing.T) {
	anchor := domain.Subnet{
		CIDR: "10.0.0.0/24", Gateway: "10.0.0.1", Type: "public",
		OVNSubnetName: "kloudia-subnet-platform-app-web", NADName: "kloudia-net-platform-app-web",
	}
	res, err := BuildNATGatewayResource("kloudia-vms", "kloudia-vpc-platform-app", "ovn-vpc-external-network", anchor)
	if err != nil {
		t.Fatalf("BuildNATGatewayResource: %v", err)
	}
	if res["kind"] != "VpcNatGateway" {
		t.Fatalf("expected VpcNatGateway, got %#v", res["kind"])
	}
	spec := res["spec"].(map[string]any)
	if spec["vpc"] != "kloudia-vpc-platform-app" || spec["subnet"] != "kloudia-subnet-platform-app-web" {
		t.Fatalf("unexpected vpc/subnet: %#v", spec)
	}
	if spec["lanIp"] != "10.0.0.254" {
		t.Fatalf("expected lanIp 10.0.0.254, got %#v", spec["lanIp"])
	}
	annotations := res["metadata"].(map[string]any)["annotations"].(map[string]any)
	if annotations["k8s.v1.cni.cncf.io/networks"] != "kloudia-vms/kloudia-net-platform-app-web" {
		t.Fatalf("expected the anchor NAD attached as the gateway LAN side, got %#v", annotations)
	}
}

func TestNATGatewayLanIPAvoidsSubnetGateway(t *testing.T) {
	cases := []struct {
		cidr, gateway, want string
	}{
		{"10.0.0.0/24", "10.0.0.1", "10.0.0.254"},
		{"10.0.0.0/24", "10.0.0.254", "10.0.0.253"},
		{"10.0.0.0/30", "10.0.0.1", "10.0.0.2"},
		{"10.0.0.0/30", "10.0.0.2", "10.0.0.1"},
	}
	for _, c := range cases {
		got, err := natGatewayLanIP(domain.Subnet{CIDR: c.cidr, Gateway: c.gateway})
		if err != nil || got != c.want {
			t.Fatalf("%s gw %s: expected %s, got %q (err %v)", c.cidr, c.gateway, c.want, got, err)
		}
	}
	for _, cidr := range []string{"10.0.0.0/31", "10.0.0.0/32", "bogus"} {
		if _, err := natGatewayLanIP(domain.Subnet{CIDR: cidr, Gateway: "10.0.0.0"}); err == nil {
			t.Fatalf("%s: expected error", cidr)
		}
	}
}

func TestBuildVPCEgressRoutePatchReroutesEachPublicCIDRBySource(t *testing.T) {
	add := BuildVPCEgressRoutePatch("kloudia-vpc-platform-app", "10.0.0.254", []string{"10.0.2.0/24", "10.0.0.0/24"})
	if add["kind"] != "Vpc" || add["metadata"].(map[string]any)["name"] != "kloudia-vpc-platform-app" {
		t.Fatalf("unexpected target: %#v", add)
	}
	spec := add["spec"].(map[string]any)
	if _, ok := spec["staticRoutes"]; ok {
		t.Fatalf("egress must not touch kube-ovn managed static routes: %#v", spec)
	}
	policies := spec["policyRoutes"].([]any)
	if len(policies) != 2 {
		t.Fatalf("expected one policy per public cidr, got %#v", policies)
	}
	for i, cidr := range []string{"10.0.0.0/24", "10.0.2.0/24"} {
		policy := policies[i].(map[string]any)
		if policy["match"] != "ip4.src == "+cidr || policy["action"] != "reroute" || policy["nextHopIP"] != "10.0.0.254" {
			t.Fatalf("policy %d: unexpected %#v", i, policy)
		}
		if policy["priority"].(int) >= 31000 {
			t.Fatalf("policy %d must rank below kube-ovn's intra-VPC allow policies, got %v", i, policy["priority"])
		}
	}

	remove := BuildVPCEgressRoutePatch("kloudia-vpc-platform-app", "", nil)
	removeSpec := remove["spec"].(map[string]any)
	if v, ok := removeSpec["policyRoutes"]; !ok || v != nil {
		t.Fatalf("expected explicit null policyRoutes to clear policies, got %#v", removeSpec)
	}
}

func TestBuildSubnetEgressResources(t *testing.T) {
	s := domain.Subnet{CIDR: "10.0.0.0/24", Type: "public", OVNSubnetName: "kloudia-subnet-platform-app-web"}
	res := BuildSubnetEgressResources("kloudia-vpc-platform-app", "ovn-vpc-external-network", s)
	if res.EIP["kind"] != "IptablesEIP" {
		t.Fatalf("expected IptablesEIP, got %#v", res.EIP["kind"])
	}
	if res.EIP["spec"].(map[string]any)["natGwDp"] != "kloudia-vpc-platform-app-natgw" {
		t.Fatalf("expected EIP on the VPC nat gateway, got %#v", res.EIP["spec"])
	}
	snatSpec := res.SnatRule["spec"].(map[string]any)
	if snatSpec["internalCIDR"] != "10.0.0.0/24" {
		t.Fatalf("expected SNAT internalCIDR to match subnet, got %#v", snatSpec["internalCIDR"])
	}
}
