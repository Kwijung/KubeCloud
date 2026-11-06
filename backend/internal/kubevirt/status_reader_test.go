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

	detail := runtimeDetail(testVirtualMachine(), vm, vmi, dv, pvc, []*unstructured.Unstructured{event}, nil)

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

	detail := runtimeDetail(testVirtualMachine(), vm, objectWithStatus("VirtualMachineInstance", "", "Running"), objectWithStatus("DataVolume", "", "Succeeded"), nil, nil, nil)

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

	detail := runtimeDetail(testVirtualMachine(), vm, objectWithStatus("VirtualMachineInstance", "", "Scheduling"), objectWithStatus("DataVolume", "", "Succeeded"), nil, nil, nil)

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

func warningEvent(kind string, name string, reason string, message string, count int64, lastTimestamp string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"type":           "Warning",
		"reason":         reason,
		"message":        message,
		"count":          count,
		"lastTimestamp":  lastTimestamp,
		"involvedObject": map[string]any{"kind": kind, "name": name},
	}}
}

func TestImportFailureDetectsDataVolumeError(t *testing.T) {
	events := []*unstructured.Unstructured{
		warningEvent("DataVolume", "platform-web-01-rootdisk", "Unschedulable", "Importer pod cannot be scheduled", 2, "2026-11-06T13:30:00Z"),
		warningEvent("DataVolume", "platform-web-01-rootdisk", "Error", "DataVolume too small to contain image", 1, "2026-11-06T13:31:00Z"),
	}
	if got := importFailure(events); got != "DataVolume too small to contain image" {
		t.Fatalf("expected the DataVolume error, got %q", got)
	}
}

func TestImportFailureNeedsRepeatedImportErrors(t *testing.T) {
	once := []*unstructured.Unstructured{
		warningEvent("PersistentVolumeClaim", "prime-1", "ErrImportFailed", "failed to pull image: connection reset by peer", 2, "2026-11-06T13:30:00Z"),
	}
	if got := importFailure(once); got != "" {
		t.Fatalf("a transient import error must not fail the VM, got %q", got)
	}
	repeated := []*unstructured.Unstructured{
		warningEvent("PersistentVolumeClaim", "prime-1", "ErrImportFailed", "failed to pull image: connection reset by peer", 2, "2026-11-06T13:30:00Z"),
		warningEvent("PersistentVolumeClaim", "prime-1", "ErrImportFailed", "Unable to connect to http data source: 404 Not Found", 3, "2026-11-06T13:32:00Z"),
	}
	if got := importFailure(repeated); got != "Unable to connect to http data source: 404 Not Found" {
		t.Fatalf("expected the latest repeated import error, got %q", got)
	}
}

func TestRuntimeDetailReportsFailedImport(t *testing.T) {
	dv := objectWithStatus("DataVolume", "", "ImportInProgress")
	events := []*unstructured.Unstructured{
		warningEvent("DataVolume", "platform-web-01-rootdisk", "Error", "DataVolume too small to contain image", 4, "2026-11-06T13:31:00Z"),
	}
	detail := runtimeDetail(domain.VirtualMachine{ID: "platform-web-01"}, objectWithStatus("VirtualMachine", "Provisioning", ""), nil, dv, nil, nil, events)

	if detail.RuntimeStatus != "Failed" {
		t.Fatalf("expected Failed, got %q", detail.RuntimeStatus)
	}
	if detail.FailureReason != "ImportFailed" || detail.FailureMessage != "DataVolume too small to contain image" {
		t.Fatalf("unexpected failure: reason=%q message=%q", detail.FailureReason, detail.FailureMessage)
	}
}

func TestRuntimeDetailIgnoresImportErrorsAfterSuccess(t *testing.T) {
	dv := objectWithStatus("DataVolume", "", "Succeeded")
	events := []*unstructured.Unstructured{
		warningEvent("DataVolume", "platform-web-01-rootdisk", "Error", "old error", 4, "2026-11-06T13:31:00Z"),
	}
	detail := runtimeDetail(domain.VirtualMachine{ID: "platform-web-01"}, objectWithStatus("VirtualMachine", "Running", ""), objectWithStatus("VirtualMachineInstance", "", "Running"), dv, nil, nil, events)

	if detail.RuntimeStatus != "Running" {
		t.Fatalf("expected Running once the import succeeded, got %q", detail.RuntimeStatus)
	}
}

func TestRuntimeDetailWithoutVMI(t *testing.T) {
	detail := runtimeDetail(testVirtualMachine(), objectWithStatus("VirtualMachine", "Stopped", ""), nil, objectWithStatus("DataVolume", "", "Succeeded"), nil, nil, nil)

	if detail.RuntimeStatus != "Stopped" || detail.VMIIP != "" {
		t.Fatalf("unexpected detail for a stopped VM: %#v", detail)
	}
}
