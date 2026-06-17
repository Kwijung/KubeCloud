# KubeVirt Infra Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first executable infra spike for installing and validating KubeVirt/CDI on the existing VM-based Kubernetes cluster.

**Architecture:** Add focused shell scripts under `scripts/` that check cluster prerequisites, install KubeVirt, install CDI, and run a minimal VM smoke test. Keep the scripts independent from the future Portal MVP so infra risk can be isolated before API/UI implementation.

**Tech Stack:** Bash, kubectl, curl, Kubernetes, KubeVirt, CDI, NFS CSI StorageClass.

---

## Scope

This plan implements the Infra Spike only. The Portal MVP will get a separate plan after the cluster can create and run a minimal KubeVirt VM.

The workspace is currently not a Git repository. Commit steps are replaced with explicit status checks. If a Git repository is initialized before execution, commit after each task with the command shown in that task.

## File Structure

- Create: `scripts/lib/common.sh`
  - Shared Bash helpers for logging, command checks, Kubernetes connectivity, and cleanup-safe namespace handling.
- Create: `scripts/check-cluster-prereqs.sh`
  - Validates kubectl access, Kubernetes version, worker node shape, StorageClass/PVC provisioning, privileged DaemonSet execution, `/dev/kvm`, `/dev/net/tun`, and `vhost_net` visibility.
- Create: `scripts/install-kubevirt.sh`
  - Installs KubeVirt operator and CR from a chosen or stable release.
- Create: `scripts/install-cdi.sh`
  - Installs CDI operator and CR from a chosen or latest GitHub release.
- Create: `scripts/smoke-test-vm.sh`
  - Creates a namespace, imports a Cirros image with CDI DataVolume, creates a small KubeVirt VM with cloud-init SSH key injection, starts it, waits for Running state, checks subresource API discovery, and prints status.
- Create: `docs/infra/kubevirt-infra-spike.md`
  - Operator-facing runbook for executing the scripts in order and interpreting failures.

## Task 1: Script Foundation

**Files:**
- Create: `scripts/lib/common.sh`

- [ ] **Step 1: Create the shared helper file**

Create `scripts/lib/common.sh` with this content:

```bash
#!/usr/bin/env bash

set -euo pipefail

info() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "Required command not found: $cmd"
}

require_kubectl() {
  require_cmd kubectl
  kubectl cluster-info >/dev/null 2>&1 || die "kubectl cannot reach the Kubernetes cluster"
}

server_version() {
  kubectl get --raw /version | sed -n 's/.*"gitVersion":"\([^"]*\)".*/\1/p'
}

require_namespace_absent_or_owned() {
  local namespace="$1"
  if kubectl get namespace "$namespace" >/dev/null 2>&1; then
    local managed
    managed="$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.labels.kloudia\.io/managed-by}' 2>/dev/null || true)"
    [ "$managed" = "infra-spike" ] || die "Namespace exists and is not owned by this spike: $namespace"
  fi
}

create_owned_namespace() {
  local namespace="$1"
  require_namespace_absent_or_owned "$namespace"
  kubectl create namespace "$namespace" --dry-run=client -o yaml \
    | kubectl label --local -f - kloudia.io/managed-by=infra-spike -o yaml \
    | kubectl apply -f -
}

cleanup_namespace_on_success() {
  local namespace="$1"
  if [ "${KLOUDIA_KEEP_RESOURCES:-false}" = "true" ]; then
    info "Keeping namespace because KLOUDIA_KEEP_RESOURCES=true: $namespace"
    return
  fi
  kubectl delete namespace "$namespace" --ignore-not-found=true >/dev/null
}
```

- [ ] **Step 2: Make the helper executable**

Run:

```bash
chmod +x scripts/lib/common.sh
```

Expected: no output.

- [ ] **Step 3: Verify shell syntax**

Run:

