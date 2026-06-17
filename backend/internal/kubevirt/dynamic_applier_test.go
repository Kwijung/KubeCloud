package kubevirt

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourceMappingForKubeVirtResources(t *testing.T) {
	type testCase struct {
		apiVersion string
		kind       string
		wantGVR    schema.GroupVersionResource
		namespaced bool
	}

	cases := []testCase{
		{
			apiVersion: "cdi.kubevirt.io/v1beta1",
			kind:       "DataVolume",
			wantGVR:    schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"},
			namespaced: true,
		},
		{
			apiVersion: "kubevirt.io/v1",
			kind:       "VirtualMachine",
			wantGVR:    schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"},
			namespaced: true,
		},
		{
			apiVersion: "kubeovn.io/v1",
			kind:       "Vpc",
			wantGVR:    schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"},
			namespaced: false,
		},
		{
			apiVersion: "kubeovn.io/v1",
			kind:       "Subnet",
			wantGVR:    schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "subnets"},
			namespaced: false,
		},
		{
			apiVersion: "k8s.cni.cncf.io/v1",
			kind:       "NetworkAttachmentDefinition",
			wantGVR:    schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"},
			namespaced: true,
		},
		{
			apiVersion: "kubeovn.io/v1",
			kind:       "VpcNatGateway",
			wantGVR:    schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-nat-gateways"},
			namespaced: false,
		},
		{
			apiVersion: "kubeovn.io/v1",
			kind:       "IptablesEIP",
			wantGVR:    schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-eips"},
			namespaced: false,
		},
		{
			apiVersion: "kubeovn.io/v1",
			kind:       "IptablesSnatRule",
			wantGVR:    schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-snat-rules"},
			namespaced: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.apiVersion+"/"+tc.kind, func(t *testing.T) {
			gvr, namespaced, err := resourceMapping(map[string]any{
				"apiVersion": tc.apiVersion,
				"kind":       tc.kind,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gvr != tc.wantGVR {
				t.Fatalf("GVR mismatch: got %#v, want %#v", gvr, tc.wantGVR)
			}
			if namespaced != tc.namespaced {
				t.Fatalf("namespaced mismatch: got %v, want %v", namespaced, tc.namespaced)
			}
		})
	}
}

func TestResourceMappingRejectsUnsupportedResource(t *testing.T) {
	_, _, err := resourceMapping(map[string]any{
		"apiVersion": "foo/v1",
		"kind":       "Bar",
	})
	if err == nil {
		t.Fatal("expected unsupported resource error")
	}
}
