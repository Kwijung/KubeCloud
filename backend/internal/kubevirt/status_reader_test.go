package kubevirt

import (
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRuntimeStatusReturnsRunningWhenVMAndVMIAreRunning(t *testing.T) {
	status := runtimeStatus(
		objectWithStatus("VirtualMachine", "Running", ""),
		objectWithStatus("VirtualMachineInstance", "Running", ""),
		objectWithStatus("DataVolume", "Succeeded", ""),
	)

	if status != "Running" {
		t.Fatalf("expected Running, got %q", status)
	}
}

func TestRuntimeStatusReturnsImportingUntilDataVolumeReady(t *testing.T) {
	status := runtimeStatus(
		objectWithStatus("VirtualMachine", "Provisioning", ""),
		nil,
		objectWithStatus("DataVolume", "ImportInProgress", ""),
	)

	if status != "Importing" {
		t.Fatalf("expected Importing, got %q", status)
	}
}

func TestRuntimeStatusReturnsFailedForUnschedulableVM(t *testing.T) {
	status := runtimeStatus(
		objectWithStatus("VirtualMachine", "ErrorUnschedulable", ""),
		objectWithStatus("VirtualMachineInstance", "Scheduling", ""),
		objectWithStatus("DataVolume", "Succeeded", ""),
	)

	if status != "Failed" {
		t.Fatalf("expected Failed, got %q", status)
	}
}

func TestRuntimeStatusReturnsMissingWhenNoKubeVirtResourcesExist(t *testing.T) {
	status := runtimeStatus(nil, nil, nil)

	if status != "Missing" {
		t.Fatalf("expected Missing, got %q", status)
	}
}

func TestRuntimeDetailExtractsVMIDataVolumePVCAndEvents(t *testing.T) {
	vm := objectWithStatus("VirtualMachine", "Running", "")
	vmi := objectWithStatus("VirtualMachineInstance", "", "Running")
	_ = unstructured.SetNestedSlice(vmi.Object, []any{map[string]any{"ipAddress": "10.244.1.183"}}, "status", "interfaces")
	_ = unstructured.SetNestedField(vmi.Object, "test-worker-ca9c9bc3", "status", "nodeName")
	dv := objectWithStatus("DataVolume", "", "Succeeded")
	_ = unstructured.SetNestedField(dv.Object, "100.0%", "status", "progress")
	pvc := objectWithStatus("PersistentVolumeClaim", "", "Bound")
	_ = unstructured.SetNestedStringMap(pvc.Object, map[string]string{"storage": "2Gi"}, "status", "capacity")
	event := &unstructured.Unstructured{Object: map[string]any{
		"type":    "Normal",
		"reason":  "Started",
		"message": "VM started",
	}}

	detail := runtimeDetail(testVirtualMachine(), vm, vmi, dv, pvc, []*unstructured.Unstructured{event})

	if detail.RuntimeStatus != "Running" {
		t.Fatalf("expected Running, got %q", detail.RuntimeStatus)
	}
	if detail.VMIIP != "10.244.1.183" {
		t.Fatalf("expected VMI IP, got %q", detail.VMIIP)
	}
	if detail.NodeName != "test-worker-ca9c9bc3" {
		t.Fatalf("expected node name, got %q", detail.NodeName)
	}
	if detail.DataVolumePhase != "Succeeded" || detail.DataVolumeStatus != "100.0%" {
		t.Fatalf("unexpected DataVolume detail: %#v", detail)
	}
	if detail.PVCPhase != "Bound" || detail.PVCCapacity != "2Gi" {
		t.Fatalf("unexpected PVC detail: %#v", detail)
	}
	if len(detail.Events) != 1 || detail.Events[0].Reason != "Started" {
		t.Fatalf("expected Started event, got %#v", detail.Events)
	}
}

func TestRuntimeDetailIgnoresNonFailureConditions(t *testing.T) {
	vm := objectWithStatus("VirtualMachine", "Running", "")
	_ = unstructured.SetNestedSlice(vm.Object, []any{map[string]any{
		"type":    "Migratable",
		"status":  "False",
		"reason":  "InterfaceNotLiveMigratable",
		"message": "cannot migrate VMI which does not use masquerade",
	}}, "status", "conditions")

	detail := runtimeDetail(testVirtualMachine(), vm, objectWithStatus("VirtualMachineInstance", "", "Running"), objectWithStatus("DataVolume", "", "Succeeded"), nil, nil)

	if detail.FailureReason != "" || detail.FailureMessage != "" {
		t.Fatalf("expected no failure detail, got reason=%q message=%q", detail.FailureReason, detail.FailureMessage)
	}
}

func TestRuntimeDetailExtractsFailureConditions(t *testing.T) {
	vm := objectWithStatus("VirtualMachine", "ErrorUnschedulable", "")
	_ = unstructured.SetNestedSlice(vm.Object, []any{map[string]any{
		"type":    "Synchronized",
		"status":  "False",
		"reason":  "Unschedulable",
		"message": "0/1 nodes are available",
	}}, "status", "conditions")

	detail := runtimeDetail(testVirtualMachine(), vm, objectWithStatus("VirtualMachineInstance", "", "Scheduling"), objectWithStatus("DataVolume", "", "Succeeded"), nil, nil)

	if detail.FailureReason != "Unschedulable" || detail.FailureMessage != "0/1 nodes are available" {
		t.Fatalf("expected failure detail, got reason=%q message=%q", detail.FailureReason, detail.FailureMessage)
	}
}

func testVirtualMachine() domain.VirtualMachine {
	return domain.VirtualMachine{
		ID:        "platform-portal-02",
		ProjectID: "platform",
		Name:      "portal-02",
		ImageID:   "platform-cirros",
		FlavorID:  "small",
		Status:    "Provisioning",
	}
}

func objectWithStatus(kind string, printableStatus string, phase string) *unstructured.Unstructured {
	status := map[string]any{}
	if printableStatus != "" {
		status["printableStatus"] = printableStatus
	}
	if phase != "" {
		status["phase"] = phase
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"kind":   kind,
		"status": status,
	}}
}
