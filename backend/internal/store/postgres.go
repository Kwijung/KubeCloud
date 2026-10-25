package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kloudia/kloudia/backend/internal/domain"
)

var vmNameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(databaseURL string) (*Postgres, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL is required")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	if p == nil || p.pool == nil {
		return
	}
	p.pool.Close()
}

func (p *Postgres) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	return p.pool.Ping(ctx)
}

func (p *Postgres) ApplySchema(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	_, err := p.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  id text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS flavors (
  id text PRIMARY KEY,
  name text NOT NULL,
  cpu integer NOT NULL CHECK (cpu > 0),
  memory_bytes bigint NOT NULL CHECK (memory_bytes > 0),
  root_disk_bytes bigint NOT NULL CHECK (root_disk_bytes > 0),
  enabled boolean NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS images (
  id text PRIMARY KEY,
  project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name text NOT NULL,
  os_type text NOT NULL,
  source_url text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS virtual_machines (
  id text PRIMARY KEY,
  project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name text NOT NULL,
  image_id text NOT NULL REFERENCES images(id),
  flavor_id text NOT NULL REFERENCES flavors(id),
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS networks (
  id text PRIMARY KEY,
  project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name text NOT NULL,
  ovn_vpc_name text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS subnets (
  id text PRIMARY KEY,
  network_id text NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
  project_id text NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name text NOT NULL,
  cidr text NOT NULL,
  gateway text NOT NULL,
  type text NOT NULL,
  ovn_subnet_name text NOT NULL,
  nad_name text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (network_id, name)
);

ALTER TABLE virtual_machines
  ADD COLUMN IF NOT EXISTS subnet_id text REFERENCES subnets(id);

-- NULL for VPCs created before VPC CIDRs existed; their subnets are not range-checked.
ALTER TABLE networks
  ADD COLUMN IF NOT EXISTS cidr text;

INSERT INTO projects (id, name, description)
VALUES
  ('platform', 'Platform', 'Shared platform engineering workspace'),
  ('data', 'Data', 'Analytics and data engineering workspace')
ON CONFLICT (id) DO NOTHING;

INSERT INTO flavors (id, name, cpu, memory_bytes, root_disk_bytes, enabled)
VALUES
  ('small', 'Small', 1, 536870912, 2147483648, true),
  ('medium', 'Medium', 2, 2147483648, 10737418240, true),
  ('large', 'Large', 4, 4294967296, 21474836480, true)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    cpu = EXCLUDED.cpu,
    memory_bytes = EXCLUDED.memory_bytes,
    root_disk_bytes = EXCLUDED.root_disk_bytes,
    enabled = EXCLUDED.enabled;

INSERT INTO images (id, project_id, name, os_type, source_url, status)
VALUES
  ('platform-cirros', 'platform', 'CirrOS smoke image', 'linux', 'https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img', 'Ready'),
  ('data-ubuntu-22', 'data', 'Ubuntu 22.04', 'linux', 'https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img', 'Importing')
ON CONFLICT (id) DO NOTHING;
`)
	return err
}

func (p *Postgres) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, name, description, created_at
FROM projects
ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []domain.Project
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (p *Postgres) ListFlavors(ctx context.Context) ([]domain.Flavor, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, name, cpu, memory_bytes, root_disk_bytes, enabled
FROM flavors
WHERE enabled = true
ORDER BY cpu, memory_bytes, root_disk_bytes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flavors []domain.Flavor
	for rows.Next() {
		var flavor domain.Flavor
		if err := rows.Scan(&flavor.ID, &flavor.Name, &flavor.CPU, &flavor.MemoryBytes, &flavor.RootDiskBytes, &flavor.Enabled); err != nil {
			return nil, err
		}
		flavors = append(flavors, flavor)
	}
	return flavors, rows.Err()
}

func (p *Postgres) ListImages(ctx context.Context, projectID string) ([]domain.Image, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, project_id, name, os_type, source_url, status, created_at
FROM images
WHERE project_id = $1
ORDER BY created_at DESC, name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []domain.Image
	for rows.Next() {
		var image domain.Image
		if err := rows.Scan(&image.ID, &image.ProjectID, &image.Name, &image.OSType, &image.SourceURL, &image.Status, &image.CreatedAt); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

func (p *Postgres) ListVirtualMachines(ctx context.Context, projectID string) ([]domain.VirtualMachine, error) {
	rows, err := p.pool.Query(ctx, `
SELECT id, project_id, name, image_id, flavor_id, subnet_id, status, created_at
FROM virtual_machines
WHERE project_id = $1
ORDER BY created_at DESC, name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vms []domain.VirtualMachine
	for rows.Next() {
		var vm domain.VirtualMachine
		var subnetID *string
		if err := rows.Scan(&vm.ID, &vm.ProjectID, &vm.Name, &vm.ImageID, &vm.FlavorID, &subnetID, &vm.Status, &vm.CreatedAt); err != nil {
			return nil, err
		}
		if subnetID != nil {
			vm.SubnetID = *subnetID
		}
		vms = append(vms, vm)
	}
	return vms, rows.Err()
}

func (p *Postgres) GetVirtualMachine(ctx context.Context, projectID string, vmID string) (domain.VirtualMachine, error) {
	var vm domain.VirtualMachine
	var subnetID *string
	err := p.pool.QueryRow(ctx, `
SELECT id, project_id, name, image_id, flavor_id, subnet_id, status, created_at
FROM virtual_machines
WHERE project_id = $1 AND id = $2`, projectID, vmID).
		Scan(&vm.ID, &vm.ProjectID, &vm.Name, &vm.ImageID, &vm.FlavorID, &subnetID, &vm.Status, &vm.CreatedAt)
	if subnetID != nil {
		vm.SubnetID = *subnetID
	}
	return vm, err
}

func (p *Postgres) DeleteVirtualMachine(ctx context.Context, projectID string, vmID string) error {
	result, err := p.pool.Exec(ctx, `
DELETE FROM virtual_machines
WHERE project_id = $1 AND id = $2`, projectID, vmID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("virtual machine not found")
	}
	return nil
}

func (p *Postgres) CreateVirtualMachine(ctx context.Context, projectID string, input domain.CreateVirtualMachineInput) (domain.VirtualMachineProvisioningPlan, error) {
	name := strings.TrimSpace(input.Name)
	imageID := strings.TrimSpace(input.ImageID)
	flavorID := strings.TrimSpace(input.FlavorID)
	if name == "" || imageID == "" || flavorID == "" {
		return domain.VirtualMachineProvisioningPlan{}, errors.New("name, image ID, and flavor ID are required")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.VirtualMachineProvisioningPlan{}, err
	}
	defer tx.Rollback(ctx)

	var image domain.Image
	if err := tx.QueryRow(ctx, `
SELECT id, project_id, name, os_type, source_url, status, created_at
FROM images
WHERE id = $1 AND project_id = $2`, imageID, projectID).
		Scan(&image.ID, &image.ProjectID, &image.Name, &image.OSType, &image.SourceURL, &image.Status, &image.CreatedAt); err != nil {
		return domain.VirtualMachineProvisioningPlan{}, err
	}
	if image.Status != "Ready" {
		return domain.VirtualMachineProvisioningPlan{}, fmt.Errorf("image %s is not ready", image.ID)
	}

	var flavor domain.Flavor
	if err := tx.QueryRow(ctx, `
SELECT id, name, cpu, memory_bytes, root_disk_bytes, enabled
FROM flavors
WHERE id = $1 AND enabled = true`, flavorID).
		Scan(&flavor.ID, &flavor.Name, &flavor.CPU, &flavor.MemoryBytes, &flavor.RootDiskBytes, &flavor.Enabled); err != nil {
		return domain.VirtualMachineProvisioningPlan{}, err
	}

	subnetID := strings.TrimSpace(input.SubnetID)
	var subnet *domain.Subnet
	if subnetID != "" {
		var s domain.Subnet
		if err := tx.QueryRow(ctx, `
SELECT id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at
FROM subnets
WHERE id = $1 AND project_id = $2`, subnetID, projectID).
			Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.VirtualMachineProvisioningPlan{}, ErrNotFound
			}
			return domain.VirtualMachineProvisioningPlan{}, err
		}
		subnet = &s
	}

	vmID := makeVirtualMachineID(projectID, name)
	var vm domain.VirtualMachine
	var scannedSubnet *string
	if err := tx.QueryRow(ctx, `
INSERT INTO virtual_machines (id, project_id, name, image_id, flavor_id, subnet_id, status)
VALUES ($1, $2, $3, $4, $5, $6, 'Provisioning')
ON CONFLICT (id) DO UPDATE
SET image_id = EXCLUDED.image_id,
    flavor_id = EXCLUDED.flavor_id,
    subnet_id = EXCLUDED.subnet_id,
    status = EXCLUDED.status
RETURNING id, project_id, name, image_id, flavor_id, subnet_id, status, created_at`, vmID, projectID, name, imageID, flavorID, nullableString(subnetID)).
		Scan(&vm.ID, &vm.ProjectID, &vm.Name, &vm.ImageID, &vm.FlavorID, &scannedSubnet, &vm.Status, &vm.CreatedAt); err != nil {
		return domain.VirtualMachineProvisioningPlan{}, err
	}
	if scannedSubnet != nil {
		vm.SubnetID = *scannedSubnet
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.VirtualMachineProvisioningPlan{}, err
	}
	return domain.VirtualMachineProvisioningPlan{
		VirtualMachine: vm,
		Image:          image,
		Flavor:         flavor,
		Subnet:         subnet,
		CloudInit:      normalizeCloudInit(input.CloudInit),
	}, nil
}

func normalizeCloudInit(input *domain.CloudInitConfig) *domain.CloudInitConfig {
	if input == nil {
		return nil
	}
	cloudInit := domain.CloudInitConfig{
		Username:     strings.TrimSpace(input.Username),
		Password:     strings.TrimSpace(input.Password),
		SSHPublicKey: strings.TrimSpace(input.SSHPublicKey),
	}
	if cloudInit.Username == "" && cloudInit.Password == "" && cloudInit.SSHPublicKey == "" {
		return nil
	}
	if cloudInit.Username == "" {
		cloudInit.Username = "cloudia"
	}
	return &cloudInit
}

func makeVirtualMachineID(projectID string, name string) string {
	id := strings.ToLower(strings.TrimSpace(projectID + "-" + name))
	id = vmNameSanitizer.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		return "vm"
	}
	return id
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
