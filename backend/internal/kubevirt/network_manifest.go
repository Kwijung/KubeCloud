package kubevirt

import (
	"fmt"

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

// EgressResources are the kube-ovn resources that give a public subnet outbound-only SNAT.
type EgressResources struct {
	NatGateway map[string]any
	EIP        map[string]any
	SnatRule   map[string]any
}

func vpcNatGatewayName(ovnVPCName string) string { return ovnVPCName + "-natgw" }
func eipName(subnet domain.Subnet) string        { return subnet.OVNSubnetName + "-eip" }
func snatRuleName(subnet domain.Subnet) string   { return subnet.OVNSubnetName + "-snat" }

// BuildPublicEgressResources builds VpcNatGateway + IptablesEIP + IptablesSnatRule for a public subnet.
// externalNetwork is the kube-ovn external provider network configured at the infra layer.
func BuildPublicEgressResources(namespace string, ovnVPCName string, externalNetwork string, subnet domain.Subnet) EgressResources {
	gwName := vpcNatGatewayName(ovnVPCName)
	eName := eipName(subnet)
	return EgressResources{
		NatGateway: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "VpcNatGateway",
			"metadata": map[string]any{
				"name":   gwName,
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"vpc":             ovnVPCName,
				"subnet":          subnet.OVNSubnetName,
				"externalSubnets": []any{externalNetwork},
			},
		},
		EIP: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "IptablesEIP",
			"metadata": map[string]any{
				"name":   eName,
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"natGwDp":        gwName,
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
