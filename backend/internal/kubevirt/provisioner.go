package kubevirt

import (
	"context"
	"errors"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type Applier interface {
	Apply(ctx context.Context, resource map[string]any) error
	Delete(ctx context.Context, resource map[string]any) error
	// Patch merge-patches resource onto the existing object with the same name.
	Patch(ctx context.Context, resource map[string]any) error
	SetVirtualMachineRunStrategy(ctx context.Context, namespace string, name string, runStrategy string) error
	// RestartVirtualMachine reboots a running VM through KubeVirt's restart subresource.
	RestartVirtualMachine(ctx context.Context, namespace string, name string) error
}

type Provisioner struct {
	namespace        string
	storageClassName string
	applier          Applier
	subnetDNSServers []string
	diskAccessMode   string
}

// SetDiskAccessMode sets the access mode of VM root disks. ReadWriteMany lets KubeVirt live-migrate a VM
// when its storage class supports it; empty keeps ReadWriteOnce.
func (p *Provisioner) SetDiskAccessMode(mode string) {
	p.diskAccessMode = mode
}

// SetSubnetDNSServers sets the DNS servers handed to VMs attached to a VPC subnet. A VPC is isolated from the
// cluster DNS service, so without them such VMs get an unreachable resolver. Empty keeps the cluster DNS.
func (p *Provisioner) SetSubnetDNSServers(servers []string) {
	p.subnetDNSServers = servers
}

func NewProvisioner(namespace string, storageClassName string, applier Applier) *Provisioner {
	return &Provisioner{
		namespace:        namespace,
		storageClassName: storageClassName,
		applier:          applier,
	}
}

func (p *Provisioner) CreateVirtualMachine(ctx context.Context, plan domain.VirtualMachineProvisioningPlan) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	resources := BuildResources(p.namespace, p.storageClassName, plan)
	if plan.Subnet != nil && len(p.subnetDNSServers) > 0 {
		applyDNSServers(resources.VirtualMachine, p.subnetDNSServers)
	}
	if p.diskAccessMode != "" {
		resources.DataVolume["spec"].(map[string]any)["pvc"].(map[string]any)["accessModes"] = []any{p.diskAccessMode}
	}
	if err := p.applier.Apply(ctx, resources.DataVolume); err != nil {
		return err
	}
	return p.applier.Apply(ctx, resources.VirtualMachine)
}

func (p *Provisioner) DeleteVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	resources := BuildDeleteResources(p.namespace, vm)
	if err := p.applier.Delete(ctx, resources.VirtualMachine); err != nil {
		return err
	}
	return p.applier.Delete(ctx, resources.DataVolume)
}

func (p *Provisioner) StartVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.SetVirtualMachineRunStrategy(ctx, p.namespace, vm.ID, "Always")
}

func (p *Provisioner) RestartVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.RestartVirtualMachine(ctx, p.namespace, vm.ID)
}

func (p *Provisioner) StopVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.SetVirtualMachineRunStrategy(ctx, p.namespace, vm.ID, "Halted")
}
