# kube-ovn and Multus for a dev cluster

Kloudia VPCs need Multus and kube-ovn running as a secondary CNI next to Cilium. Cilium must run with
`cni-exclusive=false` so the Multus config is not removed.

```bash
# Multus (thick plugin)
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/v4.3.1/deployments/multus-daemonset-thick.yml
# The manifest limits the daemon to 50Mi, which gets OOMKilled when a rebooted node recreates all its pods at
# once; pods then hang in ContainerCreating with "multus-shim ... failed to send CNI request".
kubectl -n kube-system patch ds kube-multus-ds --type=json -p '[{"op":"replace","path":"/spec/template/spec/containers/0/resources","value":{"requests":{"cpu":"100m","memory":"100Mi"},"limits":{"cpu":"500m","memory":"500Mi"}}}]'

# kube-ovn: ovn-central runs on nodes labeled kube-ovn/role=master (normally the control plane)
kubectl label node <control-plane-node> kube-ovn/role=master
helm repo add kubeovn https://kubeovn.github.io/kube-ovn/
helm install kube-ovn kubeovn/kube-ovn --version v1.16.14 -n kube-system -f values.yaml

# External network for public-subnet NAT gateways
kubectl apply -f external-network.yaml
```

Adjust before applying:

- `values.yaml`: `networking.IFACE` is the node uplink NIC.
- `external-network.yaml`: the NAD `master` NIC, the node network `cidrBlock` and `gateway`, and `excludeIps`.
  Only the IPs left out of `excludeIps` are handed out as NAT gateway EIPs (one per VPC with a public subnet),
  so they must not be used by anything else on the node network.

Then run the API with `KLOUDIA_NETWORK_PROVISIONING_ENABLED=true`, add the node network to
`KLOUDIA_RESERVED_CIDRS`, and set `KLOUDIA_VM_DNS_SERVERS` to a resolver the VPC can reach.

The control plane needs room for ovn-central and the per-node ovs-ovn and kube-ovn-cni pods (several hundred
MiB of memory requests). A 2 vCPU / 4GB control plane that also runs the KubeVirt control pods is too small.
