# KubeVirt Infra Spike Runbook

This runbook validates that the current Kubernetes cluster can install KubeVirt and CDI, import a minimal VM disk, start a KubeVirt VM, and expose KubeVirt subresource discovery for future console work.

Run commands from the repository root.

## Required Inputs

- A working `kubectl` context pointed at the target cluster.
- A dynamic StorageClass:
  - Prefer the NFS CSI StorageClass intended for KubeVirt PVCs.
  - If `KLOUDIA_STORAGE_CLASS` is not set, the scripts use the first default StorageClass.
- Worker nodes with nested virtualization enabled:
  - Worker nodes must be schedulable.
  - Worker nodes must expose `/dev/kvm` and `/dev/net/tun` to privileged pods.
  - The `vhost_net` kernel module visibility is reported by the prereq check, but the script does not fail only because it is not visible.

## Step 1: Check Cluster Prerequisites

Run:

```bash
./scripts/check-cluster-prereqs.sh
```

To force a StorageClass:

```bash
KLOUDIA_STORAGE_CLASS=<name> ./scripts/check-cluster-prereqs.sh
```

Optional environment:

- `KLOUDIA_CHECK_NAMESPACE`: namespace for temporary prereq resources. Default: `kloudia-prereq-check`.
- `KLOUDIA_STORAGE_CLASS`: StorageClass used for the PVC provisioning check. Default: first default StorageClass.
- `KLOUDIA_KEEP_RESOURCES`: keeps prereq resources after a successful run when set to `true`. Default: unset, so successful runs delete the `kvm-device-check` DaemonSet and `pvc-provisioning-check` PVC after verifying they are labeled `kloudia.io/managed-by=infra-spike`; a script-created prereq namespace is also deleted on success.

Success criteria:

- `kubectl` can reach the cluster.
- At least one worker candidate exists, excluding `node-role.kubernetes.io/control-plane` and `node-role.kubernetes.io/master`.
- A StorageClass is selected.
- The 1 Gi `pvc-provisioning-check` PVC reaches `Bound` within 180 seconds.
- The privileged `kvm-device-check` DaemonSet rolls out on worker nodes within 180 seconds.
- Operators should see `KVM device check passed` in the DaemonSet logs printed by the script. The script primarily verifies DaemonSet rollout, then prints logs for confirmation.

Cleanup and failure behavior:

- The script creates or reuses `kloudia-prereq-check` only if the namespace is absent or has label `kloudia.io/managed-by=infra-spike`.
- On success, the script deletes the owned `kvm-device-check` DaemonSet and `pvc-provisioning-check` PVC after verifying each resource has label `kloudia.io/managed-by=infra-spike`.
- On success, a namespace created by the script is deleted by default. A pre-existing prereq namespace is left in place.
- Set `KLOUDIA_KEEP_RESOURCES=true` to keep the namespace and prereq resources. Warning: this keeps the privileged hostPath `kvm-device-check` DaemonSet running until you delete it manually.
- On failure, the prereq namespace is preserved for inspection.

Useful inspection commands:

```bash
kubectl get events -n kloudia-prereq-check --sort-by=.lastTimestamp
kubectl get pvc,ds,pods -n kloudia-prereq-check -o wide
kubectl logs -n kloudia-prereq-check -l app=kvm-device-check --tail=-1
```

## Step 2: Install KubeVirt

Run:

```bash
./scripts/install-kubevirt.sh
```

To pin the version used by this spike:

```bash
KUBEVIRT_VERSION=v1.8.3 ./scripts/install-kubevirt.sh
```

Optional environment:

- `KUBEVIRT_VERSION`: KubeVirt release tag to apply. If unset, the script fetches the current stable release from KubeVirt.
- `KUBEVIRT_WAIT_TIMEOUT`: wait timeout for `kv/kubevirt` to become `Available`. Default: `600s`.

Success criteria:

- The KubeVirt operator manifest applies successfully.
- The KubeVirt CR manifest applies successfully.
- `kv/kubevirt` in namespace `kubevirt` reaches condition `Available`.
- `kubectl get pods -n kubevirt -o wide` succeeds.
- The script prints `KubeVirt installation completed`.

