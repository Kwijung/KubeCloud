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
	// ErrSubnetHostsNATGateway is returned when deleting the public subnet that hosts the VPC NAT gateway while other public subnets still depend on it.
	ErrSubnetHostsNATGateway = errors.New("subnet hosts the vpc nat gateway used by other public subnets")
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

func (p *Postgres) ListSubnets(ctx context.Context, projectID string, networkID string) ([]domain.Subnet, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at
FROM subnets
WHERE project_id = $1 AND network_id = $2
ORDER BY created_at DESC, name`, projectID, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subnets []domain.Subnet
	for rows.Next() {
		var s domain.Subnet
		if err := rows.Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		subnets = append(subnets, s)
	}
	return subnets, rows.Err()
}

func (p *Postgres) GetSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	var s domain.Subnet
	err := p.pool.QueryRow(ctx, `
SELECT id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at
FROM subnets
WHERE project_id = $1 AND id = $2`, projectID, subnetID).
		Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subnet{}, ErrNotFound
	}
	return s, err
}

func (p *Postgres) CreateSubnet(ctx context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Subnet{}, errors.New("subnet name is required")
	}
	canonicalCIDR, err := normalizeCIDR(cidr)
	if err != nil {
		return domain.Subnet{}, err
	}
	if strings.TrimSpace(gateway) == "" {
		gateway, err = defaultGateway(canonicalCIDR)
		if err != nil {
			return domain.Subnet{}, err
		}
	} else if !isUsableHostGateway(canonicalCIDR, gateway) {
		return domain.Subnet{}, ErrSubnetCIDRInvalid
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Subnet{}, err
	}
	defer tx.Rollback(ctx)

	// Lock the parent network row to serialize concurrent subnet creation within the same VPC.
	var networkID2 string
	if err := tx.QueryRow(ctx, `
SELECT id FROM networks WHERE project_id = $1 AND id = $2
FOR UPDATE`, projectID, networkID).Scan(&networkID2); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subnet{}, ErrNotFound
		}
		return domain.Subnet{}, err
	}

	// Reject overlap with existing subnets in the same VPC.
	rows, err := tx.Query(ctx, `SELECT cidr FROM subnets WHERE network_id = $1`, networkID)
	if err != nil {
		return domain.Subnet{}, err
	}
	var existingCIDRs []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return domain.Subnet{}, err
		}
		existingCIDRs = append(existingCIDRs, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Subnet{}, err
	}
	for _, c := range existingCIDRs {
		if cidrsOverlap(canonicalCIDR, c) {
			return domain.Subnet{}, ErrSubnetCIDROverlap
		}
	}

	id := makeResourceID(networkID, name)
	ovnSubnetName := "kloudia-subnet-" + id
	nadName := "kloudia-net-" + id
	var s domain.Subnet
	err = tx.QueryRow(ctx, `
INSERT INTO subnets (id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'Ready')
RETURNING id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at`,
		id, networkID, projectID, name, canonicalCIDR, gateway, subnetType, ovnSubnetName, nadName).
		Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt)
	if err != nil {
		return domain.Subnet{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Subnet{}, err
	}
	return s, nil
}

func (p *Postgres) DeleteSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Subnet{}, err
	}
	defer tx.Rollback(ctx)

	var s domain.Subnet
	err = tx.QueryRow(ctx, `
SELECT id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at
FROM subnets
WHERE project_id = $1 AND id = $2
FOR UPDATE`, projectID, subnetID).
		Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subnet{}, ErrNotFound
	}
	if err != nil {
		return domain.Subnet{}, err
	}

	var vmCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM virtual_machines WHERE subnet_id = $1`, subnetID).Scan(&vmCount); err != nil {
		return domain.Subnet{}, err
	}
	if vmCount > 0 {
		return domain.Subnet{}, ErrSubnetInUse
	}

	if s.Type == "public" {
		// Lock the parent network row so a concurrent public subnet creation cannot slip past this check.
		if _, err := tx.Exec(ctx, `SELECT id FROM networks WHERE id = $1 FOR UPDATE`, s.NetworkID); err != nil {
			return domain.Subnet{}, err
		}
		publicSubnets, err := listPublicSubnets(ctx, tx, s.NetworkID)
		if err != nil {
			return domain.Subnet{}, err
		}
		if anchor, ok := domain.NATGatewayAnchor(publicSubnets); ok && anchor.ID == s.ID && len(publicSubnets) > 1 {
			return domain.Subnet{}, ErrSubnetHostsNATGateway
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM subnets WHERE id = $1`, subnetID); err != nil {
		return domain.Subnet{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Subnet{}, err
	}
	return s, nil
}

func listPublicSubnets(ctx context.Context, tx pgx.Tx, networkID string) ([]domain.Subnet, error) {
	rows, err := tx.Query(ctx, `
SELECT id, type, created_at FROM subnets
WHERE network_id = $1 AND type = 'public'`, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subnets []domain.Subnet
	for rows.Next() {
		var s domain.Subnet
		if err := rows.Scan(&s.ID, &s.Type, &s.CreatedAt); err != nil {
			return nil, err
		}
		subnets = append(subnets, s)
	}
	return subnets, rows.Err()
}

// isUsableHostGateway reports whether host is a valid gateway address within cidr —
// i.e. it is inside the prefix but is neither the network address nor the broadcast address.
// Only IPv4 is supported.
func isUsableHostGateway(cidr string, host string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is4() || !prefix.Contains(addr) {
		return false
	}
	network := prefix.Masked().Addr()
	if addr == network {
		return false // network address is not a usable host
	}
	if addr == broadcastAddr(prefix) {
		return false // broadcast address is not a usable host
	}
	return true
}

// broadcastAddr returns the last (broadcast) address of an IPv4 prefix.
func broadcastAddr(prefix netip.Prefix) netip.Addr {
	b := prefix.Masked().Addr().As4()
	hostBits := 32 - prefix.Bits()
	for i := 0; i < hostBits; i++ {
		byteIdx := 3 - i/8
		bitIdx := uint(i % 8)
		b[byteIdx] |= 1 << bitIdx
	}
	return netip.AddrFrom4(b)
}

func (p *Postgres) DeleteNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Network{}, err
	}
	defer tx.Rollback(ctx)

	var n domain.Network
	err = tx.QueryRow(ctx, `
SELECT id, project_id, name, ovn_vpc_name, status, created_at
FROM networks
WHERE project_id = $1 AND id = $2
FOR UPDATE`, projectID, networkID).
		Scan(&n.ID, &n.ProjectID, &n.Name, &n.OVNVPCName, &n.Status, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Network{}, ErrNotFound
	}
	if err != nil {
		return domain.Network{}, err
	}

	var subnetCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM subnets WHERE network_id = $1`, networkID).Scan(&subnetCount); err != nil {
		return domain.Network{}, err
	}
	if subnetCount > 0 {
		return domain.Network{}, ErrNetworkInUse
	}

	if _, err := tx.Exec(ctx, `DELETE FROM networks WHERE id = $1`, networkID); err != nil {
		return domain.Network{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Network{}, err
	}
	return n, nil
}
