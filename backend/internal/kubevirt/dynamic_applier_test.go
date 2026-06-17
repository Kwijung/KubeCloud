package kubevirt

import "testing"

func TestResourceMappingForKubeVirtResources(t *testing.T) {
	dv, namespaced, err := resourceMapping(map[string]any{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
	})
	if err != nil {
		t.Fatalf("data volume mapping: %v", err)
	}
	if dv.Group != "cdi.kubevirt.io" || dv.Version != "v1beta1" || dv.Resource != "datavolumes" {
		t.Fatalf("unexpected DataVolume mapping: %#v", dv)
	}
	if !namespaced {
		t.Fatalf("DataVolume should be namespaced")
	}

	vm, namespaced, err := resourceMapping(map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
	})
	if err != nil {
		t.Fatalf("virtual machine mapping: %v", err)
	}
	if vm.Group != "kubevirt.io" || vm.Version != "v1" || vm.Resource != "virtualmachines" {
		t.Fatalf("unexpected VirtualMachine mapping: %#v", vm)
	}
	if !namespaced {
		t.Fatalf("VirtualMachine should be namespaced")
	}
}

func TestResourceMappingRejectsUnsupportedResource(t *testing.T) {
	_, _, err := resourceMapping(map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
	})
	if err == nil {
		t.Fatal("expected unsupported resource error")
	}
}
