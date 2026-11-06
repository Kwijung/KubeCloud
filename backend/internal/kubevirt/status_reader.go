package kubevirt

import (
	"context"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type StatusReader struct {
	namespace        string
	storageClassName string
	client           dynamic.Interface
}

func NewStatusReader(namespace string, storageClassName string, client dynamic.Interface) *StatusReader {
	return &StatusReader{namespace: namespace, storageClassName: storageClassName, client: client}
}

func NewDynamicStatusReader(namespace string, storageClassName string, kubeconfigPath string) (*StatusReader, error) {
	cfg, err := buildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewStatusReader(namespace, storageClassName, client), nil
}

func (r *StatusReader) GetVirtualMachineStatus(ctx context.Context, vm domain.VirtualMachine) (string, error) {
	if r == nil || r.client == nil {
		return vm.Status, nil
	}
	kubeVM, err := r.get(ctx, schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}, vm.ID)
	if err != nil && !apierrors.IsNotFound(err) {
		return vm.Status, err
	}
	vmi, err := r.get(ctx, schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}, vm.ID)
	if err != nil && !apierrors.IsNotFound(err) {
		return vm.Status, err
	}
	dv, err := r.get(ctx, schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}, vm.ID+"-rootdisk")
	if err != nil && !apierrors.IsNotFound(err) {
		return vm.Status, err
	}
	status := runtimeStatus(kubeVM, vmi, dv)
	if status == "Importing" {
		pvc, err := r.get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, vm.ID+"-rootdisk")
		if err != nil && !apierrors.IsNotFound(err) {
			return status, err
		}
		importEvents, err := r.listImportEvents(ctx, vm.ID+"-rootdisk", dv, pvc)
		if err != nil {
			return status, err
		}
		if importFailure(importEvents) != "" {
			return "Failed", nil
		}
	}
	return status, nil
}

