# VPC/Subnet Network Virtualization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let portal users create VPCs and subnets (choosing the CIDR and a private/public type) and launch VMs onto a chosen subnet, with kube-ovn providing isolated custom VPCs and Multus attaching the subnet to the VM as its single NIC.

**Architecture:** Cilium stays the cluster default CNI. Each VPC maps to a kube-ovn `Vpc` CR (cluster-scoped, overlapping CIDR allowed); each subnet maps to a kube-ovn `Subnet` CR plus a Multus `NetworkAttachmentDefinition` (NAD) in the VM namespace. A VM's guest gets exactly one bridge interface bound to that NAD. Private subnets have no external path; public subnets get outbound-only SNAT via a kube-ovn VPC NAT gateway. PostgreSQL is the source of truth for network metadata; Kubernetes is the source of truth for runtime state.

**Tech Stack:** Go 1.25 (chi, pgx/v5, client-go dynamic client), PostgreSQL, kube-ovn, Multus, KubeVirt/CDI, React + TypeScript + Vite + TanStack Query.

**Spec:** [docs/superpowers/specs/2026-06-17-vpc-subnet-network-virtualization-design.md](../specs/2026-06-17-vpc-subnet-network-virtualization-design.md)

---

## File Structure

**Backend — create:**
- `backend/internal/kubevirt/network_manifest.go` — builds Vpc / Subnet / NAD / NAT-gateway / SNAT resource maps.
- `backend/internal/kubevirt/network_manifest_test.go` — golden tests for the above.
- `backend/internal/kubevirt/network_provisioner.go` — `NetworkProvisioner` applying/deleting network resources via the existing `Applier`.
- `backend/internal/store/network.go` — Postgres CRUD + CIDR validation for networks and subnets.
- `backend/internal/store/network_test.go` — unit tests for CIDR parse/overlap/gateway helpers (no DB needed).
- `backend/internal/httpapi/network_test.go` — handler tests for network/subnet endpoints.

**Backend — modify:**
- `backend/internal/domain/domain.go` — add `Network`, `Subnet`; extend VM input/plan/record.
- `backend/internal/store/postgres.go` — schema migration (networks, subnets, `virtual_machines.subnet_id`); VM create reads subnet.
- `backend/internal/kubevirt/dynamic_applier.go` — cluster-scoped resource support + new GVR mappings.
- `backend/internal/kubevirt/manifest.go` — inject Multus network/interface when a subnet is present.
- `backend/internal/kubevirt/status_reader.go` — kube-ovn/Multus preflight checks; prefer Multus interface IP.
- `backend/internal/httpapi/router.go` — `Dependencies` struct + new constructor, Store interface additions, network/subnet routes, `subnetId` on VM create.
- `backend/internal/httpapi/catalog_test.go` — extend `fakeCatalogStore` with the new Store methods.
- `backend/internal/config/config.go` — add `OVNExternalNetwork`.
- `backend/cmd/api/main.go` — wire `NetworkProvisioner`.

**Frontend — modify:**
- `frontend/src/api.ts` — `Network`/`Subnet` types + client functions; `subnetId` on VM create.
- `frontend/src/i18n.ts` — ko/en keys for networks/subnets.
- `frontend/src/App.tsx` — Networks panel, subnet form (with type), VM-form subnet dropdown, VM-detail subnet/IP.

**Docs — modify:**
- `README.md` — document the network APIs and the new env var.

---

## Phase A — Domain & Store

### Task 1: Domain types for Network and Subnet

**Files:**
- Modify: `backend/internal/domain/domain.go`

- [ ] **Step 1: Add the new types and extend VM types**

Append to `backend/internal/domain/domain.go` (after the `Image` type, before `VirtualMachine`):

```go
type Network struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	Name       string    `json:"name"`
	OVNVPCName string    `json:"-"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Subnet struct {
	ID            string    `json:"id"`
	NetworkID     string    `json:"networkId"`
	ProjectID     string    `json:"projectId"`
	Name          string    `json:"name"`
	CIDR          string    `json:"cidr"`
	Gateway       string    `json:"gateway"`
	Type          string    `json:"type"` // private | public
	OVNSubnetName string    `json:"-"`
	NADName       string    `json:"-"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
}
```

Change `VirtualMachine` to add a subnet reference (add the field after `FlavorID`):

```go
	FlavorID  string    `json:"flavorId"`
	SubnetID  string    `json:"subnetId"`
	Status    string    `json:"status"`
```

Change `CreateVirtualMachineInput` to add `SubnetID`:

```go
type CreateVirtualMachineInput struct {
	Name     string
	ImageID  string
	FlavorID string
	SubnetID string
}
```

Change `VirtualMachineProvisioningPlan` to carry the subnet (nil when networking not used):

```go
type VirtualMachineProvisioningPlan struct {
	VirtualMachine VirtualMachine
	Image          Image
	Flavor         Flavor
	Subnet         *Subnet
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly (no test run yet).

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/domain/domain.go && git commit -m "feat(domain): add Network and Subnet types"
```

---

### Task 2: CIDR validation helpers (pure functions, TDD)

**Files:**
- Create: `backend/internal/store/network.go`
- Test: `backend/internal/store/network_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/store/network_test.go`:

```go
package store

import "testing"

func TestNormalizeCIDRRejectsInvalid(t *testing.T) {
	if _, err := normalizeCIDR("not-a-cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if _, err := normalizeCIDR("10.0.0.0/24"); err != nil {
		t.Fatalf("expected 10.0.0.0/24 to be valid, got %v", err)
	}
}

func TestDefaultGatewayIsFirstHost(t *testing.T) {
	gw, err := defaultGateway("10.0.5.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gw != "10.0.5.1" {
		t.Fatalf("expected 10.0.5.1, got %q", gw)
	}
}

func TestCIDRsOverlapDetectsContainment(t *testing.T) {
	if !cidrsOverlap("10.0.0.0/16", "10.0.1.0/24") {
		t.Fatal("expected 10.0.0.0/16 and 10.0.1.0/24 to overlap")
	}
	if cidrsOverlap("10.0.0.0/24", "10.0.1.0/24") {
		t.Fatal("expected disjoint /24s not to overlap")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/store -run 'TestNormalizeCIDR|TestDefaultGateway|TestCIDRsOverlap' -v`
Expected: FAIL — `undefined: normalizeCIDR` (and the others).

- [ ] **Step 3: Implement the helpers**

Create `backend/internal/store/network.go`:

```go
package store

import (
	"errors"
	"net/netip"
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/store -run 'TestNormalizeCIDR|TestDefaultGateway|TestCIDRsOverlap' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/store/network.go internal/store/network_test.go && git commit -m "feat(store): add CIDR validation helpers"
```

---

### Task 3: Schema migration for networks and subnets

**Files:**
- Modify: `backend/internal/store/postgres.go:49-109` (the `ApplySchema` SQL block)

- [ ] **Step 1: Add the new tables and column to the schema SQL**

In `ApplySchema`, immediately after the `virtual_machines` `CREATE TABLE` statement (currently ending at line 84) and before the `INSERT INTO projects` block, insert:

```sql
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
```

- [ ] **Step 2: Build to verify the SQL string still compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/store/postgres.go && git commit -m "feat(store): add networks and subnets schema"
```

---

### Task 4: Network CRUD in Postgres

**Files:**
- Modify: `backend/internal/store/network.go`
- Modify: `backend/internal/store/postgres.go` (reuse `vmNameSanitizer`)

> Note: these methods need a `*pgxpool.Pool`. They are methods on `*Postgres`. There is no DB-free unit test here; correctness is exercised by the Postgres-backed integration test (`internal/store/postgres_test.go`, gated by `KLOUDIA_STORE_TEST_DATABASE_URL`). Verify compilation in this task; behavior in Task 6.

- [ ] **Step 1: Add an ID sanitizer and Network CRUD methods**

Append to `backend/internal/store/network.go`:

```go
import (
	"context"
	"errors"
	"net/netip"
	"strings"

	"github.com/kloudia/kloudia/backend/internal/domain"
)
```

Replace the existing import block of `network.go` with the combined block above (it must contain `context`, `errors`, `net/netip`, `strings`, and the `domain` package). Then append the methods:

```go
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
```

Add `"github.com/jackc/pgx/v5"` to the import block of `network.go` (needed for `pgx.ErrNoRows`).

- [ ] **Step 2: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/store/network.go && git commit -m "feat(store): add network CRUD"
```

---

### Task 5: Subnet CRUD in Postgres (with CIDR validation + guards)

**Files:**
- Modify: `backend/internal/store/network.go`

- [ ] **Step 1: Add Subnet CRUD methods**

Append to `backend/internal/store/network.go`:

```go
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
	} else if !cidrContainsHost(canonicalCIDR, gateway) {
		return domain.Subnet{}, ErrSubnetCIDRInvalid
	}

	// Ensure the parent VPC exists and belongs to the project.
	if _, err := p.GetNetwork(ctx, projectID, networkID); err != nil {
		return domain.Subnet{}, err
	}

	// Reject overlap with existing subnets in the same VPC.
	existing, err := p.ListSubnets(ctx, projectID, networkID)
	if err != nil {
		return domain.Subnet{}, err
	}
	for _, s := range existing {
		if cidrsOverlap(canonicalCIDR, s.CIDR) {
			return domain.Subnet{}, ErrSubnetCIDROverlap
		}
	}

	id := makeResourceID(networkID, name)
	ovnSubnetName := "kloudia-subnet-" + id
	nadName := "kloudia-net-" + id
	var s domain.Subnet
	err = p.pool.QueryRow(ctx, `
INSERT INTO subnets (id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'Ready')
RETURNING id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at`,
		id, networkID, projectID, name, canonicalCIDR, gateway, subnetType, ovnSubnetName, nadName).
		Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt)
	return s, err
}

func (p *Postgres) DeleteSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	s, err := p.GetSubnet(ctx, projectID, subnetID)
	if err != nil {
		return domain.Subnet{}, err
	}
	var vmCount int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM virtual_machines WHERE subnet_id = $1`, subnetID).Scan(&vmCount); err != nil {
		return domain.Subnet{}, err
	}
	if vmCount > 0 {
		return domain.Subnet{}, ErrSubnetInUse
	}
	if _, err := p.pool.Exec(ctx, `DELETE FROM subnets WHERE id = $1`, subnetID); err != nil {
		return domain.Subnet{}, err
	}
	return s, nil
}