Existing-install behavior:

- If `kv/kubevirt` already exists and `KUBEVIRT_VERSION` is unset, the script is a no-op. It prints current KubeVirt status and pods, then exits `0`.
- A no-op exit means the install already exists, not necessarily that it is healthy. Inspect the printed status before treating KubeVirt as ready, or run:

```bash
kubectl -n kubevirt wait kv/kubevirt --for=condition=Available --timeout=600s
```

- If `kv/kubevirt` already exists and `KUBEVIRT_VERSION` is set, the script applies the operator and CR manifests for that explicit version. Treat this as a reconcile or possible upgrade.

## Step 3: Install CDI

Run:

```bash
./scripts/install-cdi.sh
```

To pin the version:

```bash
CDI_VERSION=v1.62.0 ./scripts/install-cdi.sh
```

Optional environment:

- `CDI_VERSION`: CDI release tag to apply, for example `v1.62.0`. If unset, the script fetches the latest GitHub release tag.
- `CDI_WAIT_TIMEOUT`: wait timeout used for CDI waits. Default: `300s` for `deployment/cdi-operator`, `600s` for `cdi/cdi`.

Success criteria:

- The CDI operator manifest applies successfully.
- The CDI CR manifest applies successfully.
- `deployment/cdi-operator` in namespace `cdi` reaches condition `Available`.
- `cdi/cdi` in namespace `cdi` reaches condition `Available`.
- `kubectl get pods -n cdi -o wide` succeeds.
- The script prints `CDI installation completed`.

Existing-install behavior:

- If `cdi/cdi` already exists and `CDI_VERSION` is unset, the script is a no-op. It prints current CDI status and pods, then exits `0`.
- A no-op exit means the install already exists, not necessarily that it is healthy. Inspect the printed status before treating CDI as ready, or run:

```bash
kubectl -n cdi wait cdi/cdi --for=condition=Available --timeout=600s
```

- If `cdi/cdi` already exists and `CDI_VERSION` is set, the script applies the operator and CR manifests for that explicit version. Treat this as a reconcile or possible upgrade.

## Step 4: Run the Smoke Test VM

Run:

```bash
./scripts/smoke-test-vm.sh
```

Common pinned run:

```bash
KLOUDIA_STORAGE_CLASS=<name> ./scripts/smoke-test-vm.sh
```

Optional environment:

- `KLOUDIA_SMOKE_NAMESPACE`: namespace for smoke resources. Default: `kloudia-smoke`.
- `KLOUDIA_STORAGE_CLASS`: StorageClass for the VM root disk DataVolume. Default: first default StorageClass.
- `KLOUDIA_IMAGE_URL`: HTTP(S) image imported by CDI. Default: Cirros `0.6.2` x86_64 disk image.
- `KLOUDIA_VM_NAME`: VM and VMI name. Default: `smoke-cirros`.
- `KLOUDIA_PUBLIC_KEY`: single-line SSH public key injected into the cloud-init manifest. Default: throwaway smoke-test key.
- `DATAVOLUME_WAIT_TIMEOUT`: wait timeout for the DataVolume to become `Ready`. Default: `600s`.
- `KLOUDIA_KEEP_RESOURCES`: smoke resources are kept by default. Set `KLOUDIA_KEEP_RESOURCES=false` to delete the smoke namespace after a successful run.

Success criteria:

- Names and single-line values pass script validation.
- Namespaces `kubevirt` and `cdi` exist.
- The smoke namespace is created or reused only if it is owned by this spike.
- Before rerun, the script checks only same-name `vm`, `vmi`, `datavolume`, and `pvc` resources, verifies each existing resource has label `kloudia.io/managed-by=infra-spike`, and deletes those same-name resources. It does not broadly clean the namespace.
- The CDI DataVolume `${KLOUDIA_VM_NAME}-rootdisk` reaches condition `Ready`.
- The VirtualMachine is created with a cloud-init `NoCloud` disk containing the configured SSH public key.
- A VMI appears and reaches phase `Running`.
- Raw API discovery for `/apis/subresources.kubevirt.io/v1` includes `virtualmachineinstances/vnc` or `virtualmachineinstances/console`.
- The script prints VM, VMI, PVC, DataVolume status and recent events, then prints `Smoke test completed`.

