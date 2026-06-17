#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KLOUDIA_CHECK_NAMESPACE:-kloudia-prereq-check}"
STORAGE_CLASS="${KLOUDIA_STORAGE_CLASS:-}"
NAMESPACE_CREATED=false

cleanup_owned_resource_on_success() {
  [ "$#" -eq 2 ] && [ -n "$1" ] && [ -n "$2" ] || die "cleanup_owned_resource_on_success requires kind and name"
  local kind="$1"
  local name="$2"
  local managed

  if ! kubectl get -n "$NAMESPACE" "$kind/$name" >/dev/null 2>&1; then
    return
  fi

  managed="$(kubectl get -n "$NAMESPACE" "$kind/$name" -o jsonpath='{.metadata.labels.kloudia\.io/managed-by}' 2>/dev/null || true)"
  [ "$managed" = "infra-spike" ] || die "Refusing to delete $kind/$name because it is not labeled kloudia.io/managed-by=infra-spike"

  kubectl delete -n "$NAMESPACE" "$kind/$name" --ignore-not-found=true >/dev/null
  info "Deleted prereq check resource: $kind/$name"
}

cleanup() {
  local status=$?

  if [ "$status" -eq 0 ]; then
    if [ "${KLOUDIA_KEEP_RESOURCES:-false}" = "true" ]; then
      warn "KLOUDIA_KEEP_RESOURCES=true leaves prereq resources in place, including privileged hostPath DaemonSet kvm-device-check"
      info "Keeping namespace and prereq resources: $NAMESPACE"
    else
      cleanup_owned_resource_on_success daemonset kvm-device-check
      cleanup_owned_resource_on_success pvc pvc-provisioning-check
    fi

    if [ "$NAMESPACE_CREATED" = "true" ]; then
      cleanup_namespace_on_success "$NAMESPACE"
    else
      info "Leaving pre-existing namespace in place: $NAMESPACE"
    fi
  else
    warn "Prerequisite check failed; preserving namespace for inspection: $NAMESPACE"
  fi

  exit "$status"
}

require_cmd sed
require_cmd grep
require_kubectl

version="$(server_version)"
info "Kubernetes server version: ${version:-unknown}"

case "$version" in
  v1.36.*)
    info "Kubernetes version matches expected planning baseline: $version"
    ;;
  *)
    warn "Kubernetes version differs from planning baseline v1.36.x: ${version:-unknown}"
    ;;
esac

info "Cluster nodes:"
kubectl get nodes -o wide

worker_nodes="$(
  kubectl get nodes --selector='!node-role.kubernetes.io/control-plane,!node-role.kubernetes.io/master' \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null
)"
worker_count="$(printf '%s\n' "$worker_nodes" | grep -c . || true)"
[ "$worker_count" -gt 0 ] || die "No worker node candidate found"
info "Worker node candidate count: $worker_count"
info "Worker node candidates:"
printf '%s\n' "$worker_nodes"

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
  STORAGE_CLASS="$(printf '%s\n' "$default_storage_classes" | sed -n '1p')"
fi
[ -n "$STORAGE_CLASS" ] || die "No StorageClass selected. Set KLOUDIA_STORAGE_CLASS to the NFS CSI StorageClass name"
info "Using StorageClass: $STORAGE_CLASS"

if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
  NAMESPACE_CREATED=true
fi
trap cleanup EXIT
create_owned_namespace "$NAMESPACE"

info "Testing dynamic PVC provisioning"
cat <<YAML | kubectl apply -n "$NAMESPACE" -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-provisioning-check
  labels:
    kloudia.io/managed-by: infra-spike
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: "$STORAGE_CLASS"
  resources:
    requests:
      storage: 1Gi
YAML

kubectl wait -n "$NAMESPACE" pvc/pvc-provisioning-check --for=jsonpath='{.status.phase}'=Bound --timeout=180s
info "PVC provisioning succeeded"

info "Checking privileged pod access to host virtualization devices on worker nodes"
cat <<'YAML' | kubectl apply -n "$NAMESPACE" -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: kvm-device-check
  labels:
    app: kvm-device-check
    kloudia.io/managed-by: infra-spike
spec:
  selector:
    matchLabels:
      app: kvm-device-check
  template:
    metadata:
      labels:
        app: kvm-device-check
        kloudia.io/managed-by: infra-spike
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
                    operator: DoesNotExist
                  - key: node-role.kubernetes.io/master
                    operator: DoesNotExist
      containers:
        - name: check
          image: busybox:1.36
          securityContext:
            privileged: true
          command:
            - sh
            - -c
            - |
              set -eu
              test -c /host-dev/kvm
              test -c /host-dev/net/tun
              if [ -d /host-sys/module/vhost_net ]; then
                echo "vhost_net module visible"
              else
                echo "vhost_net module not visible"
              fi
              echo "KVM device check passed on $(hostname)"
              sleep 3600
          volumeMounts:
            - name: host-dev
              mountPath: /host-dev
              readOnly: true
            - name: host-sys
              mountPath: /host-sys
              readOnly: true
      volumes:
        - name: host-dev
          hostPath:
            path: /dev
        - name: host-sys
          hostPath:
            path: /sys
YAML

kubectl rollout status -n "$NAMESPACE" daemonset/kvm-device-check --timeout=180s
kubectl logs -n "$NAMESPACE" -l app=kvm-device-check --tail=-1
info "Prerequisite check completed"