func cidrContainsHost(cidr string, host string) bool {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return prefix.Contains(addr)
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/store/network.go && git commit -m "feat(store): add subnet CRUD with CIDR validation and guards"
```

---

### Task 6: VM create reads the subnet into the provisioning plan

**Files:**
- Modify: `backend/internal/store/postgres.go:223-278` (`CreateVirtualMachine`)

- [ ] **Step 1: Thread `SubnetID` through VM creation**

In `CreateVirtualMachine`, after the flavor lookup (currently ends ~line 255) and before `vmID := makeVirtualMachineID(...)`, add a subnet lookup:

```go
	subnetID := strings.TrimSpace(input.SubnetID)
	var subnet *domain.Subnet
	if subnetID != "" {
		var s domain.Subnet
		if err := tx.QueryRow(ctx, `
SELECT id, network_id, project_id, name, cidr, gateway, type, ovn_subnet_name, nad_name, status, created_at
FROM subnets
WHERE id = $1 AND project_id = $2`, subnetID, projectID).
			Scan(&s.ID, &s.NetworkID, &s.ProjectID, &s.Name, &s.CIDR, &s.Gateway, &s.Type, &s.OVNSubnetName, &s.NADName, &s.Status, &s.CreatedAt); err != nil {
			return domain.VirtualMachineProvisioningPlan{}, err
		}
		subnet = &s
	}
```

Change the `INSERT INTO virtual_machines` statement to include `subnet_id`. Replace the existing insert/scan with:

```go
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
```

Update the returned plan to include the subnet:

```go
	return domain.VirtualMachineProvisioningPlan{
		VirtualMachine: vm,
		Image:          image,
		Flavor:         flavor,
		Subnet:         subnet,
	}, nil
```

Add this helper at the bottom of `postgres.go`:

```go
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
```

- [ ] **Step 2: Update the other VM read queries to select subnet_id**

`subnet_id` is nullable, so scans must use `*string`. Update `ListVirtualMachines` and `GetVirtualMachine` to select and scan it.

In `ListVirtualMachines`, change the query to `SELECT id, project_id, name, image_id, flavor_id, subnet_id, status, created_at` and the scan loop body to:

```go
		var vm domain.VirtualMachine
		var subnetID *string
		if err := rows.Scan(&vm.ID, &vm.ProjectID, &vm.Name, &vm.ImageID, &vm.FlavorID, &subnetID, &vm.Status, &vm.CreatedAt); err != nil {
			return nil, err
		}
		if subnetID != nil {
			vm.SubnetID = *subnetID
		}
```

In `GetVirtualMachine`, change the query to include `subnet_id` and scan into a `*string`:

```go
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
```

- [ ] **Step 3: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Run the Postgres integration test (if a DB is available)**

Run: `cd backend && KLOUDIA_STORE_TEST_DATABASE_URL='postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable' go test ./internal/store -v`
Expected: PASS. If no DB is available, run `cd backend && go test ./internal/store -run 'TestNormalizeCIDR|TestDefaultGateway|TestCIDRsOverlap'` and note the integration test is skipped.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/store/postgres.go && git commit -m "feat(store): attach subnet to VM provisioning"
```

---

## Phase B — KubeVirt manifests & applier

### Task 7: Cluster-scoped support + new GVRs in the dynamic applier

**Files:**
- Modify: `backend/internal/kubevirt/dynamic_applier.go`

- [ ] **Step 1: Make `resourceMapping` return a namespaced flag and add the new GVRs**

Replace the `resourceMapping` function with:

```go
func resourceMapping(resource map[string]any) (schema.GroupVersionResource, bool, error) {
	apiVersion, _ := resource["apiVersion"].(string)
	kind, _ := resource["kind"].(string)
	switch {
	case apiVersion == "cdi.kubevirt.io/v1beta1" && kind == "DataVolume":
		return schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}, true, nil
	case apiVersion == "kubevirt.io/v1" && kind == "VirtualMachine":
		return schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}, true, nil
	case apiVersion == "kubeovn.io/v1" && kind == "Vpc":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "Subnet":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "subnets"}, false, nil
	case apiVersion == "k8s.cni.cncf.io/v1" && kind == "NetworkAttachmentDefinition":
		return schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"}, true, nil
	case apiVersion == "kubeovn.io/v1" && kind == "VpcNatGateway":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-nat-gateways"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "IptablesEIP":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-eips"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "IptablesSnatRule":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-snat-rules"}, false, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("unsupported resource %s %s", apiVersion, kind)
	}
}
```

> Note: kube-ovn CRD scoping (namespaced vs cluster) for the NAT-gateway resources is provisional and must be confirmed on the test cluster per the spec's unresolved items. Adjust the boolean if verification shows otherwise.

- [ ] **Step 2: Update `Apply` to honor the namespaced flag**

Replace the body of `Apply` from the `gvr, err := resourceMapping(resource)` line through the `resourceClient := ...` assignment with:

```go
	gvr, namespaced, err := resourceMapping(resource)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: resource}
	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("resource %s requires metadata.name", gvr.Resource)
	}
	var resourceClient dynamic.ResourceInterface = a.client.Resource(gvr)
	if namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			return fmt.Errorf("resource %s requires metadata.namespace", gvr.Resource)
		}
		resourceClient = a.client.Resource(gvr).Namespace(namespace)
	}
```

(The remaining create-or-update logic using `resourceClient` is unchanged.)

- [ ] **Step 3: Update `Delete` the same way**

Replace the body of `Delete` from `gvr, err := resourceMapping(resource)` through the `Delete(...)` call with:

```go
	gvr, namespaced, err := resourceMapping(resource)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: resource}
	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("resource %s requires metadata.name", gvr.Resource)
	}
	var resourceClient dynamic.ResourceInterface = a.client.Resource(gvr)
	if namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			return fmt.Errorf("resource %s requires metadata.namespace", gvr.Resource)
		}
		resourceClient = a.client.Resource(gvr).Namespace(namespace)
	}
	err = resourceClient.Delete(ctx, name, v1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
```

- [ ] **Step 4: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly (`dynamic` is already imported).

- [ ] **Step 5: Run existing kubevirt tests to confirm no regression**

Run: `cd backend && go test ./internal/kubevirt -run TestBuildResources -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/kubevirt/dynamic_applier.go && git commit -m "feat(kubevirt): support cluster-scoped resources and kube-ovn GVRs"
```

---

### Task 8: VPC / Subnet / NAD manifest builders (TDD)

**Files:**
- Create: `backend/internal/kubevirt/network_manifest.go`
- Test: `backend/internal/kubevirt/network_manifest_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/kubevirt/network_manifest_test.go`:

```go
package kubevirt

import (
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestBuildVPCResource(t *testing.T) {
	n := domain.Network{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app"}
	res := BuildVPCResource(n)
	if res["apiVersion"] != "kubeovn.io/v1" || res["kind"] != "Vpc" {
		t.Fatalf("unexpected gvk: %#v", res)
	}
	meta := res["metadata"].(map[string]any)
	if meta["name"] != "kloudia-vpc-platform-app" {
		t.Fatalf("unexpected vpc name: %#v", meta["name"])
	}
	if _, hasNS := meta["namespace"]; hasNS {
		t.Fatalf("Vpc is cluster-scoped and must not set namespace")
	}
}

func TestBuildSubnetResourceWiresVPCAndProvider(t *testing.T) {
	s := domain.Subnet{
		ID: "platform-app-web", ProjectID: "platform", NetworkID: "platform-app",
		CIDR: "10.0.0.0/24", Gateway: "10.0.0.1",
		OVNSubnetName: "kloudia-subnet-platform-app-web", NADName: "kloudia-net-platform-app-web",
	}
	res := BuildSubnetResource("kloudia-vms", "kloudia-vpc-platform-app", s)
	spec := res["spec"].(map[string]any)
	if spec["vpc"] != "kloudia-vpc-platform-app" {
		t.Fatalf("expected vpc ref, got %#v", spec["vpc"])
	}
	if spec["cidrBlock"] != "10.0.0.0/24" {
		t.Fatalf("expected cidrBlock, got %#v", spec["cidrBlock"])
	}
	if spec["provider"] != "kloudia-net-platform-app-web.kloudia-vms.ovn" {
		t.Fatalf("expected provider, got %#v", spec["provider"])
	}
}

func TestBuildNADResourceIsNamespacedWithProvider(t *testing.T) {
	s := domain.Subnet{NADName: "kloudia-net-platform-app-web"}
	res := BuildNADResource("kloudia-vms", s)
	if res["apiVersion"] != "k8s.cni.cncf.io/v1" || res["kind"] != "NetworkAttachmentDefinition" {
		t.Fatalf("unexpected gvk: %#v", res)
	}
	meta := res["metadata"].(map[string]any)
	if meta["namespace"] != "kloudia-vms" || meta["name"] != "kloudia-net-platform-app-web" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	spec := res["spec"].(map[string]any)
	cfg := spec["config"].(string)
	if !contains(cfg, "kube-ovn") || !contains(cfg, "kloudia-net-platform-app-web.kloudia-vms.ovn") {
		t.Fatalf("expected kube-ovn config with provider, got %q", cfg)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || index(haystack, needle) >= 0)
}

func index(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/kubevirt -run 'TestBuildVPCResource|TestBuildSubnetResource|TestBuildNADResource' -v`
Expected: FAIL — `undefined: BuildVPCResource`.

- [ ] **Step 3: Implement the manifest builders**

Create `backend/internal/kubevirt/network_manifest.go`:

```go
package kubevirt

import (
	"fmt"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

// nadProvider is the kube-ovn provider string linking a Subnet to its NAD: "<nad>.<namespace>.ovn".
func nadProvider(namespace string, subnet domain.Subnet) string {
	return fmt.Sprintf("%s.%s.ovn", subnet.NADName, namespace)
}

func BuildVPCResource(network domain.Network) map[string]any {
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Vpc",
		"metadata": map[string]any{
			"name": network.OVNVPCName,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/project":           network.ProjectID,
				"kloudia.io/network-id":        network.ID,
			},
		},
		"spec": map[string]any{},
	}
}

func BuildSubnetResource(namespace string, ovnVPCName string, subnet domain.Subnet) map[string]any {
	return map[string]any{
		"apiVersion": "kubeovn.io/v1",
		"kind":       "Subnet",
		"metadata": map[string]any{
			"name": subnet.OVNSubnetName,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/project":           subnet.ProjectID,
				"kloudia.io/subnet-id":         subnet.ID,
			},
		},
		"spec": map[string]any{
			"vpc":       ovnVPCName,
			"cidrBlock": subnet.CIDR,
			"gateway":   subnet.Gateway,
			"protocol":  "IPv4",
			"provider":  nadProvider(namespace, subnet),
		},
	}
}

func BuildNADResource(namespace string, subnet domain.Subnet) map[string]any {
	config := fmt.Sprintf(
		`{"cniVersion":"0.3.1","type":"kube-ovn","server_socket":"/run/openvswitch/kube-ovn-daemon.sock","provider":"%s"}`,
		nadProvider(namespace, subnet),
	)
	return map[string]any{
		"apiVersion": "k8s.cni.cncf.io/v1",
		"kind":       "NetworkAttachmentDefinition",
		"metadata": map[string]any{
			"name":      subnet.NADName,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/subnet-id":         subnet.ID,
			},
		},
		"spec": map[string]any{
			"config": config,
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/kubevirt -run 'TestBuildVPCResource|TestBuildSubnetResource|TestBuildNADResource' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/kubevirt/network_manifest.go internal/kubevirt/network_manifest_test.go && git commit -m "feat(kubevirt): add VPC/Subnet/NAD manifest builders"
```

---

### Task 9: Public-subnet SNAT manifests (TDD)

**Files:**
- Modify: `backend/internal/kubevirt/network_manifest.go`
- Modify: `backend/internal/kubevirt/network_manifest_test.go`

- [ ] **Step 1: Write the failing test**

Append to `network_manifest_test.go`:

```go
func TestBuildPublicEgressResources(t *testing.T) {
	s := domain.Subnet{
		ID: "platform-app-web", ProjectID: "platform", NetworkID: "platform-app",
		CIDR: "10.0.0.0/24", Type: "public",
		OVNSubnetName: "kloudia-subnet-platform-app-web",
	}
	res := BuildPublicEgressResources("kloudia-vms", "kloudia-vpc-platform-app", "ovn-vpc-external-network", s)
	if res.NatGateway["kind"] != "VpcNatGateway" {
		t.Fatalf("expected VpcNatGateway, got %#v", res.NatGateway["kind"])
	}
	if res.EIP["kind"] != "IptablesEIP" {
		t.Fatalf("expected IptablesEIP, got %#v", res.EIP["kind"])
	}
	snatSpec := res.SnatRule["spec"].(map[string]any)
	if snatSpec["internalCIDR"] != "10.0.0.0/24" {
		t.Fatalf("expected SNAT internalCIDR to match subnet, got %#v", snatSpec["internalCIDR"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/kubevirt -run TestBuildPublicEgressResources -v`
Expected: FAIL — `undefined: BuildPublicEgressResources`.

- [ ] **Step 3: Implement the SNAT builders**

Append to `network_manifest.go`:

```go
// EgressResources are the kube-ovn resources that give a public subnet outbound-only SNAT.
type EgressResources struct {
	NatGateway map[string]any
	EIP        map[string]any
	SnatRule   map[string]any
}

func vpcNatGatewayName(ovnVPCName string) string { return ovnVPCName + "-natgw" }
func eipName(subnet domain.Subnet) string         { return subnet.OVNSubnetName + "-eip" }
func snatRuleName(subnet domain.Subnet) string     { return subnet.OVNSubnetName + "-snat" }

// BuildPublicEgressResources builds VpcNatGateway + IptablesEIP + IptablesSnatRule for a public subnet.
// externalNetwork is the kube-ovn external provider network configured at the infra layer.
func BuildPublicEgressResources(namespace string, ovnVPCName string, externalNetwork string, subnet domain.Subnet) EgressResources {
	gwName := vpcNatGatewayName(ovnVPCName)
	eName := eipName(subnet)
	return EgressResources{
		NatGateway: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "VpcNatGateway",
			"metadata": map[string]any{
				"name":   gwName,
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"vpc":             ovnVPCName,
				"subnet":          subnet.OVNSubnetName,
				"externalSubnets": []any{externalNetwork},
			},
		},
		EIP: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "IptablesEIP",
			"metadata": map[string]any{
				"name":   eName,
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"natGwDp":        gwName,
				"externalSubnet": externalNetwork,
			},
		},
		SnatRule: map[string]any{
			"apiVersion": "kubeovn.io/v1",
			"kind":       "IptablesSnatRule",
			"metadata": map[string]any{
				"name":   snatRuleName(subnet),
				"labels": map[string]any{"app.kubernetes.io/managed-by": "kloudia"},
			},
			"spec": map[string]any{
				"eip":          eName,
				"internalCIDR": subnet.CIDR,
			},
		},
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/kubevirt -run TestBuildPublicEgressResources -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/kubevirt/network_manifest.go internal/kubevirt/network_manifest_test.go && git commit -m "feat(kubevirt): add public-subnet SNAT manifests"
```

---

### Task 10: Inject the Multus NIC into the VM manifest (TDD)

**Files:**
- Modify: `backend/internal/kubevirt/manifest.go:43-91` (`BuildResources`)
- Modify: `backend/internal/kubevirt/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Append to `manifest_test.go`:

```go
func TestBuildResourcesAddsMultusNICWhenSubnetPresent(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
		Subnet:         &domain.Subnet{ID: "platform-app-web", NADName: "kloudia-net-platform-app-web"},
	}

	resources := BuildResources("kloudia-vms", "test-delete", plan)

	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	networks := templateSpec["networks"].([]any)
	if len(networks) != 1 {
		t.Fatalf("expected exactly one network, got %#v", networks)
	}
	net0 := networks[0].(map[string]any)
	multus := net0["multus"].(map[string]any)
	if multus["networkName"] != "kloudia-vms/kloudia-net-platform-app-web" {
		t.Fatalf("expected multus networkName, got %#v", multus["networkName"])
	}
	ifaces := templateSpec["domain"].(map[string]any)["devices"].(map[string]any)["interfaces"].([]any)
	if len(ifaces) != 1 {
		t.Fatalf("expected one interface, got %#v", ifaces)
	}
	if _, hasBridge := ifaces[0].(map[string]any)["bridge"]; !hasBridge {
		t.Fatalf("expected bridge binding, got %#v", ifaces[0])
	}
}

