package kubevirt

import (
	"fmt"
	"net/netip"
	"sort"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

// nadProvider is the kube-ovn provider string linking a Subnet to its NAD: "<nad>.<namespace>.ovn".
func nadProvider(namespace string, subnet domain.Subnet) string {
	return fmt.Sprintf("%s.%s.ovn", subnet.NADName, namespace)
}

func BuildVPCResource(network domain.Network) map[string]any {
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Vpc",
		"metadata": map[string]any{
			"name": network.OVNVPCName,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/project":           network.ProjectID,
				"kloudia.io/network-id":        network.ID,
			},
		},
		"spec": map[string]any{},
	}
}

func BuildSubnetResource(namespace string, ovnVPCName string, subnet domain.Subnet) map[string]any {
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Subnet",
		"metadata": map[string]any{
			"name": subnet.OVNSubnetName,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/project":           subnet.ProjectID,
				"kloudia.io/subnet-id":         subnet.ID,
			},
		},
		"spec": map[string]any{
			"vpc":       ovnVPCName,
			"cidrBlock": subnet.CIDR,
			"gateway":   subnet.Gateway,
			"protocol":  "IPv4",
			"provider":  nadProvider(namespace, subnet),
		},
	}
}

func BuildNADResource(namespace string, subnet domain.Subnet) map[string]any {
	config := fmt.Sprintf(
		`{"cniVersion":"0.3.1","type":"kube-ovn","server_socket":"/run/openvswitch/kube-ovn-daemon.sock","provider":"%s"}`,
		nadProvider(namespace, subnet),
	)
	return map[string]any{
		"apiVersion": "k8s.cni.cncf.io/v1",
		"kind":       "NetworkAttachmentDefinition",
		"metadata": map[string]any{
			"name":      subnet.NADName,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/subnet-id":         subnet.ID,
			},
		},
		"spec": map[string]any{
			"config": config,
		},
	}
}

// SubnetEgressResources are the per-subnet kube-ovn resources that give a public subnet outbound-only SNAT
// through its VPC's shared NAT gateway.
type SubnetEgressResources struct {
	EIP      map[string]any
	SnatRule map[string]any
}

func vpcNatGatewayName(ovnVPCName string) string { return ovnVPCName + "-natgw" }
func eipName(subnet domain.Subnet) string        { return subnet.OVNSubnetName + "-eip" }
func snatRuleName(subnet domain.Subnet) string   { return subnet.OVNSubnetName + "-snat" }

// natGatewayLanIP picks the NAT gateway's address inside its anchor subnet: the last usable host,
// or the one before it when that is the subnet gateway. VMs are allocated from the low end of the CIDR.
func natGatewayLanIP(subnet domain.Subnet) (string, error) {
	prefix, err := netip.ParsePrefix(subnet.CIDR)
	if err != nil || !prefix.Addr().Is4() {
		return "", fmt.Errorf("subnet %s has invalid cidr %q", subnet.OVNSubnetName, subnet.CIDR)
	}
	prefix = prefix.Masked()
	candidate := lastAddr(prefix).Prev()
	if candidate.String() == subnet.Gateway {
		candidate = candidate.Prev()
	}
	if !prefix.Contains(candidate) || candidate == prefix.Addr() {
		return "", fmt.Errorf("subnet %s cidr %s has no room for a nat gateway address", subnet.OVNSubnetName, subnet.CIDR)
	}
	return candidate.String(), nil
}

// lastAddr returns the last (broadcast) address of an IPv4 prefix.
func lastAddr(prefix netip.Prefix) netip.Addr {
	b := prefix.Masked().Addr().As4()
	host := ^uint32(0) >> prefix.Bits()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]) | host
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// BuildNATGatewayResource builds the VPC's shared VpcNatGateway, attached to the anchor public subnet.
// externalNetwork is the kube-ovn external provider network configured at the infra layer.
// kube-ovn runs as a secondary CNI next to Cilium, so it does not attach the gateway's LAN side by
// itself: the anchor subnet's NAD must be listed in the CR's networks annotation.
func BuildNATGatewayResource(namespace string, ovnVPCName string, externalNetwork string, anchor domain.Subnet) (map[string]any, error) {
	lanIP, err := natGatewayLanIP(anchor)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "VpcNatGateway",
		"metadata": map[string]any{
			"name":   vpcNatGatewayName(ovnVPCName),
			"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			"annotations": map[string]any{
				"k8s.v1.cni.cncf.io/networks": namespace + "/" + anchor.NADName,
			},
		},
		"spec": map[string]any{
			"vpc":             ovnVPCName,
			"subnet":          anchor.OVNSubnetName,
			"lanIp":           lanIP,
			"externalSubnets": []any{externalNetwork},
		},
	}, nil
}

// natGatewayRef identifies the VPC's NAT gateway for deletion without needing its anchor subnet.
func natGatewayRef(ovnVPCName string) map[string]any {
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "VpcNatGateway",
		"metadata":   map[string]any{"name": vpcNatGatewayName(ovnVPCName)},
	}
}

// egressPolicyPriority sits below the priority-31000 "ip4.dst == <subnet> allow" policies kube-ovn adds for
// every subnet, so traffic between subnets of the VPC is never rerouted to the NAT gateway.
const egressPolicyPriority = 20000

// BuildVPCEgressRoutePatch builds a merge patch for the Vpc's policy routes: one "ip4.src == <cidr>" reroute
// to the NAT gateway at lanIP per public subnet CIDR, or no policy routes when there are no public CIDRs.
// Private subnets get no path to the gateway: a VPC-wide 0.0.0.0/0 route would let their traffic reach it
// and leave un-NATed. Static src-ip routes cannot be used for this because kube-ovn owns a src-ip route per
// subnet CIDR and overwrites ours. A merge patch keeps kube-ovn's metadata (finalizers, annotations) that a
// full update would drop.
func BuildVPCEgressRoutePatch(ovnVPCName string, lanIP string, publicCIDRs []string) map[string]any {
	var policies any
	if len(publicCIDRs) > 0 {
		sorted := append([]string(nil), publicCIDRs...)
		sort.Strings(sorted)
		list := make([]any, 0, len(sorted))
		for _, cidr := range sorted {
			list = append(list, map[string]any{
				"priority":  egressPolicyPriority,
				"match":     "ip4.src == " + cidr,
				"action":    "reroute",
				"nextHopIP": lanIP,
			})
		}
		policies = list
	}
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Vpc",
		"metadata":   map[string]any{"name": ovnVPCName},
		"spec":       map[string]any{"policyRoutes": policies},
	}
}

// BuildSubnetEgressResources builds the IptablesEIP + IptablesSnatRule that SNAT a public subnet on its
// VPC's NAT gateway.
func BuildSubnetEgressResources(ovnVPCName string, externalNetwork string, subnet domain.Subnet) SubnetEgressResources {
	eName := eipName(subnet)
	return SubnetEgressResources{
		EIP: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "IptablesEIP",
			"metadata": map[string]any{
				"name":   eName,
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"natGwDp":        vpcNatGatewayName(ovnVPCName),
				"externalSubnet": externalNetwork,
			},
		},
		SnatRule: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "IptablesSnatRule",
			"metadata": map[string]any{
				"name":   snatRuleName(subnet),
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"eip":          eName,
				"internalCIDR": subnet.CIDR,
			},
		},
	}
}
