package store

import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/kloudia/kloudia/backend/internal/domain"
)

var (
	// ErrSubnetCIDRInvalid is returned when a subnet CIDR cannot be parsed.
	ErrSubnetCIDRInvalid = errors.New("subnet cidr is invalid")
	// ErrSubnetCIDROverlap is returned when a subnet CIDR overlaps another subnet in the same VPC.
	ErrSubnetCIDROverlap = errors.New("subnet cidr overlaps an existing subnet in the same vpc")
	// ErrSubnetInUse is returned when deleting a subnet that still has VMs attached.
	ErrSubnetInUse = errors.New("subnet still has virtual machines attached")
	// ErrNetworkInUse is returned when deleting a VPC that still has subnets.
	ErrNetworkInUse = errors.New("network still has subnets")
	// ErrNotFound is returned when a network or subnet does not exist.
	ErrNotFound = errors.New("not found")
)

// normalizeCIDR validates a CIDR and returns its canonical masked form (e.g. 10.0.5.7/24 -> 10.0.5.0/24).
func normalizeCIDR(cidr string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return "", ErrSubnetCIDRInvalid
	}
	return prefix.Masked().String(), nil
}

// defaultGateway returns the first usable host address of a CIDR (network address + 1).
func defaultGateway(cidr string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return "", ErrSubnetCIDRInvalid
	}
	return prefix.Masked().Addr().Next().String(), nil
}

// cidrsOverlap reports whether two CIDRs share any address. Invalid inputs are treated as non-overlapping.
func cidrsOverlap(a, b string) bool {
	pa, err := netip.ParsePrefix(a)
	if err != nil {
		return false
	}
	pb, err := netip.ParsePrefix(b)
	if err != nil {
		return false
	}
	return pa.Overlaps(pb)
}

func makeResourceID(parts ...string) string {
	id := strings.ToLower(strings.TrimSpace(strings.Join(parts, "-")))
	id = vmNameSanitizer.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		return "net"
	}
	return id
}

func (p *Postgres) ListNetworks(ctx context.Context, projectID string) ([]domain.Network, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, project_id, name, ovn_vpc_name, status, created_at
FROM networks
WHERE project_id = $1
ORDER BY created_at DESC, name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var networks []domain.Network
	for rows.Next() {
		var n domain.Network
		if err := rows.Scan(&n.ID, &n.ProjectID, &n.Name, &n.OVNVPCName, &n.Status, &n.CreatedAt); err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

func (p *Postgres) GetNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error) {
	var n domain.Network
	err := p.pool.QueryRow(ctx, `
SELECT id, project_id, name, ovn_vpc_name, status, created_at
FROM networks
WHERE project_id = $1 AND id = $2`, projectID, networkID).
		Scan(&n.ID, &n.ProjectID, &n.Name, &n.OVNVPCName, &n.Status, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Network{}, ErrNotFound
	}
	return n, err
}

func (p *Postgres) CreateNetwork(ctx context.Context, projectID string, name string) (domain.Network, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Network{}, errors.New("network name is required")
	}
	id := makeResourceID(projectID, name)
	ovnVPCName := "kloudia-vpc-" + id
	var n domain.Network
	err := p.pool.QueryRow(ctx, `
INSERT INTO networks (id, project_id, name, ovn_vpc_name, status)
VALUES ($1, $2, $3, $4, 'Ready')
RETURNING id, project_id, name, ovn_vpc_name, status, created_at`, id, projectID, name, ovnVPCName).
		Scan(&n.ID, &n.ProjectID, &n.Name, &n.OVNVPCName, &n.Status, &n.CreatedAt)
	return n, err
}

func (p *Postgres) DeleteNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error) {
	n, err := p.GetNetwork(ctx, projectID, networkID)
	if err != nil {
		return domain.Network{}, err
	}
	var subnetCount int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM subnets WHERE network_id = $1`, networkID).Scan(&subnetCount); err != nil {
		return domain.Network{}, err
	}
	if subnetCount > 0 {
		return domain.Network{}, ErrNetworkInUse
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM networks WHERE id = $1`, networkID); err != nil {
		return domain.Network{}, err
	}
	return n, nil
}