func TestBuildResourcesOmitsNetworksWhenNoSubnet(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-02", ProjectID: "platform", Name: "web-02"},
		Image:          domain.Image{ID: "platform-cirros", SourceURL: "https://example.invalid/cirros.img"},
		Flavor:         domain.Flavor{ID: "small", CPU: 1, MemoryBytes: 512 * 1024 * 1024, RootDiskBytes: 2 * 1024 * 1024 * 1024},
	}
	resources := BuildResources("kloudia-vms", "test-delete", plan)
	templateSpec := resources.VirtualMachine["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	if _, hasNetworks := templateSpec["networks"]; hasNetworks {
		t.Fatalf("expected no networks block when subnet is absent")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/kubevirt -run 'TestBuildResourcesAddsMultusNIC|TestBuildResourcesOmitsNetworks' -v`
Expected: FAIL — the networks key does not exist.

- [ ] **Step 3: Inject the network block when a subnet is present**

In `manifest.go`, the `BuildResources` function returns a `Resources` struct literal. Refactor so the VM map is built into a variable first, then conditionally augmented. Replace the `VirtualMachine: map[string]any{ ... }` literal so the function reads:

```go
	vm := map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]any{
			"name":      vmName,
			"namespace": namespace,
			"labels":    labels(plan),
		},
		"spec": map[string]any{
			"runStrategy": "Always",
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels(plan),
				},
				"spec": map[string]any{
					"domain": map[string]any{
						"cpu": map[string]any{
							"cores": int64(plan.Flavor.CPU),
						},
						"devices": map[string]any{
							"disks": []any{
								map[string]any{
									"name": "rootdisk",
									"disk": map[string]any{"bus": "virtio"},
								},
							},
						},
						"resources": map[string]any{
							"requests": map[string]any{
								"memory": formatBytes(plan.Flavor.MemoryBytes),
							},
						},
					},
					"volumes": []any{
						map[string]any{
							"name":       "rootdisk",
							"dataVolume": map[string]any{"name": dataVolumeName},
						},
					},
				},
			},
		},
	}

	if plan.Subnet != nil {
		applyMultusNetwork(vm, namespace, *plan.Subnet)
	}

	return Resources{
		DataVolume:     dataVolume,
		VirtualMachine: vm,
	}
