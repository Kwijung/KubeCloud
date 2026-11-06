package kubevirt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
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

func TestRestartVirtualMachineCallsKubeVirtSubresource(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusAccepted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusConflict {
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409}`))
		}
	}))
	defer server.Close()

	// An API server behind a path prefix, as with some proxies, must keep its prefix.
	applier, err := newDynamicApplierForConfig(&rest.Config{Host: server.URL + "/k8s/clusters/c-1"})
	if err != nil {
		t.Fatalf("applier: %v", err)
	}
	if err := applier.RestartVirtualMachine(context.Background(), "kloudia-vms", "platform-web-01"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	want := "/k8s/clusters/c-1/apis/subresources.kubevirt.io/v1/namespaces/kloudia-vms/virtualmachines/platform-web-01/restart"
	if gotMethod != http.MethodPut || gotPath != want {
		t.Fatalf("got %s %s, want PUT %s", gotMethod, gotPath, want)
	}

	status = http.StatusConflict
	if err := applier.RestartVirtualMachine(context.Background(), "kloudia-vms", "platform-web-01"); !errors.Is(err, ErrVirtualMachineNotRunning) {
		t.Fatalf("KubeVirt's 409 should mean not running, got %v", err)
	}
}