func (r *StatusReader) GetVirtualMachineDetail(ctx context.Context, vm domain.VirtualMachine) (domain.VirtualMachineDetail, error) {
	if r == nil || r.client == nil {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, nil
	}
	kubeVM, err := r.get(ctx, schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}, vm.ID)
	if err != nil && !apierrors.IsNotFound(err) {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	vmi, err := r.get(ctx, schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}, vm.ID)
	if err != nil && !apierrors.IsNotFound(err) {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	dv, err := r.get(ctx, schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}, vm.ID+"-rootdisk")
	if err != nil && !apierrors.IsNotFound(err) {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	pvc, err := r.get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, vm.ID+"-rootdisk")
	if err != nil && !apierrors.IsNotFound(err) {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	events, err := r.listEvents(ctx, vm.ID)
	if err != nil {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	importEvents, err := r.listImportEvents(ctx, vm.ID+"-rootdisk", dv, pvc)
	if err != nil {
		return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, err
	}
	return runtimeDetail(vm, kubeVM, vmi, dv, pvc, events, importEvents), nil
}

func (r *StatusReader) get(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	return r.client.Resource(gvr).Namespace(r.namespace).Get(ctx, name, v1.GetOptions{})
}

func (r *StatusReader) GetPreflight(ctx context.Context) (domain.PreflightResult, error) {
	if r == nil || r.client == nil {
		return domain.PreflightResult{Status: "Warning", Checks: []domain.PreflightCheck{{Name: "kubernetes", Status: "Unknown", Message: "Kubernetes client is not configured"}}}, nil
	}
	checks := []domain.PreflightCheck{}
	checks = append(checks, r.objectCheck(ctx, schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, "", r.namespace, "namespace"))
	checks = append(checks, r.objectCheck(ctx, schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, "", r.storageClassName, "storageClass"))
	nodes, err := r.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "nodes"}).List(ctx, v1.ListOptions{LabelSelector: "kubevirt.io/schedulable=true"})
	if err != nil {
		checks = append(checks, domain.PreflightCheck{Name: "schedulableNodes", Status: "Failed", Message: err.Error()})
	} else if len(nodes.Items) == 0 {
		checks = append(checks, domain.PreflightCheck{Name: "schedulableNodes", Status: "Failed", Message: "No KubeVirt schedulable worker nodes found"})
	} else {
		checks = append(checks, domain.PreflightCheck{Name: "schedulableNodes", Status: "OK", Message: "KubeVirt schedulable nodes: " + strconv.Itoa(len(nodes.Items))})
	}
	vms, err := r.client.Resource(schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}).Namespace(r.namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		checks = append(checks, domain.PreflightCheck{Name: "virtualMachines", Status: "Warning", Message: err.Error()})
	} else {
		checks = append(checks, domain.PreflightCheck{Name: "virtualMachines", Status: "OK", Message: "Current VM resources: " + strconv.Itoa(len(vms.Items))})
	}
	_, err = r.client.Resource(schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"}).List(ctx, v1.ListOptions{Limit: 1})
	if err != nil {
		checks = append(checks, domain.PreflightCheck{Name: "kube-ovn-vpc", Status: "Failed", Message: err.Error()})
	} else {
		checks = append(checks, domain.PreflightCheck{Name: "kube-ovn-vpc", Status: "OK", Message: "kube-ovn VPC CRD is available"})
	}
	_, err = r.client.Resource(schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"}).Namespace(r.namespace).List(ctx, v1.ListOptions{Limit: 1})
	if err != nil {
		checks = append(checks, domain.PreflightCheck{Name: "multus", Status: "Failed", Message: err.Error()})
	} else {
		checks = append(checks, domain.PreflightCheck{Name: "multus", Status: "OK", Message: "Multus NetworkAttachmentDefinition CRD is available"})
	}
	_, err = r.client.Resource(schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-nat-gateways"}).List(ctx, v1.ListOptions{Limit: 1})
	if err != nil {
		checks = append(checks, domain.PreflightCheck{Name: "kube-ovn-nat-gateway", Status: "Warning", Message: "public subnet egress unavailable: " + err.Error()})
	} else {
		checks = append(checks, domain.PreflightCheck{Name: "kube-ovn-nat-gateway", Status: "OK", Message: "kube-ovn VPC NAT gateway CRD is available"})
	}
	status := "OK"
	for _, check := range checks {
		if check.Status == "Failed" {
			status = "Failed"
			break
		}
		if check.Status == "Warning" && status == "OK" {
			status = "Warning"
		}
	}
	return domain.PreflightResult{Status: status, Checks: checks}, nil
}

func (r *StatusReader) objectCheck(ctx context.Context, gvr schema.GroupVersionResource, namespace string, name string, label string) domain.PreflightCheck {
	var err error
	if namespace == "" {
		_, err = r.client.Resource(gvr).Get(ctx, name, v1.GetOptions{})
	} else {
		_, err = r.client.Resource(gvr).Namespace(namespace).Get(ctx, name, v1.GetOptions{})
	}
	if err != nil {
		return domain.PreflightCheck{Name: label, Status: "Failed", Message: err.Error()}
	}
	return domain.PreflightCheck{Name: label, Status: "OK", Message: name}
}

func (r *StatusReader) listEvents(ctx context.Context, vmID string) ([]*unstructured.Unstructured, error) {
	list, err := r.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "events"}).
		Namespace(r.namespace).
		List(ctx, v1.ListOptions{FieldSelector: "involvedObject.name=" + vmID})
	if err != nil {
		return nil, err
	}
	events := make([]*unstructured.Unstructured, 0, len(list.Items))
	start := 0
	if len(list.Items) > 8 {
		start = len(list.Items) - 8
	}
	for i := start; i < len(list.Items); i++ {
		event := list.Items[i]
		events = append(events, &event)
	}
	return events, nil
}

// listImportEvents returns the warning events of a root disk import: those on the DataVolume, and those on the
// "prime" PVC that CDI imports into before handing the data to the target PVC (named prime-<target PVC UID>).
// Events outlive their objects (about an hour), and a DataVolume can be re-created under the same name, so
// events on the DataVolume count only when they belong to the current one (its UID).
func (r *StatusReader) listImportEvents(ctx context.Context, dataVolumeName string, dv, pvc *unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	names := []string{dataVolumeName}
	if pvc != nil && pvc.GetUID() != "" {
		names = append(names, "prime-"+string(pvc.GetUID()))
	}
	var events []*unstructured.Unstructured
	for _, name := range names {
		list, err := r.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "events"}).
			Namespace(r.namespace).
			List(ctx, v1.ListOptions{FieldSelector: "type=Warning,involvedObject.name=" + name})
		if err != nil {
			return nil, err
		}
		for i := range list.Items {
			event := &list.Items[i]
			if name == dataVolumeName {
				uid, _, _ := unstructured.NestedString(event.Object, "involvedObject", "uid")
				if dv == nil || uid != string(dv.GetUID()) {
					continue // an earlier DataVolume of the same name
				}
			}
			events = append(events, event)
		}
	}
	return events, nil
}