```

Rename the existing `DataVolume: map[string]any{ ... }` literal to a `dataVolume := map[string]any{ ... }` variable declared just above `vm`, so both are referenced in the final return.

Add this helper to `manifest.go`:

```go
func applyMultusNetwork(vm map[string]any, namespace string, subnet domain.Subnet) {
	templateSpec := vm["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	templateSpec["networks"] = []any{
		map[string]any{
			"name": "net1",
			"multus": map[string]any{
				"networkName": namespace + "/" + subnet.NADName,
			},
		},
	}
	devices := templateSpec["domain"].(map[string]any)["devices"].(map[string]any)
	devices["interfaces"] = []any{
		map[string]any{
			"name":   "net1",
			"bridge": map[string]any{},
		},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/kubevirt -run 'TestBuildResources' -v`
Expected: PASS (both the new tests and the existing `TestBuildResourcesUsesRunStrategyAndDataVolumeSource`).

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/kubevirt/manifest.go internal/kubevirt/manifest_test.go && git commit -m "feat(kubevirt): attach Multus NIC to VM when subnet is set"
```

---

### Task 11: NetworkProvisioner

**Files:**
- Create: `backend/internal/kubevirt/network_provisioner.go`

- [ ] **Step 1: Implement the provisioner**

Create `backend/internal/kubevirt/network_provisioner.go`:

```go
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
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
cd backend && git add internal/kubevirt/network_provisioner.go && git commit -m "feat(kubevirt): add NetworkProvisioner"
```

---

## Phase C — HTTP API

### Task 12: Extend Store interface + Dependencies constructor

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/catalog_test.go`

- [ ] **Step 1: Extend the Store interface and add NetworkProvisioner**

In `router.go`, add to the `Store` interface (after `DeleteVirtualMachine`):

```go
	ListNetworks(ctx context.Context, projectID string) ([]domain.Network, error)
	GetNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	CreateNetwork(ctx context.Context, projectID string, name string) (domain.Network, error)
	DeleteNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	ListSubnets(ctx context.Context, projectID string, networkID string) ([]domain.Subnet, error)
	GetSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
	CreateSubnet(ctx context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error)
	DeleteSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
```

Add a `NetworkProvisioner` interface and a noop after the `noopConsoleStreamer` type:

```go
type NetworkProvisioner interface {
	CreateNetwork(ctx context.Context, network domain.Network) error
	DeleteNetwork(ctx context.Context, network domain.Network) error
	CreateSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error
	DeleteSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error
}

type noopNetworkProvisioner struct{}

func (noopNetworkProvisioner) CreateNetwork(context.Context, domain.Network) error { return nil }
func (noopNetworkProvisioner) DeleteNetwork(context.Context, domain.Network) error { return nil }
func (noopNetworkProvisioner) CreateSubnet(context.Context, string, domain.Subnet) error { return nil }
func (noopNetworkProvisioner) DeleteSubnet(context.Context, string, domain.Subnet) error { return nil }
```

- [ ] **Step 2: Add a Dependencies struct and route everything through it**

In `router.go`, add above `NewRouter`:

```go
type Dependencies struct {
	Version            string
	Store              Store
	Provisioner        Provisioner
	StatusReader       StatusReader
	ConsoleStreamer    ConsoleStreamer
	NetworkProvisioner NetworkProvisioner
}
```

Change `NewRouterWithProvisionerStatusReaderAndConsoleStreamer` to delegate to a new `NewRouterWithDependencies`:

```go
func NewRouterWithProvisionerStatusReaderAndConsoleStreamer(version string, store Store, provisioner Provisioner, statusReader StatusReader, consoleStreamer ConsoleStreamer) http.Handler {
	return NewRouterWithDependencies(Dependencies{
		Version:         version,
		Store:           store,
		Provisioner:     provisioner,
		StatusReader:    statusReader,
		ConsoleStreamer: consoleStreamer,
	})
}
```

Rename the current body of `NewRouterWithProvisionerStatusReaderAndConsoleStreamer` into `NewRouterWithDependencies(deps Dependencies) http.Handler`. At the top of that function, default any nil dependency:

```go
func NewRouterWithDependencies(deps Dependencies) http.Handler {
	version := deps.Version
	store := deps.Store
	provisioner := deps.Provisioner
	statusReader := deps.StatusReader
	consoleStreamer := deps.ConsoleStreamer
	networkProvisioner := deps.NetworkProvisioner
	if provisioner == nil {
		provisioner = noopProvisioner{}
	}
	if statusReader == nil {
		statusReader = noopStatusReader{}
	}
	if consoleStreamer == nil {
		consoleStreamer = noopConsoleStreamer{}
	}
	if networkProvisioner == nil {
		networkProvisioner = noopNetworkProvisioner{}
	}
	router := chi.NewRouter()
	// ... existing middleware + routes unchanged ...
}
```

- [ ] **Step 3: Add the new Store methods to `fakeCatalogStore`**

In `catalog_test.go`, add fields to `fakeCatalogStore`:

```go
	networks []domain.Network
	subnets  []domain.Subnet
```

Add method implementations (place after `DeleteVirtualMachine`):

```go
func (s *fakeCatalogStore) ListNetworks(_ context.Context, projectID string) ([]domain.Network, error) {
	var out []domain.Network
	for _, n := range s.networks {
		if n.ProjectID == projectID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *fakeCatalogStore) GetNetwork(_ context.Context, projectID string, networkID string) (domain.Network, error) {
	for _, n := range s.networks {
		if n.ProjectID == projectID && n.ID == networkID {
			return n, nil
		}
	}
	return domain.Network{}, errors.New("network not found")
}

func (s *fakeCatalogStore) CreateNetwork(_ context.Context, projectID string, name string) (domain.Network, error) {
	n := domain.Network{ID: projectID + "-" + name, ProjectID: projectID, Name: name, OVNVPCName: "kloudia-vpc-" + projectID + "-" + name, Status: "Ready"}
	s.networks = append(s.networks, n)
	return n, nil
}

func (s *fakeCatalogStore) DeleteNetwork(_ context.Context, projectID string, networkID string) (domain.Network, error) {
	for i, n := range s.networks {
		if n.ProjectID == projectID && n.ID == networkID {
			s.networks = append(s.networks[:i], s.networks[i+1:]...)
			return n, nil
		}
	}
	return domain.Network{}, errors.New("network not found")
}

func (s *fakeCatalogStore) ListSubnets(_ context.Context, projectID string, networkID string) ([]domain.Subnet, error) {
	var out []domain.Subnet
	for _, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.NetworkID == networkID {
			out = append(out, sn)
		}
	}
	return out, nil
}

func (s *fakeCatalogStore) GetSubnet(_ context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	for _, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.ID == subnetID {
			return sn, nil
		}
	}
	return domain.Subnet{}, errors.New("subnet not found")
}

func (s *fakeCatalogStore) CreateSubnet(_ context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error) {
	sn := domain.Subnet{ID: networkID + "-" + name, NetworkID: networkID, ProjectID: projectID, Name: name, CIDR: cidr, Gateway: gateway, Type: subnetType, OVNSubnetName: "kloudia-subnet-" + networkID + "-" + name, NADName: "kloudia-net-" + networkID + "-" + name, Status: "Ready"}
	s.subnets = append(s.subnets, sn)
	return sn, nil
}

func (s *fakeCatalogStore) DeleteSubnet(_ context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	for i, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.ID == subnetID {
			s.subnets = append(s.subnets[:i], s.subnets[i+1:]...)
			return sn, nil
		}
	}
	return domain.Subnet{}, errors.New("subnet not found")
}
```

> Note: `fakeCatalogStore` is sometimes used as a value and sometimes as a pointer in existing tests. The existing `GetVirtualMachine`/`DeleteVirtualMachine` are already pointer receivers and tests pass `&fakeCatalogStore{}`. Keep all new methods on the pointer receiver `*fakeCatalogStore` for consistency; update any value-based construction in existing tests to `&fakeCatalogStore{...}` if the compiler reports the interface is not satisfied.

- [ ] **Step 4: Run the existing handler tests to confirm no regression**

Run: `cd backend && go test ./internal/httpapi -v`
Expected: PASS (all existing tests still green).

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/httpapi/router.go internal/httpapi/catalog_test.go && git commit -m "feat(httpapi): add Dependencies constructor and network Store methods"
```

