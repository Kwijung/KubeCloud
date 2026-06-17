package kubevirt

import (
	"context"
	"errors"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type NetworkProvisioner struct {
	namespace       string
	externalNetwork string
	applier         Applier
}

func NewNetworkProvisioner(namespace string, externalNetwork string, applier Applier) *NetworkProvisioner {
	return &NetworkProvisioner{namespace: namespace, externalNetwork: externalNetwork, applier: applier}
}

func (p *NetworkProvisioner) CreateNetwork(ctx context.Context, network domain.Network) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.Apply(ctx, BuildVPCResource(network))
}

func (p *NetworkProvisioner) DeleteNetwork(ctx context.Context, network domain.Network) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	return p.applier.Delete(ctx, BuildVPCResource(network))
}

func (p *NetworkProvisioner) CreateSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	if err := p.applier.Apply(ctx, BuildSubnetResource(p.namespace, ovnVPCName, subnet)); err != nil {
		return err
	}
	if err := p.applier.Apply(ctx, BuildNADResource(p.namespace, subnet)); err != nil {
		return err
	}
	if subnet.Type == "public" {
		egress := BuildPublicEgressResources(p.namespace, ovnVPCName, p.externalNetwork, subnet)
		if err := p.applier.Apply(ctx, egress.NatGateway); err != nil {
			return err
		}
		if err := p.applier.Apply(ctx, egress.EIP); err != nil {
			return err
		}
		if err := p.applier.Apply(ctx, egress.SnatRule); err != nil {
			return err
		}
	}
	return nil
}

func (p *NetworkProvisioner) DeleteSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	if subnet.Type == "public" {
		egress := BuildPublicEgressResources(p.namespace, ovnVPCName, p.externalNetwork, subnet)
		_ = p.applier.Delete(ctx, egress.SnatRule)
		_ = p.applier.Delete(ctx, egress.EIP)
	}
	if err := p.applier.Delete(ctx, BuildNADResource(p.namespace, subnet)); err != nil {
		return err
	}
	return p.applier.Delete(ctx, BuildSubnetResource(p.namespace, ovnVPCName, subnet))
}
