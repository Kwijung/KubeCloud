#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KLOUDIA_SMOKE_NAMESPACE:-kloudia-smoke}"
STORAGE_CLASS="${KLOUDIA_STORAGE_CLASS:-}"
IMAGE_URL="${KLOUDIA_IMAGE_URL:-https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img}"
VM_NAME="${KLOUDIA_VM_NAME:-smoke-cirros}"
PUBLIC_KEY="${KLOUDIA_PUBLIC_KEY:-ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDkloudiasmoke kloudia-smoke@example.invalid}"
DATAVOLUME_TIMEOUT="${DATAVOLUME_WAIT_TIMEOUT:-600s}"

validate_dns_labelish() {
  [ "$#" -eq 2 ] || die "validate_dns_labelish requires a field name and value"
  local field="$1"
  local value="$2"

  [ -n "$value" ] || die "$field must not be empty"
  [ "${#value}" -le 63 ] || die "$field must be 63 characters or fewer: $value"
  [[ "$value" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] \
    || die "$field must use lowercase alphanumeric characters or '-', and start and end with an alphanumeric character: $value"
}

validate_single_line() {
  [ "$#" -eq 2 ] || die "validate_single_line requires a field name and value"
  local field="$1"
  local value="$2"

  case "$value" in
    *$'\n'*|*$'\r'*)
      die "$field must be a single-line value"
      ;;
  esac
}

print_diagnostics() {
  info "Collecting failure diagnostics for namespace: $NAMESPACE"

  kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp 2>/dev/null | tail -n 30 \
    || warn "Failed to collect namespace events"

  kubectl get datavolume,pvc,vm,vmi,pods -n "$NAMESPACE" -o wide 2>/dev/null \
    || warn "Failed to list smoke test resources"

  kubectl describe datavolume -n "$NAMESPACE" "${VM_NAME}-rootdisk" 2>/dev/null \
    || warn "Failed to describe DataVolume: ${VM_NAME}-rootdisk"
  kubectl describe pvc -n "$NAMESPACE" "${VM_NAME}-rootdisk" 2>/dev/null \
    || warn "Failed to describe PVC: ${VM_NAME}-rootdisk"
  kubectl describe vm -n "$NAMESPACE" "$VM_NAME" 2>/dev/null \
    || warn "Failed to describe VM: $VM_NAME"
  kubectl describe vmi -n "$NAMESPACE" "$VM_NAME" 2>/dev/null \
    || warn "Failed to describe VMI: $VM_NAME"
  kubectl describe pods -n "$NAMESPACE" 2>/dev/null \
    || warn "Failed to describe namespace pods"
}

resource_exists() {
  [ "$#" -eq 1 ] || die "resource_exists requires a resource"
  local resource="$1"

  kubectl get -n "$NAMESPACE" "$resource" >/dev/null 2>&1
}

require_managed_resource() {
  [ "$#" -eq 1 ] || die "require_managed_resource requires a resource"
  local resource="$1"
  local managed

  if resource_exists "$resource"; then
    managed="$(kubectl get -n "$NAMESPACE" "$resource" -o jsonpath='{.metadata.labels.kloudia\.io/managed-by}' 2>/dev/null || true)"
    [ "$managed" = "infra-spike" ] || die "Existing resource is not owned by this spike: $resource"
  fi
}

delete_resource_best_effort() {
  [ "$#" -eq 2 ] || die "delete_resource_best_effort requires a resource and name"
  local resource="$1"
  local name="$2"

  if kubectl get -n "$NAMESPACE" "$resource" "$name" >/dev/null 2>&1; then
    info "Deleting existing smoke test resource: $resource/$name"
    kubectl delete -n "$NAMESPACE" "$resource" "$name" --ignore-not-found=true >/dev/null \
      || warn "Failed to delete existing resource: $resource/$name"
  fi
}

wait_deleted_best_effort() {
  [ "$#" -eq 2 ] || die "wait_deleted_best_effort requires a resource and name"
  local resource="$1"
  local name="$2"

  if kubectl get -n "$NAMESPACE" "$resource" "$name" >/dev/null 2>&1; then
    kubectl wait -n "$NAMESPACE" "$resource/$name" --for=delete --timeout=120s >/dev/null 2>&1 \
      || warn "Timed out waiting for deletion of $resource/$name; continuing"
  fi
}

delete_existing_smoke_resources() {
  require_managed_resource "datavolume/${VM_NAME}-rootdisk"
  require_managed_resource "pvc/${VM_NAME}-rootdisk"
  require_managed_resource "vm/${VM_NAME}"
  require_managed_resource "vmi/${VM_NAME}"

  delete_resource_best_effort vm "$VM_NAME"
  delete_resource_best_effort vmi "$VM_NAME"
  delete_resource_best_effort datavolume "${VM_NAME}-rootdisk"
  delete_resource_best_effort pvc "${VM_NAME}-rootdisk"

  wait_deleted_best_effort vmi "$VM_NAME"
  wait_deleted_best_effort vm "$VM_NAME"
  wait_deleted_best_effort datavolume "${VM_NAME}-rootdisk"
  wait_deleted_best_effort pvc "${VM_NAME}-rootdisk"
}

cleanup() {
  local status=$?

  if [ "$status" -eq 0 ]; then
    if [ "${KLOUDIA_KEEP_RESOURCES:-true}" = "false" ]; then
      cleanup_namespace_on_success "$NAMESPACE"
    else
      info "Keeping smoke test resources. Delete with: kubectl delete namespace $NAMESPACE"
    fi
  else
    print_diagnostics
    warn "Smoke test failed; preserving namespace for inspection: $NAMESPACE"
  fi

  exit "$status"
}