---

### Task 13: Network endpoints (TDD)

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Create: `backend/internal/httpapi/network_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/httpapi/network_test.go`:

```go
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestCreateNetworkPersistsAndProvisions(t *testing.T) {
	store := &fakeCatalogStore{}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})
	body := bytes.NewBufferString(`{"name":"app"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(np.createdNetworks) != 1 {
		t.Fatalf("expected provisioner CreateNetwork once, got %d", len(np.createdNetworks))
	}
}

func TestListNetworksFiltersByProject(t *testing.T) {
	store := &fakeCatalogStore{networks: []domain.Network{
		{ID: "platform-app", ProjectID: "platform", Name: "app", Status: "Ready"},
		{ID: "data-lake", ProjectID: "data", Name: "lake", Status: "Ready"},
	}}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/networks", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	var body struct {
		Data []domain.Network `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "platform-app" {
		t.Fatalf("unexpected networks: %#v", body.Data)
	}
}
```

Add the recording provisioner to `network_test.go`:

```go
type recordingNetworkProvisioner struct {
	createdNetworks []domain.Network
	deletedNetworks []domain.Network
	createdSubnets  []domain.Subnet
	deletedSubnets  []domain.Subnet
}

func (p *recordingNetworkProvisioner) CreateNetwork(_ context.Context, n domain.Network) error {
	p.createdNetworks = append(p.createdNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteNetwork(_ context.Context, n domain.Network) error {
	p.deletedNetworks = append(p.deletedNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) CreateSubnet(_ context.Context, _ string, s domain.Subnet) error {
	p.createdSubnets = append(p.createdSubnets, s)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteSubnet(_ context.Context, _ string, s domain.Subnet) error {
	p.deletedSubnets = append(p.deletedSubnets, s)
	return nil
}
```

Add the `context` import to the test file's import block.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/httpapi -run 'TestCreateNetwork|TestListNetworks' -v`
Expected: FAIL — routes return 404 (not registered yet).

- [ ] **Step 3: Register the network routes**

In `NewRouterWithDependencies`, after the `/api/v1/projects/{projectID}/vms/...` routes and before `return router`, add:

```go
	router.Get("/api/v1/projects/{projectID}/networks", func(w http.ResponseWriter, r *http.Request) {
		networks, err := store.ListNetworks(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NETWORKS_UNAVAILABLE", "failed to list networks")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": networks})
	})

	router.Post("/api/v1/projects/{projectID}/networks", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_NETWORK_REQUEST", "name is required")
			return
		}
		network, err := store.CreateNetwork(r.Context(), chi.URLParam(r, "projectID"), request.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NETWORK_CREATE_FAILED", "failed to create network")
			return
		}
		if err := networkProvisioner.CreateNetwork(r.Context(), network); err != nil {
			writeError(w, http.StatusBadGateway, "NETWORK_PROVISION_FAILED", "failed to provision network")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": network})
	})

	router.Delete("/api/v1/projects/{projectID}/networks/{networkID}", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		if _, err := store.DeleteNetwork(r.Context(), projectID, networkID); err != nil {
			if errors.Is(err, storepkg.ErrNetworkInUse) {
				writeError(w, http.StatusConflict, "NETWORK_IN_USE", "network still has subnets")
				return
			}
			writeError(w, http.StatusInternalServerError, "NETWORK_DELETE_FAILED", "failed to delete network")
			return
		}
		if err := networkProvisioner.DeleteNetwork(r.Context(), network); err != nil {
			writeError(w, http.StatusBadGateway, "NETWORK_DEPROVISION_FAILED", "failed to delete network resources")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
```

> Type-resolution note: `store` is the local variable (a `Store`), while the sentinel errors live in the `store` package — a name clash. Import the store package under an alias to compare sentinels. Add `storepkg "github.com/kloudia/kloudia/backend/internal/store"` to `router.go` imports and use `errors.Is(err, storepkg.ErrNetworkInUse)` (not `store.ErrNetworkInUse`). Also add `"errors"` to the imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/httpapi -run 'TestCreateNetwork|TestListNetworks' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/httpapi/router.go internal/httpapi/network_test.go && git commit -m "feat(httpapi): add network endpoints"
```

---

### Task 14: Subnet endpoints (TDD)

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/network_test.go`

- [ ] **Step 1: Write the failing test**

Append to `network_test.go`:

```go
func TestCreateSubnetValidatesTypeAndProvisions(t *testing.T) {
	store := &fakeCatalogStore{networks: []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}}}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	ok := bytes.NewBufferString(`{"name":"web","cidr":"10.0.0.0/24","type":"private"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", ok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(np.createdSubnets) != 1 {
		t.Fatalf("expected provisioner CreateSubnet once, got %d", len(np.createdSubnets))
	}

	bad := bytes.NewBufferString(`{"name":"web2","cidr":"10.0.1.0/24","type":"weird"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", bad)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid type, got %d", rec2.Code)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/httpapi -run TestCreateSubnet -v`
Expected: FAIL — route not registered.

- [ ] **Step 3: Register the subnet routes**

In `NewRouterWithDependencies`, after the network routes, add:

```go
	router.Get("/api/v1/projects/{projectID}/networks/{networkID}/subnets", func(w http.ResponseWriter, r *http.Request) {
		subnets, err := store.ListSubnets(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "networkID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SUBNETS_UNAVAILABLE", "failed to list subnets")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": subnets})
	})

	router.Post("/api/v1/projects/{projectID}/networks/{networkID}/subnets", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		var request struct {
			Name    string `json:"name"`
			CIDR    string `json:"cidr"`
			Type    string `json:"type"`
			Gateway string `json:"gateway"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" || request.CIDR == "" {
			writeError(w, http.StatusBadRequest, "INVALID_SUBNET_REQUEST", "name and cidr are required")
			return
		}
		if request.Type == "" {
			request.Type = "private"
		}
		if request.Type != "private" && request.Type != "public" {
			writeError(w, http.StatusBadRequest, "SUBNET_TYPE_INVALID", "type must be private or public")
			return
		}
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		subnet, err := store.CreateSubnet(r.Context(), projectID, networkID, request.Name, request.CIDR, request.Type, request.Gateway)
		if err != nil {
			switch {
			case errors.Is(err, storepkg.ErrSubnetCIDRInvalid):
				writeError(w, http.StatusBadRequest, "SUBNET_CIDR_INVALID", "subnet cidr is invalid")
			case errors.Is(err, storepkg.ErrSubnetCIDROverlap):
				writeError(w, http.StatusConflict, "SUBNET_CIDR_OVERLAP", "subnet cidr overlaps an existing subnet in this vpc")
			default:
				writeError(w, http.StatusInternalServerError, "SUBNET_CREATE_FAILED", "failed to create subnet")
			}
			return
		}
		if err := networkProvisioner.CreateSubnet(r.Context(), network.OVNVPCName, subnet); err != nil {
			writeError(w, http.StatusBadGateway, "SUBNET_PROVISION_FAILED", "failed to provision subnet")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": subnet})
	})

	router.Delete("/api/v1/projects/{projectID}/networks/{networkID}/subnets/{subnetID}", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		subnetID := chi.URLParam(r, "subnetID")
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		subnet, err := store.GetSubnet(r.Context(), projectID, subnetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "SUBNET_NOT_FOUND", "subnet was not found")
			return
		}
		if _, err := store.DeleteSubnet(r.Context(), projectID, subnetID); err != nil {
			if errors.Is(err, storepkg.ErrSubnetInUse) {
				writeError(w, http.StatusConflict, "SUBNET_IN_USE", "subnet still has virtual machines attached")
				return
			}
			writeError(w, http.StatusInternalServerError, "SUBNET_DELETE_FAILED", "failed to delete subnet")
			return
		}
		if err := networkProvisioner.DeleteSubnet(r.Context(), network.OVNVPCName, subnet); err != nil {
			writeError(w, http.StatusBadGateway, "SUBNET_DEPROVISION_FAILED", "failed to delete subnet resources")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/httpapi -run TestCreateSubnet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/httpapi/router.go internal/httpapi/network_test.go && git commit -m "feat(httpapi): add subnet endpoints"
```

---

### Task 15: Accept `subnetId` on VM create (TDD)

**Files:**
- Modify: `backend/internal/httpapi/router.go:194-224` (VM create handler)
- Modify: `backend/internal/httpapi/vm_create_test.go`

- [ ] **Step 1: Write the failing test**

Append to `vm_create_test.go`:

```go
func TestCreateVirtualMachinePassesSubnetID(t *testing.T) {
	store := &fakeCatalogStore{}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)
	body := bytes.NewBufferString(`{"name":"web-09","imageId":"platform-cirros","flavorId":"small","subnetId":"platform-app-web"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.createInputs) != 1 || store.createInputs[0].SubnetID != "platform-app-web" {
		t.Fatalf("expected subnetId to reach the store, got %#v", store.createInputs)
	}
}
```

This requires `fakeCatalogStore` to record create inputs. In `catalog_test.go`, add a field `createInputs []domain.CreateVirtualMachineInput` to the struct, and at the top of `CreateVirtualMachine` append the input: change the method to use a pointer receiver and record:

```go
func (s *fakeCatalogStore) CreateVirtualMachine(_ context.Context, projectID string, input domain.CreateVirtualMachineInput) (domain.VirtualMachineProvisioningPlan, error) {
	s.createInputs = append(s.createInputs, input)
	plan := s.createdPlan
	// ... rest unchanged ...
}
```

> The receiver changes from value to pointer; existing tests already pass `&fakeCatalogStore{}`, so this is compatible.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/httpapi -run TestCreateVirtualMachinePassesSubnetID -v`
Expected: FAIL — `subnetId` not decoded/forwarded.

- [ ] **Step 3: Forward `subnetId` in the handler**

In the VM create handler, extend the request struct and the input:

```go
		var request struct {
			Name     string `json:"name"`
			ImageID  string `json:"imageId"`
			FlavorID string `json:"flavorId"`
			SubnetID string `json:"subnetId"`
		}
```

and

```go
		plan, err := store.CreateVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), domain.CreateVirtualMachineInput{
			Name:     request.Name,
			ImageID:  request.ImageID,
			FlavorID: request.FlavorID,
			SubnetID: request.SubnetID,
		})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/httpapi -v`
Expected: PASS (all handler tests).

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/httpapi/router.go internal/httpapi/vm_create_test.go internal/httpapi/catalog_test.go && git commit -m "feat(httpapi): accept subnetId on VM create"
```