// importFailure returns the error of a root disk import that is not going to succeed on its own, or "".
// CDI retries a failed import forever, so the DataVolume stays in progress. A DataVolume "Error" event (for
// example "DataVolume too small to contain image") is final; an import error is treated as final once it
// has repeated, so a single dropped download does not fail the VM.
func importFailure(events []*unstructured.Unstructured) string {
	const repeatedImportErrors = 3
	var message, latest string
	for _, event := range events {
		kind, _, _ := unstructured.NestedString(event.Object, "involvedObject", "kind")
		reason := stringField(event, "reason")
		count, _, _ := unstructured.NestedInt64(event.Object, "count")
		final := (kind == "DataVolume" && reason == "Error") || (reason == "ErrImportFailed" && count >= repeatedImportErrors)
		if !final {
			continue
		}
		if timestamp := stringField(event, "lastTimestamp"); message == "" || timestamp >= latest {
			message, latest = stringField(event, "message"), timestamp
		}
	}
	return message
}

func runtimeStatus(vm *unstructured.Unstructured, vmi *unstructured.Unstructured, dv *unstructured.Unstructured) string {
	if vm == nil && vmi == nil && dv == nil {
		return "Missing"
	}
	dvPhase := statusField(dv, "phase")
	if dvPhase == "" {
		dvPhase = statusField(dv, "printableStatus")
	}
	if dvPhase != "" && dvPhase != "Succeeded" {
		return "Importing"
	}
	vmStatus := statusField(vm, "printableStatus")
	vmiPhase := statusField(vmi, "phase")
	if strings.Contains(strings.ToLower(vmStatus), "error") || strings.Contains(strings.ToLower(vmStatus), "failed") {
		return "Failed"
	}
	if vmiPhase == "Running" || vmStatus == "Running" {
		return "Running"
	}
	if vmStatus == "Stopped" {
		return "Stopped"
	}
	return "Provisioning"
}

func statusField(obj *unstructured.Unstructured, field string) string {
	if obj == nil {
		return ""
	}
	value, _, _ := unstructured.NestedString(obj.Object, "status", field)
	return value
}

func runtimeDetail(vm domain.VirtualMachine, kubeVM *unstructured.Unstructured, vmi *unstructured.Unstructured, dv *unstructured.Unstructured, pvc *unstructured.Unstructured, events []*unstructured.Unstructured, importEvents []*unstructured.Unstructured) domain.VirtualMachineDetail {
	runtime := runtimeStatus(kubeVM, vmi, dv)
	reason, message := failureFromConditions(kubeVM, vmi, dv, pvc)
	if runtime == "Importing" {
		if failure := importFailure(importEvents); failure != "" {
			runtime, reason, message = "Failed", "ImportFailed", failure
		}
	}
	detail := domain.VirtualMachineDetail{
		VirtualMachine:   vm,
		RuntimeStatus:    runtime,
		FailureReason:    reason,
		FailureMessage:   message,
		VMIPhase:         statusField(vmi, "phase"),
		NodeName:         statusField(vmi, "nodeName"),
		DataVolumePhase:  statusField(dv, "phase"),
		DataVolumeStatus: statusField(dv, "progress"),
		PVCPhase:         statusField(pvc, "phase"),
		PVCCapacity:      pvcCapacity(pvc),
		ConsoleAvailable: runtime == "Running",
		Events:           runtimeEvents(events),
	}
	if vmi == nil {
		return detail
	}
	if interfaces, ok, _ := unstructured.NestedSlice(vmi.Object, "status", "interfaces"); ok && len(interfaces) > 0 {
		if first, ok := interfaces[0].(map[string]any); ok {
			if ip, ok := first["ipAddress"].(string); ok {
				detail.VMIIP = ip
			}
		}
	}
	return detail
}

func failureFromConditions(objects ...*unstructured.Unstructured) (string, string) {
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		conditions, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
		if !ok {
			continue
		}
		for _, item := range conditions {
			condition, ok := item.(map[string]any)
			if !ok {
				continue
			}
			status, _ := condition["status"].(string)
			reason, _ := condition["reason"].(string)
			message, _ := condition["message"].(string)
			if status == "False" && isFailureSignal(reason, message) {
				return reason, message
			}
		}
	}
	return "", ""
}

func isFailureSignal(reason string, message string) bool {
	text := strings.ToLower(reason + " " + message)
	for _, signal := range []string{"fail", "error", "unschedulable", "insufficient", "exceeded", "forbidden", "invalid", "timeout"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func pvcCapacity(pvc *unstructured.Unstructured) string {
	if pvc == nil {
		return ""
	}
	capacity, _, _ := unstructured.NestedStringMap(pvc.Object, "status", "capacity")
	return capacity["storage"]
}

func runtimeEvents(events []*unstructured.Unstructured) []domain.RuntimeEvent {
	result := make([]domain.RuntimeEvent, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		result = append(result, domain.RuntimeEvent{
			Type:    stringField(event, "type"),
			Reason:  stringField(event, "reason"),
			Message: stringField(event, "message"),
		})
	}
	return result
}

func stringField(obj *unstructured.Unstructured, field string) string {
	value, _, _ := unstructured.NestedString(obj.Object, field)
	return value
}