trap cleanup EXIT

require_cmd grep
require_kubectl
validate_dns_labelish "KLOUDIA_SMOKE_NAMESPACE" "$NAMESPACE"
validate_dns_labelish "KLOUDIA_VM_NAME" "$VM_NAME"
validate_single_line "KLOUDIA_IMAGE_URL" "$IMAGE_URL"
validate_single_line "KLOUDIA_PUBLIC_KEY" "$PUBLIC_KEY"
[[ "$IMAGE_URL" =~ ^https?:// ]] || die "KLOUDIA_IMAGE_URL must start with http:// or https://"
[[ "$PUBLIC_KEY" =~ ^(ssh-rsa|ssh-ed25519|ecdsa-sha2-[^[:space:]]+)[[:space:]] ]] \
  || die "KLOUDIA_PUBLIC_KEY must start with ssh-rsa, ssh-ed25519, or ecdsa-sha2-"

if [ -z "$STORAGE_CLASS" ]; then
  default_storage_classes="$(
    kubectl get storageclass \
      -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
      || true
  )"
  default_storage_class_count="$(printf '%s\n' "$default_storage_classes" | grep -c . || true)"
  if [ "$default_storage_class_count" -gt 1 ]; then
    warn "Multiple default StorageClasses found; using the first"
  fi
  read -r STORAGE_CLASS <<< "$default_storage_classes"
fi
validate_single_line "KLOUDIA_STORAGE_CLASS" "$STORAGE_CLASS"
[ -n "$STORAGE_CLASS" ] || die "No StorageClass selected. Set KLOUDIA_STORAGE_CLASS to the NFS CSI StorageClass name"
info "Using StorageClass: $STORAGE_CLASS"

kubectl get namespace kubevirt >/dev/null 2>&1 || die "KubeVirt namespace not found. Run scripts/install-kubevirt.sh first"
kubectl get namespace cdi >/dev/null 2>&1 || die "CDI namespace not found. Run scripts/install-cdi.sh first"

create_owned_namespace "$NAMESPACE"
delete_existing_smoke_resources

cat <<YAML | kubectl apply -n "$NAMESPACE" -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: ${VM_NAME}-rootdisk
  labels:
    kloudia.io/managed-by: infra-spike
spec:
  source:
    http:
      url: "${IMAGE_URL}"
  storage:
    resources:
      requests:
        storage: 2Gi
    storageClassName: "${STORAGE_CLASS}"
YAML

info "Waiting for DataVolume import to succeed"
kubectl wait -n "$NAMESPACE" "datavolume/${VM_NAME}-rootdisk" --for=condition=Ready --timeout="$DATAVOLUME_TIMEOUT"

cat <<YAML | kubectl apply -n "$NAMESPACE" -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${VM_NAME}
  labels:
    kloudia.io/managed-by: infra-spike
spec:
  runStrategy: Always
  template:
    metadata:
      labels:
        kubevirt.io/domain: ${VM_NAME}
        kloudia.io/managed-by: infra-spike
    spec:
      domain:
        cpu:
          cores: 1
        resources:
          requests:
            memory: 512Mi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
            - name: cloudinitdisk
              disk:
                bus: virtio
          interfaces:
            - name: default
              masquerade: {}
      networks:
        - name: default
          pod: {}
      volumes:
        - name: rootdisk
          persistentVolumeClaim:
            claimName: ${VM_NAME}-rootdisk
        - name: cloudinitdisk
          cloudInitNoCloud:
            userData: |
              #cloud-config
              users:
                - name: cirros
                  sudo: ALL=(ALL) NOPASSWD:ALL
                  shell: /bin/sh
                  ssh_authorized_keys:
                    - ${PUBLIC_KEY}
YAML

info "Waiting for VirtualMachineInstance to appear"
for _ in {1..120}; do
  if kubectl get vmi -n "$NAMESPACE" "$VM_NAME" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

kubectl get vmi -n "$NAMESPACE" "$VM_NAME" >/dev/null 2>&1 || die "VMI did not appear for VM: $VM_NAME"

info "Waiting for VMI phase Running"
for _ in {1..180}; do
  phase="$(kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [ "$phase" = "Running" ]; then
    break
  fi
  sleep 2
done

phase="$(kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o jsonpath='{.status.phase}' 2>/dev/null)" \
  || die "VMI disappeared before reaching Running phase"
[ "$phase" = "Running" ] || die "VMI did not reach Running phase. Current phase: ${phase:-unknown}"

info "Checking KubeVirt subresource API discovery"
kubectl get --raw /apis/subresources.kubevirt.io/v1 | grep -E '"name"[[:space:]]*:[[:space:]]*"virtualmachineinstances/(vnc|console)"' >/dev/null \
  || die "KubeVirt subresource API discovery did not include VNC or console resources"
info "Protocol-level console validation is deferred to virtctl/API proxy work"
info "Cloud-init manifest includes the cirros SSH authorized key"
info "In-guest SSH verification is deferred because pod masquerade networking may not expose SSH from this runner"

info "VM status:"
kubectl get vm,vmi,pvc,dv -n "$NAMESPACE" -o wide

info "Recent namespace events:"
kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp | tail -n 20

info "Smoke test completed"