---

## Phase D — Status & preflight

### Task 16: kube-ovn/Multus preflight checks

**Files:**
- Modify: `backend/internal/kubevirt/status_reader.go` (in `GetPreflight`)

- [ ] **Step 1: Read the current GetPreflight implementation**

Run: `cd backend && grep -n "func.*GetPreflight" internal/kubevirt/status_reader.go`
Then read the function to find where individual checks are appended (each check builds a `domain.PreflightCheck`).

- [ ] **Step 2: Add network checks**

Following the existing check-append pattern in `GetPreflight`, add checks that confirm the kube-ovn and Multus CRDs are servable by listing each GVR with a `Limit: 1` list call. Use the same `domain.PreflightCheck{Name, Status, Message}` shape already used in the function. Add three checks:
- Name `"kube-ovn-vpc"`: list `schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"}`; `OK` if the list succeeds, `Failed` with the error message otherwise.
- Name `"multus"`: list `schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"}` in the configured namespace; `OK`/`Failed`.
- Name `"kube-ovn-nat-gateway"`: list `schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-nat-gateways"}`; `OK` if it succeeds, `Warning` (not `Failed`) otherwise with message `"public subnet egress unavailable"` — public is optional, so this must not fail overall readiness.

Match the function's existing aggregation logic: if it computes overall `Status` from the worst check, a `Warning` must not downgrade an otherwise-`OK` result to `Failed`.

- [ ] **Step 3: Build and run kubevirt tests**

Run: `cd backend && go build ./... && go test ./internal/kubevirt -v`
Expected: builds; existing kubevirt tests PASS (GetPreflight has no unit test harness since it needs a live cluster; verification is manual on the test cluster).

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/kubevirt/status_reader.go && git commit -m "feat(kubevirt): add kube-ovn and multus preflight checks"
```

---

## Phase E — Wiring

### Task 17: Config + main.go wiring

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/cmd/api/main.go`

- [ ] **Step 1: Add the external-network config field**

In `config.go`, add to the `Config` struct:

```go
	OVNExternalNetwork string
```