Cleanup behavior:

- By default, the script keeps the smoke namespace and resources for inspection.
- To delete smoke resources automatically after a successful run:

```bash
KLOUDIA_KEEP_RESOURCES=false ./scripts/smoke-test-vm.sh
```

- `KLOUDIA_KEEP_RESOURCES=false` deletes the smoke namespace only on success.
- On failure, the script prints diagnostics and preserves the smoke namespace for inspection.
- Manual cleanup deletes the entire namespace and everything in it, not only the smoke VM resources:

```bash
kubectl delete namespace kloudia-smoke
```

If you used a custom namespace:

```bash
kubectl delete namespace <KLOUDIA_SMOKE_NAMESPACE>
```

Cluster-wide install cleanup:

- These scripts do not uninstall KubeVirt or CDI.
- Uninstall is intentionally separate. Follow upstream or manual cleanup only after confirming no VM workloads, DataVolumes, PVCs, or controllers depend on the KubeVirt/CDI installation.

## Failure Interpretation

PVC timeout:

- Seen in Step 1 when `pvc-provisioning-check` does not reach `Bound`, or in Step 4 when the DataVolume/PVC cannot bind.
- Check the selected StorageClass name, default StorageClass annotation, NFS CSI controller pods, provisioner events, and PVC events.
- The cluster is not ready for KubeVirt disk-backed VM testing until dynamic provisioning succeeds.

DaemonSet scheduling or KVM failure:

- Seen in Step 1 when `daemonset/kvm-device-check` does not roll out or logs do not include the success line.
- If pods are Pending, inspect node labels, taints, tolerations, and worker schedulability.
- If pods start but fail, inspect whether worker hosts expose `/dev/kvm` and `/dev/net/tun` inside privileged pods.
- Nested virtualization must be enabled on the VM hosts before KubeVirt can run hardware-accelerated VM workloads.

KubeVirt unavailable:

- Seen in Step 2 when `kv/kubevirt` does not become `Available`.
- Inspect KubeVirt pods, deployments, events, and operator logs in namespace `kubevirt`.
- Common causes are missing KVM support, unsupported cluster configuration, image pull failures, or operator reconciliation errors.

CDI unavailable or import failure:

- Seen in Step 3 when CDI resources do not become `Available`, or in Step 4 when the DataVolume does not become `Ready`.
- For CDI availability, inspect namespace `cdi` pods, events, and operator logs.
- For import failure, inspect the DataVolume, PVC, importer pod, image URL reachability, storage capacity, and StorageClass binding behavior.

VMI not Running:

- Seen in Step 4 after the VM is applied but the VMI never reaches `Running`.
- Inspect `kubectl describe vmi`, `kubectl describe vm`, virt-launcher pod events, node placement, KubeVirt handler pods, KVM device access, and PVC attachment.
- A VMI stuck before `Running` means the cluster cannot yet start the minimal VM workload reliably.

Subresource discovery failure:

- Seen in Step 4 when `/apis/subresources.kubevirt.io/v1` does not advertise VNC or console subresources.
- Confirm the KubeVirt API components are healthy and that the aggregated API is registered.
- This blocks later API-proxy or web-console work even if the VMI itself reaches `Running`.

## Explicit Limitations

- The smoke test validates that the VM manifest includes cloud-init SSH key injection. It does not verify in-guest SSH access.
- The smoke test validates KubeVirt subresource API discovery. It does not validate a protocol-level web console, VNC stream, serial console stream, `virtctl`, or browser console integration.
- The smoke test uses pod masquerade networking and a Cirros image for infra validation only. It is not a production VM networking, image, or access pattern.
- Unpinned installs fetch current upstream releases at runtime. Pin `KUBEVIRT_VERSION` and `CDI_VERSION` when operators need reproducible install inputs.
