package kubevirt

import (
	"context"
	"errors"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type Applier interface {
	Apply(ctx context.Context, resource map[string]any) error
	Delete(ctx context.Context, resource map[string]any) error
	SetVirtualMachineRunStrategy(ctx context.Context, namespace string, name string, runStrategy string) error
}

type Provisioner struct {
	namespace        string
	storageClassName string
	applier          Applier
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

func (p *Provisioner) StopVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.SetVirtualMachineRunStrategy(ctx, p.namespace, vm.ID, "Halted")
}