and in `Load()`:

```go
		OVNExternalNetwork: env("KLOUDIA_OVN_EXTERNAL_NETWORK", "ovn-vpc-external-network"),
```

- [ ] **Step 2: Wire the NetworkProvisioner via Dependencies**

In `main.go`, inside the `if cfg.KubeVirtEnabled {` block, after the `provisioner := kubevirt.NewProvisioner(...)` line, replace the `handler = httpapi.NewRouterWithProvisionerStatusReaderAndConsoleStreamer(...)` call with:

```go
		networkProvisioner := kubevirt.NewNetworkProvisioner(cfg.KubeVirtNamespace, cfg.OVNExternalNetwork, applier)
		handler = httpapi.NewRouterWithDependencies(httpapi.Dependencies{
			Version:            cfg.Version,
			Store:              db,
			Provisioner:        provisioner,
			StatusReader:       statusReader,
			ConsoleStreamer:    consoleStreamer,
			NetworkProvisioner: networkProvisioner,
		})
```

- [ ] **Step 3: Build the binary**

Run: `cd backend && go build ./cmd/api`
Expected: builds cleanly.

- [ ] **Step 4: Run the whole backend test suite**

Run: `cd backend && go test ./...`
Expected: PASS for all packages (store integration test skipped without `KLOUDIA_STORE_TEST_DATABASE_URL`).

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/config/config.go cmd/api/main.go && git commit -m "feat: wire NetworkProvisioner into the API"
```

---

## Phase F — Frontend

### Task 18: API client types and functions

**Files:**
- Modify: `frontend/src/api.ts`

- [ ] **Step 1: Add types**

After the `Flavor` type in `api.ts`, add:

```ts
export type Network = {
  id: string;
  projectId: string;
  name: string;
  status: string;
  createdAt: string;
};

export type Subnet = {
  id: string;
  networkId: string;
  projectId: string;
  name: string;
  cidr: string;
  gateway: string;
  type: string; // "private" | "public"
  status: string;
  createdAt: string;
};
```

Change `CreateVirtualMachineRequest` to include an optional subnet:

```ts
export type CreateVirtualMachineRequest = {
  name: string;
  imageId: string;
  flavorId: string;
  subnetId?: string;
};
```

- [ ] **Step 2: Add client functions**

After `fetchFlavors`, add:

```ts
export async function fetchNetworks(projectId: string): Promise<Network[]> {
  const response = await getJSON<ListResponse<Network>>(`/api/v1/projects/${projectId}/networks`);
  return response.data;
}

export async function createNetwork(projectId: string, name: string): Promise<Network> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/networks`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    throw new Error(`network create failed: ${response.status}`);
  }
  const body = (await response.json()) as { data: Network };
  return body.data;
}

export async function deleteNetwork(projectId: string, networkId: string): Promise<void> {
  const response = await fetch(`${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`network delete failed: ${response.status}`);
  }
}

export async function fetchSubnets(projectId: string, networkId: string): Promise<Subnet[]> {
  const response = await getJSON<ListResponse<Subnet>>(
    `/api/v1/projects/${projectId}/networks/${networkId}/subnets`,
  );
  return response.data;
}

export async function createSubnet(
  projectId: string,
  networkId: string,
  request: { name: string; cidr: string; type: string; gateway?: string },
): Promise<Subnet> {
  const response = await fetch(
    `${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}/subnets`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    },
  );
  if (!response.ok) {
    throw new Error(`subnet create failed: ${response.status}`);
  }
  const body = (await response.json()) as { data: Subnet };
  return body.data;
}

export async function deleteSubnet(projectId: string, networkId: string, subnetId: string): Promise<void> {
  const response = await fetch(
    `${apiBaseUrl}/api/v1/projects/${projectId}/networks/${networkId}/subnets/${subnetId}`,
    { method: "DELETE" },
  );
  if (!response.ok) {
    throw new Error(`subnet delete failed: ${response.status}`);
  }
}
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && npm run build`
Expected: `tsc -b` passes and Vite build succeeds. (Run `npm install` first if `node_modules` is absent.)

- [ ] **Step 4: Commit**

```bash
cd frontend && git add src/api.ts && git commit -m "feat(frontend): add network/subnet API client"
```

---

### Task 19: i18n keys

**Files:**
- Modify: `frontend/src/i18n.ts`

- [ ] **Step 1: Add keys to both the `ko` and `en` message maps**

Open `i18n.ts` and find the two message objects (Korean and English). Add the following keys to **both** (Korean values shown first, then the English values to use in the `en` map):

Korean (`ko`):
```ts
  networksTitle: "네트워크",
  networkNameLabel: "VPC 이름",
  networkCreate: "VPC 생성",
  networkDelete: "삭제",
  networkEmpty: "VPC가 없습니다",
  subnetsTitle: "서브넷",
  subnetNameLabel: "서브넷 이름",
  subnetCidrLabel: "CIDR",
  subnetTypeLabel: "유형",
  subnetTypePrivate: "프라이빗 (외부 단절)",
  subnetTypePublic: "퍼블릭 (아웃바운드 허용)",
  subnetGatewayLabel: "게이트웨이 (선택)",
  subnetCreate: "서브넷 생성",
  subnetEmpty: "서브넷이 없습니다",
  vmSubnetLabel: "서브넷",
  vmSubnetNone: "없음",
```

English (`en`) — same keys with these values:
```ts
  networksTitle: "Networks",
  networkNameLabel: "VPC name",
  networkCreate: "Create VPC",
  networkDelete: "Delete",
  networkEmpty: "No VPCs yet",
  subnetsTitle: "Subnets",
  subnetNameLabel: "Subnet name",
  subnetCidrLabel: "CIDR",
  subnetTypeLabel: "Type",
  subnetTypePrivate: "Private (isolated)",
  subnetTypePublic: "Public (outbound allowed)",
  subnetGatewayLabel: "Gateway (optional)",
  subnetCreate: "Create subnet",
  subnetEmpty: "No subnets yet",
  vmSubnetLabel: "Subnet",
  vmSubnetNone: "None",
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && npm run build`
Expected: passes (the message maps stay structurally identical between languages).

- [ ] **Step 3: Commit**

```bash
cd frontend && git add src/i18n.ts && git commit -m "feat(frontend): add network i18n keys"
```

---

### Task 20: Networks panel and subnet form in App.tsx

**Files:**
- Modify: `frontend/src/App.tsx`

> Context: `App.tsx` uses TanStack Query for reads and mutations and a `selectedProjectId` state. Follow the existing query/mutation pattern used for VMs (see the `useQuery`/`useMutation` calls already in the file). Use the existing `messages`/translation helper for all visible strings.

- [ ] **Step 1: Add imports and queries**

Add to the api import in `App.tsx`:

```ts
import {
  // ...existing imports...
  fetchNetworks,
  createNetwork,
  deleteNetwork,
  fetchSubnets,
  createSubnet,
  deleteSubnet,
  type Network,
  type Subnet,
} from "./api";
```

Add component state near the other `useState` hooks:

```ts
const [selectedNetworkId, setSelectedNetworkId] = useState<string | null>(null);
const [newNetworkName, setNewNetworkName] = useState("");
const [newSubnetName, setNewSubnetName] = useState("");
const [newSubnetCidr, setNewSubnetCidr] = useState("");
const [newSubnetType, setNewSubnetType] = useState("private");
```

Add queries following the existing VM query pattern (use `selectedProjectId`):

```ts
const networksQuery = useQuery({
  queryKey: ["networks", selectedProjectId],
  queryFn: () => fetchNetworks(selectedProjectId!),
  enabled: Boolean(selectedProjectId),
});

const subnetsQuery = useQuery({
  queryKey: ["subnets", selectedProjectId, selectedNetworkId],
  queryFn: () => fetchSubnets(selectedProjectId!, selectedNetworkId!),
  enabled: Boolean(selectedProjectId && selectedNetworkId),
});
```

- [ ] **Step 2: Add mutations**

Following the existing VM mutation pattern (which calls `queryClient.invalidateQueries`), add:

```ts
const createNetworkMutation = useMutation({
  mutationFn: () => createNetwork(selectedProjectId!, newNetworkName),
  onSuccess: () => {
    setNewNetworkName("");
    queryClient.invalidateQueries({ queryKey: ["networks", selectedProjectId] });
  },
});