```bash
bash -n scripts/lib/common.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 4: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

Expected in the current workspace: `fatal: not a git repository`. If it prints `true`, run:

```bash
git add scripts/lib/common.sh
git commit -m "chore: add infra spike script helpers"
```

## Task 2: Cluster Prerequisite Check

**Files:**
- Create: `scripts/check-cluster-prereqs.sh`

- [ ] **Step 1: Write the prerequisite check script**

Create `scripts/check-cluster-prereqs.sh` with this content:

```bash
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

NAMESPACE="${KLOUDIA_CHECK_NAMESPACE:-kloudia-prereq-check}"
STORAGE_CLASS="${KLOUDIA_STORAGE_CLASS:-}"

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

worker_count="$(
  kubectl get nodes -o jsonpath='{range .items[?(@.metadata.labels.node-role\.kubernetes\.io/control-plane!="")]}{.metadata.name}{"\n"}{end}' \
    | grep -c . || true
)"
if [ "$worker_count" -eq 0 ]; then
  worker_count="$(kubectl get nodes --no-headers | grep -vc 'control-plane' || true)"
fi
[ "$worker_count" -gt 0 ] || die "No worker node candidate found"
info "Worker node candidate count: $worker_count"

if [ -z "$STORAGE_CLASS" ]; then
  STORAGE_CLASS="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{end}' 2>/dev/null || true)"
fi
[ -n "$STORAGE_CLASS" ] || die "No StorageClass selected. Set KLOUDIA_STORAGE_CLASS to the NFS CSI StorageClass name"
info "Using StorageClass: $STORAGE_CLASS"

create_owned_namespace "$NAMESPACE"
trap 'cleanup_namespace_on_success "$NAMESPACE"' EXIT

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
      tolerations:
        - operator: Exists
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
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
```

- [ ] **Step 2: Make the script executable**

Run:

```bash
chmod +x scripts/check-cluster-prereqs.sh
```

Expected: no output.

- [ ] **Step 3: Verify shell syntax**

Run:

```bash
bash -n scripts/check-cluster-prereqs.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 4: Run against the cluster**

Run:

```bash
./scripts/check-cluster-prereqs.sh
```

Expected:

- Prints Kubernetes server version.
- Prints worker node candidate count greater than `0`.
- PVC reaches `Bound`.
- DaemonSet rollout succeeds.
- Logs include `KVM device check passed`.

- [ ] **Step 5: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

If it prints `true`, run:

```bash
git add scripts/check-cluster-prereqs.sh
git commit -m "chore: add cluster prerequisite check"
```

## Task 3: KubeVirt Installer

**Files:**
- Create: `scripts/install-kubevirt.sh`

- [ ] **Step 1: Write the KubeVirt install script**

Create `scripts/install-kubevirt.sh` with this content:

```bash
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

require_cmd curl
require_kubectl

KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-}"
if [ -z "$KUBEVIRT_VERSION" ]; then
  KUBEVIRT_VERSION="$(curl -fsSL https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)"
fi
[ -n "$KUBEVIRT_VERSION" ] || die "Unable to determine KubeVirt version"

info "Installing KubeVirt version: $KUBEVIRT_VERSION"

kubectl apply -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"
kubectl apply -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml"

info "Waiting for KubeVirt to become Available"
kubectl -n kubevirt wait kv kubevirt --for=condition=Available --timeout="${KUBEVIRT_WAIT_TIMEOUT:-600s}"

info "KubeVirt pods:"
kubectl get pods -n kubevirt -o wide

info "KubeVirt installation completed"
```

- [ ] **Step 2: Make the script executable**

Run:

```bash
chmod +x scripts/install-kubevirt.sh
```

Expected: no output.

- [ ] **Step 3: Verify shell syntax**

Run:

