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
	// The NAT gateway normally goes with the VPC's last public subnet; deleting it here also covers a
	// gateway left behind by a failed subnet deletion.
	if err := p.applier.Delete(ctx, natGatewayRef(network.OVNVPCName)); err != nil {
		return err
	}
	return p.applier.Delete(ctx, BuildVPCResource(network))
}

// CreateSubnet applies the subnet and its NAD. A public subnet also gets SNAT egress through the VPC's shared
// NAT gateway, which is created when the subnet is the gateway anchor. publicSubnets are the VPC's public
// subnets including this one; they set the VPC egress policy routes.
func (p *NetworkProvisioner) CreateSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet, publicSubnets []domain.Subnet) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	if err := p.applier.Apply(ctx, BuildSubnetResource(p.namespace, ovnVPCName, subnet)); err != nil {
		return err
	}
	if err := p.applier.Apply(ctx, BuildNADResource(p.namespace, subnet)); err != nil {
		return err
	}
	if subnet.Type != "public" {
		return nil
	}
	if anchor, ok := domain.NATGatewayAnchor(publicSubnets); ok && anchor.ID == subnet.ID {
		gateway, err := BuildNATGatewayResource(p.namespace, ovnVPCName, p.externalNetwork, subnet)
		if err != nil {
			return err
		}
		if err := p.applier.Apply(ctx, gateway); err != nil {
			return err
		}
	}
	egress := BuildSubnetEgressResources(ovnVPCName, p.externalNetwork, subnet)
	if err := p.applier.Apply(ctx, egress.EIP); err != nil {
		return err
	}
	if err := p.applier.Apply(ctx, egress.SnatRule); err != nil {
		return err
	}
	// Route to the gateway only once the SNAT rule is in place, so no traffic leaves un-NATed.
	return p.patchEgressRoutes(ctx, ovnVPCName, publicSubnets)
}

// DeleteSubnet removes the subnet's resources. remainingPublicSubnets are the VPC's public subnets left after
// this one is gone; when none remain, the shared NAT gateway is removed as well.
func (p *NetworkProvisioner) DeleteSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet, remainingPublicSubnets []domain.Subnet) error {
	if p == nil || p.applier == nil {
		return errors.New("kubevirt applier is not configured")
	}
	if subnet.Type == "public" {
		if err := p.patchEgressRoutes(ctx, ovnVPCName, remainingPublicSubnets); err != nil {
			return err
		}
		egress := BuildSubnetEgressResources(ovnVPCName, p.externalNetwork, subnet)
		_ = p.applier.Delete(ctx, egress.SnatRule)
		_ = p.applier.Delete(ctx, egress.EIP)
		if len(remainingPublicSubnets) == 0 {
			if err := p.applier.Delete(ctx, natGatewayRef(ovnVPCName)); err != nil {
				return err
			}
		}
	}
	if err := p.applier.Delete(ctx, BuildNADResource(p.namespace, subnet)); err != nil {
		return err
	}
	return p.applier.Delete(ctx, BuildSubnetResource(p.namespace, ovnVPCName, subnet))
}

// patchEgressRoutes reroutes every public subnet's traffic, by source, to the NAT gateway's LAN address on
// the anchor subnet, or clears the egress policies when there are no public subnets.
func (p *NetworkProvisioner) patchEgressRoutes(ctx context.Context, ovnVPCName string, publicSubnets []domain.Subnet) error {
	anchor, ok := domain.NATGatewayAnchor(publicSubnets)
	if !ok {
		return p.applier.Patch(ctx, BuildVPCEgressRoutePatch(ovnVPCName, "", nil))
	}
	lanIP, err := natGatewayLanIP(anchor)
	if err != nil {
		return err
	}
	cidrs := make([]string, 0, len(publicSubnets))
	for _, s := range publicSubnets {
		cidrs = append(cidrs, s.CIDR)
	}
	return p.applier.Patch(ctx, BuildVPCEgressRoutePatch(ovnVPCName, lanIP, cidrs))
}