const deleteNetworkMutation = useMutation({
  mutationFn: (networkId: string) => deleteNetwork(selectedProjectId!, networkId),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: ["networks", selectedProjectId] }),
});

const createSubnetMutation = useMutation({
  mutationFn: () =>
    createSubnet(selectedProjectId!, selectedNetworkId!, {
      name: newSubnetName,
      cidr: newSubnetCidr,
      type: newSubnetType,
    }),
  onSuccess: () => {
    setNewSubnetName("");
    setNewSubnetCidr("");
    queryClient.invalidateQueries({ queryKey: ["subnets", selectedProjectId, selectedNetworkId] });
  },
});

const deleteSubnetMutation = useMutation({
  mutationFn: (subnetId: string) => deleteSubnet(selectedProjectId!, selectedNetworkId!, subnetId),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: ["subnets", selectedProjectId, selectedNetworkId] }),
});
```

- [ ] **Step 3: Render the Networks panel**

Add a panel in the JSX next to the Projects/VMs panels (reuse the existing `panel`/`card` CSS classes used by other panels). Render the VPC list (clicking selects `selectedNetworkId`), a create-VPC form, then the subnet list + create-subnet form for the selected VPC:

```tsx
<section className="panel">
  <h2>{t("networksTitle")}</h2>
  <form
    onSubmit={(e) => {
      e.preventDefault();
      if (newNetworkName.trim()) createNetworkMutation.mutate();
    }}
  >
    <input
      aria-label={t("networkNameLabel")}
      placeholder={t("networkNameLabel")}
      value={newNetworkName}
      onChange={(e) => setNewNetworkName(e.target.value)}
    />
    <button type="submit">{t("networkCreate")}</button>
  </form>
  <ul>
    {(networksQuery.data ?? []).map((n: Network) => (
      <li key={n.id}>
        <button onClick={() => setSelectedNetworkId(n.id)}>{n.name}</button>
        <button onClick={() => deleteNetworkMutation.mutate(n.id)}>{t("networkDelete")}</button>
      </li>
    ))}
    {(networksQuery.data ?? []).length === 0 && <li>{t("networkEmpty")}</li>}
  </ul>

  {selectedNetworkId && (
    <div>
      <h3>{t("subnetsTitle")}</h3>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (newSubnetName.trim() && newSubnetCidr.trim()) createSubnetMutation.mutate();
        }}
      >
        <input
          aria-label={t("subnetNameLabel")}
          placeholder={t("subnetNameLabel")}
          value={newSubnetName}
          onChange={(e) => setNewSubnetName(e.target.value)}
        />
        <input
          aria-label={t("subnetCidrLabel")}
          placeholder="10.0.0.0/24"
          value={newSubnetCidr}
          onChange={(e) => setNewSubnetCidr(e.target.value)}
        />
        <select
          aria-label={t("subnetTypeLabel")}
          value={newSubnetType}
          onChange={(e) => setNewSubnetType(e.target.value)}
        >
          <option value="private">{t("subnetTypePrivate")}</option>
          <option value="public">{t("subnetTypePublic")}</option>
        </select>
        <button type="submit">{t("subnetCreate")}</button>
      </form>
      <ul>
        {(subnetsQuery.data ?? []).map((s: Subnet) => (
          <li key={s.id}>
            {s.name} — {s.cidr} ({s.type})
            <button onClick={() => deleteSubnetMutation.mutate(s.id)}>{t("networkDelete")}</button>
          </li>
        ))}
        {(subnetsQuery.data ?? []).length === 0 && <li>{t("subnetEmpty")}</li>}
      </ul>
    </div>
  )}
</section>
```

> Use whatever the file's existing translation accessor is named (e.g. `t(...)` or `messages[language]....`). Match the surrounding code exactly rather than introducing a new helper.

- [ ] **Step 4: Typecheck/build**

Run: `cd frontend && npm run build`
Expected: passes.

- [ ] **Step 5: Commit**

```bash
cd frontend && git add src/App.tsx && git commit -m "feat(frontend): add networks and subnets panel"
```

---

### Task 21: Subnet selector in the VM create form + detail display

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Add subnet selection state and feed subnets for the project**

Add state near the VM form state:

```ts
const [selectedSubnetId, setSelectedSubnetId] = useState<string>("");
```

To populate a project-wide subnet list for the dropdown, add a query that flattens subnets across the project's networks. Use the simplest correct approach: when exactly one network exists keep using `subnetsQuery`; otherwise fetch per network. For the MVP dropdown, reuse `subnetsQuery.data` scoped to `selectedNetworkId` and require the user to pick a VPC first. Render the dropdown in the existing VM create form (next to image/flavor selects):

```tsx
<select
  aria-label={t("vmSubnetLabel")}
  value={selectedSubnetId}
  onChange={(e) => setSelectedSubnetId(e.target.value)}
>
  <option value="">{t("vmSubnetNone")}</option>
  {(subnetsQuery.data ?? []).map((s: Subnet) => (
    <option key={s.id} value={s.id}>
      {s.name} ({s.cidr})
    </option>
  ))}
</select>
```

- [ ] **Step 2: Include `subnetId` in the create-VM mutation**

Find the existing `createVirtualMachine` call in the VM create mutation and add `subnetId`:

```ts
createVirtualMachine(selectedProjectId!, {
  name: /* existing name state */,
  imageId: /* existing image state */,
  flavorId: /* existing flavor state */,
  subnetId: selectedSubnetId || undefined,
});
```

- [ ] **Step 3: Show the subnet in the VM detail panel**

In the VM detail rendering, add a row that displays the VM's subnet. The detail payload's `virtualMachine.subnetId` carries the id; display it (or `vmSubnetNone` when empty):

```tsx
<div>
  <span>{t("vmSubnetLabel")}</span>
  <span>{detail.virtualMachine.subnetId || t("vmSubnetNone")}</span>
</div>
```

- [ ] **Step 4: Build**

Run: `cd frontend && npm run build`
Expected: passes.

- [ ] **Step 5: Commit**

```bash
cd frontend && git add src/App.tsx && git commit -m "feat(frontend): select subnet on VM create and show it in detail"
```

---

### Task 22: Docs + full verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document the network APIs and env var**

In `README.md`, under the "Catalog APIs" curl block, add:

```bash
curl http://localhost:8080/api/v1/projects/platform/networks
curl -X POST http://localhost:8080/api/v1/projects/platform/networks \
  -H 'Content-Type: application/json' -d '{"name":"app"}'
curl -X POST http://localhost:8080/api/v1/projects/platform/networks/platform-app/subnets \
  -H 'Content-Type: application/json' -d '{"name":"web","cidr":"10.0.0.0/24","type":"private"}'
curl http://localhost:8080/api/v1/projects/platform/networks/platform-app/subnets
```

In the "KubeVirt provisioning settings" block, add:

```bash
KLOUDIA_OVN_EXTERNAL_NETWORK=ovn-vpc-external-network
```

And add a sentence: "Subnets can be `private` (isolated) or `public` (outbound SNAT egress via a kube-ovn VPC NAT gateway). Public subnets require kube-ovn's VPC NAT gateway feature and an external provider network configured at the infra layer."

- [ ] **Step 2: Run the full backend suite**

Run: `cd backend && go test ./...`
Expected: PASS (store integration test skipped without a DB URL).

- [ ] **Step 3: Run the frontend build**

Run: `cd frontend && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md && git commit -m "docs: document VPC/subnet network APIs"
```

---

## Verification Checklist (end-to-end, manual on test cluster)

These require the live VM-based test cluster with kube-ovn + Multus installed; run after the plan is implemented (per spec §11 integration strategy):

- [ ] Create a VPC via the portal; confirm a kube-ovn `Vpc` CR appears (`kubectl get vpc`).
- [ ] Create a private subnet with a chosen CIDR; confirm `Subnet` CR + NAD exist and CIDR matches.
- [ ] Create a second subnet in the same VPC with an overlapping CIDR; confirm the API returns `SUBNET_CIDR_OVERLAP` (409).
- [ ] Create a VM on the subnet; confirm the guest has one NIC with an IP inside the subnet CIDR and no pod-network NIC.
- [ ] Confirm a private-subnet VM cannot reach the internet; a public-subnet VM can (egress only).
- [ ] Confirm deleting a subnet with an attached VM is rejected (`SUBNET_IN_USE`), and deleting a VPC with subnets is rejected (`NETWORK_IN_USE`).
```