```bash
bash -n scripts/install-kubevirt.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 4: Install KubeVirt**

Run:

```bash
./scripts/install-kubevirt.sh
```

Expected:

- `kubectl apply` creates or configures KubeVirt operator resources.
- `kubectl -n kubevirt wait kv kubevirt --for=condition=Available` exits successfully.
- `kubectl get pods -n kubevirt` shows KubeVirt pods running.

- [ ] **Step 5: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

If it prints `true`, run:

```bash
git add scripts/install-kubevirt.sh
git commit -m "chore: add KubeVirt installer"
```

## Task 4: CDI Installer

**Files:**
- Create: `scripts/install-cdi.sh`

- [ ] **Step 1: Write the CDI install script**

Create `scripts/install-cdi.sh` with this content:

```bash
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

require_cmd curl
require_cmd sed
require_kubectl

CDI_VERSION="${CDI_VERSION:-}"
if [ -z "$CDI_VERSION" ]; then
  CDI_VERSION="$(
    curl -fsSL https://api.github.com/repos/kubevirt/containerized-data-importer/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
      | head -n 1
  )"
fi
[ -n "$CDI_VERSION" ] || die "Unable to determine CDI version"

info "Installing CDI version: $CDI_VERSION"

kubectl apply -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml"
kubectl apply -f "https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml"

info "Waiting for CDI deployment to become Available"
kubectl -n cdi wait deployment/cdi-operator --for=condition=Available --timeout="${CDI_WAIT_TIMEOUT:-300s}"
kubectl -n cdi wait cdi cdi --for=condition=Available --timeout="${CDI_WAIT_TIMEOUT:-600s}"

info "CDI pods:"
kubectl get pods -n cdi -o wide

info "CDI installation completed"
```

- [ ] **Step 2: Make the script executable**

Run:

```bash
chmod +x scripts/install-cdi.sh
```

Expected: no output.

- [ ] **Step 3: Verify shell syntax**

Run:

```bash
bash -n scripts/install-cdi.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 4: Install CDI**

Run:

```bash
./scripts/install-cdi.sh
```

Expected:

- `kubectl apply` creates or configures CDI operator resources.
- `kubectl -n cdi wait cdi cdi --for=condition=Available` exits successfully.
- `kubectl get pods -n cdi` shows CDI pods running.

- [ ] **Step 5: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

If it prints `true`, run:

```bash
git add scripts/install-cdi.sh
git commit -m "chore: add CDI installer"
```

## Task 5: VM Smoke Test

**Files:**
- Create: `scripts/smoke-test-vm.sh`

- [ ] **Step 1: Write the smoke test script**

Create `scripts/smoke-test-vm.sh` with this content:

```bash
#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

require_cmd grep
require_kubectl

NAMESPACE="${KLOUDIA_SMOKE_NAMESPACE:-kloudia-smoke}"
STORAGE_CLASS="${KLOUDIA_STORAGE_CLASS:-}"
IMAGE_URL="${KLOUDIA_IMAGE_URL:-https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img}"
VM_NAME="${KLOUDIA_VM_NAME:-smoke-cirros}"
PUBLIC_KEY="${KLOUDIA_PUBLIC_KEY:-ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDkloudiasmoke kloudia-smoke@example.invalid}"

if [ -z "$STORAGE_CLASS" ]; then
  STORAGE_CLASS="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{end}' 2>/dev/null || true)"
fi
[ -n "$STORAGE_CLASS" ] || die "No StorageClass selected. Set KLOUDIA_STORAGE_CLASS to the NFS CSI StorageClass name"

kubectl get namespace kubevirt >/dev/null 2>&1 || die "KubeVirt namespace not found. Run scripts/install-kubevirt.sh first"
kubectl get namespace cdi >/dev/null 2>&1 || die "CDI namespace not found. Run scripts/install-cdi.sh first"

create_owned_namespace "$NAMESPACE"

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
kubectl wait -n "$NAMESPACE" datavolume/${VM_NAME}-rootdisk --for=condition=Ready --timeout="${DATAVOLUME_WAIT_TIMEOUT:-600s}"

cat <<YAML | kubectl apply -n "$NAMESPACE" -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: ${VM_NAME}
  labels:
    kloudia.io/managed-by: infra-spike
spec:
  running: true
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
for _ in $(seq 1 120); do
  if kubectl get vmi -n "$NAMESPACE" "$VM_NAME" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

kubectl get vmi -n "$NAMESPACE" "$VM_NAME" >/dev/null 2>&1 || die "VMI did not appear for VM: $VM_NAME"

info "Waiting for VMI phase Running"
for _ in $(seq 1 180); do
  phase="$(kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [ "$phase" = "Running" ]; then
    break
  fi
  sleep 2
done

phase="$(kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o jsonpath='{.status.phase}')"
[ "$phase" = "Running" ] || die "VMI did not reach Running phase. Current phase: $phase"

info "Checking KubeVirt subresource API discovery"
kubectl get --raw /apis/subresources.kubevirt.io/v1 | grep -E '"name":"virtualmachineinstances/(vnc|console)"' >/dev/null

info "VM status:"
kubectl get vm,vmi,pvc,dv -n "$NAMESPACE" -o wide

info "Recent namespace events:"
kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp | tail -n 20

if [ "${KLOUDIA_KEEP_RESOURCES:-true}" = "true" ]; then
  info "Keeping smoke test resources. Delete with: kubectl delete namespace $NAMESPACE"
else
  cleanup_namespace_on_success "$NAMESPACE"
fi

info "Smoke test completed"
```

- [ ] **Step 2: Make the script executable**

Run:

```bash
chmod +x scripts/smoke-test-vm.sh
```

Expected: no output.

- [ ] **Step 3: Verify shell syntax**

Run:

```bash
bash -n scripts/smoke-test-vm.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 4: Run the VM smoke test**

Run:

```bash
./scripts/smoke-test-vm.sh
```

Expected:

- DataVolume reaches `Ready`.
- VirtualMachineInstance reaches `Running`.
- Subresource API discovery includes `virtualmachineinstances/vnc` or `virtualmachineinstances/console`.
- Final output prints `vm`, `vmi`, `pvc`, and `dv` resources.

- [ ] **Step 5: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

If it prints `true`, run:

```bash
git add scripts/smoke-test-vm.sh
git commit -m "chore: add KubeVirt VM smoke test"
```

## Task 6: Infra Spike Runbook

**Files:**
- Create: `docs/infra/kubevirt-infra-spike.md`

- [ ] **Step 1: Create the runbook directory**

Run:

```bash
mkdir -p docs/infra
```

Expected: no output.

- [ ] **Step 2: Write the runbook**

Create `docs/infra/kubevirt-infra-spike.md` with this content:

```markdown
# KubeVirt Infra Spike Runbook

This runbook validates whether the existing VM-based Kubernetes cluster can run KubeVirt workloads for Kloudia.

## Required Inputs

- A working `kubectl` context pointed at the target cluster.
- A dynamic NFS CSI StorageClass name.
- Worker nodes with nested virtualization exposed.

## 1. Check Cluster Prerequisites

```bash
./scripts/check-cluster-prereqs.sh
```

Success criteria:

- Kubernetes API is reachable.
- At least one worker node candidate exists.
- Test PVC reaches `Bound`.
- Privileged DaemonSet runs on worker nodes.
- Logs include `KVM device check passed`.

## 2. Install KubeVirt

```bash
./scripts/install-kubevirt.sh
```

Use a pinned version when repeatability is required:

```bash
KUBEVIRT_VERSION=v1.8.3 ./scripts/install-kubevirt.sh
```

Success criteria:

- `kubectl -n kubevirt wait kv kubevirt --for=condition=Available` succeeds.
- KubeVirt pods are running in the `kubevirt` namespace.

## 3. Install CDI

```bash
./scripts/install-cdi.sh
```

Use a pinned version when repeatability is required:

```bash
CDI_VERSION=v1.62.0 ./scripts/install-cdi.sh
```

Success criteria:

- `kubectl -n cdi wait cdi cdi --for=condition=Available` succeeds.
- CDI pods are running in the `cdi` namespace.

## 4. Run VM Smoke Test

```bash
./scripts/smoke-test-vm.sh
```

Success criteria:

- CDI DataVolume reaches `Ready`.
- KubeVirt VirtualMachineInstance reaches `Running`.
- KubeVirt subresource API discovery includes console or VNC resources.
- Final output shows VM, VMI, PVC, and DataVolume resources.

## Cleanup

The smoke test keeps resources by default for inspection.

```bash
kubectl delete namespace kloudia-smoke
```

The prerequisite check deletes its temporary namespace unless `KLOUDIA_KEEP_RESOURCES=true` is set.

## Failure Interpretation

- PVC timeout: verify NFS CSI provisioner, StorageClass name, and default storage permissions.
- DaemonSet scheduling failure: verify worker labels, taints, and privileged workload policy.
- `/dev/kvm` failure: verify nested virtualization exposure on every worker VM.
- KubeVirt unavailable: inspect `kubectl get pods -n kubevirt` and `kubectl describe kv -n kubevirt kubevirt`.
- CDI import failure: inspect DataVolume events and confirm cluster egress to the image URL.
- VMI not Running: inspect `kubectl describe vmi -n kloudia-smoke smoke-cirros` and worker node KVM status.
```

- [ ] **Step 3: Verify markdown references**

Run:

```bash
grep -R "scripts/check-cluster-prereqs.sh" docs/infra/kubevirt-infra-spike.md
grep -R "scripts/smoke-test-vm.sh" docs/infra/kubevirt-infra-spike.md
```

Expected: both commands print matching lines.

- [ ] **Step 4: Commit if Git is available**

Run:

```bash
git rev-parse --is-inside-work-tree
```

If it prints `true`, run:

```bash
git add docs/infra/kubevirt-infra-spike.md
git commit -m "docs: add KubeVirt infra spike runbook"
```

## Task 7: Final Verification

**Files:**
- Verify: `scripts/lib/common.sh`
- Verify: `scripts/check-cluster-prereqs.sh`
- Verify: `scripts/install-kubevirt.sh`
- Verify: `scripts/install-cdi.sh`
- Verify: `scripts/smoke-test-vm.sh`
- Verify: `docs/infra/kubevirt-infra-spike.md`

- [ ] **Step 1: Run shell syntax checks**

Run:

```bash
bash -n scripts/lib/common.sh
bash -n scripts/check-cluster-prereqs.sh
bash -n scripts/install-kubevirt.sh
bash -n scripts/install-cdi.sh
bash -n scripts/smoke-test-vm.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 2: Confirm executable bits**

Run:

```bash
test -x scripts/check-cluster-prereqs.sh
test -x scripts/install-kubevirt.sh
test -x scripts/install-cdi.sh
test -x scripts/smoke-test-vm.sh
```

Expected: no output and exit code `0`.

- [ ] **Step 3: Run cluster verification in order**

Run:

```bash
./scripts/check-cluster-prereqs.sh
./scripts/install-kubevirt.sh
./scripts/install-cdi.sh
./scripts/smoke-test-vm.sh
```

Expected:

- Prerequisite check completes.
- KubeVirt condition `Available` is true.
- CDI condition `Available` is true.
- Smoke VM reaches `Running`.

- [ ] **Step 4: Capture final cluster evidence**

Run:

```bash
kubectl get pods -n kubevirt -o wide
kubectl get pods -n cdi -o wide
kubectl get vm,vmi,pvc,dv -n kloudia-smoke -o wide
kubectl get events -n kloudia-smoke --sort-by=.lastTimestamp | tail -n 30
```

Expected: output is sufficient to decide whether Portal MVP planning can begin.
