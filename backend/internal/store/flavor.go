package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kloudia/kloudia/backend/internal/domain"
)

var (
	// ErrFlavorInvalid is returned for a flavor whose size is out of range or whose name is empty.
	ErrFlavorInvalid = errors.New("flavor is invalid")
	// ErrFlavorNameTaken is returned when another flavor already has the name.
	ErrFlavorNameTaken = errors.New("a flavor with this name already exists")
	// ErrFlavorInUse is returned when resizing or deleting a flavor that VMs use.
	ErrFlavorInUse = errors.New("flavor is used by virtual machines")
)

const gib = int64(1) << 30

// validateFlavorSize checks a flavor's size against what a KubeVirt VM can sensibly use.
func validateFlavorSize(cpu int, memoryBytes, rootDiskBytes int64) error {
	switch {
	case cpu < 1 || cpu > 64:
		return fmt.Errorf("cpu %d must be 1-64: %w", cpu, ErrFlavorInvalid)
	case memoryBytes < 128<<20 || memoryBytes > 512*gib:
		return fmt.Errorf("memory %d bytes must be 128MiB-512GiB: %w", memoryBytes, ErrFlavorInvalid)
	case rootDiskBytes < gib || rootDiskBytes > 4096*gib:
		return fmt.Errorf("root disk %d bytes must be 1GiB-4TiB: %w", rootDiskBytes, ErrFlavorInvalid)
	}
	return nil
}

const flavorSelectColumns = "id, name, cpu, memory_bytes, root_disk_bytes, enabled"

func flavorColumns(f *domain.Flavor) []any {
	return []any{&f.ID, &f.Name, &f.CPU, &f.MemoryBytes, &f.RootDiskBytes, &f.Enabled}
}

// ListAllFlavors returns every flavor, disabled ones included, with how many VMs use each.
func (p *Postgres) ListAllFlavors(ctx context.Context) ([]domain.Flavor, error) {
	rows, err := p.pool.Query(ctx, `
SELECT `+flavorSelectColumns+`, (SELECT count(*) FROM virtual_machines v WHERE v.flavor_id = flavors.id)
FROM flavors
ORDER BY cpu, memory_bytes, root_disk_bytes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var flavors []domain.Flavor
	for rows.Next() {
		var f domain.Flavor
		var vmCount int
		if err := rows.Scan(append(flavorColumns(&f), &vmCount)...); err != nil {
			return nil, err
		}
		f.VMCount = &vmCount
		flavors = append(flavors, f)
	}
	return flavors, rows.Err()
}

// CreateFlavor adds an enabled flavor. Its ID is derived from the name.
func (p *Postgres) CreateFlavor(ctx context.Context, input domain.FlavorInput) (domain.Flavor, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Flavor{}, fmt.Errorf("name is required: %w", ErrFlavorInvalid)
	}
	if err := validateFlavorSize(input.CPU, input.MemoryBytes, input.RootDiskBytes); err != nil {
		return domain.Flavor{}, err
	}
	// The ID is random rather than derived from the name: names may be non-ASCII or differ only in
	// punctuation, and a flavor can be renamed. The unique index on lower(name) keeps names distinct.
	id, err := newFlavorID()
	if err != nil {
		return domain.Flavor{}, err
	}
	var f domain.Flavor
	err = p.pool.QueryRow(ctx, `
INSERT INTO flavors (id, name, cpu, memory_bytes, root_disk_bytes, enabled)
VALUES ($1, $2, $3, $4, $5, true)
RETURNING `+flavorSelectColumns, id, name, input.CPU, input.MemoryBytes, input.RootDiskBytes).Scan(flavorColumns(&f)...)
	if isUniqueViolation(err) {
		return domain.Flavor{}, ErrFlavorNameTaken
	}
	return f, err
}

func newFlavorID() (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "flv-" + hex.EncodeToString(raw), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// UpdateFlavor applies the set fields. Resizing is refused while a VM uses the flavor, because that VM was
// created with the old size and the catalog would no longer describe it.
func (p *Postgres) UpdateFlavor(ctx context.Context, id string, update domain.FlavorUpdate) (domain.Flavor, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Flavor{}, err
	}
	defer tx.Rollback(ctx)

	var f domain.Flavor
	if err := tx.QueryRow(ctx, `SELECT `+flavorSelectColumns+` FROM flavors WHERE id = $1 FOR UPDATE`, id).Scan(flavorColumns(&f)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Flavor{}, ErrFlavorNotFound
		}
		return domain.Flavor{}, err
	}
	// A field counts as a resize only when its value changes, so an edit form that sends every field can
	// still rename a flavor in use.
	resize := (update.CPU != nil && *update.CPU != f.CPU) ||
		(update.MemoryBytes != nil && *update.MemoryBytes != f.MemoryBytes) ||
		(update.RootDiskBytes != nil && *update.RootDiskBytes != f.RootDiskBytes)
	if resize {
		var inUse bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM virtual_machines WHERE flavor_id = $1)`, id).Scan(&inUse); err != nil {
			return domain.Flavor{}, err
		}
		if inUse {
			return domain.Flavor{}, ErrFlavorInUse
		}
	}
	if update.Name != nil {
		f.Name = strings.TrimSpace(*update.Name)
		if f.Name == "" {
			return domain.Flavor{}, fmt.Errorf("name is required: %w", ErrFlavorInvalid)
		}
	}
	if update.CPU != nil {
		f.CPU = *update.CPU
	}
	if update.MemoryBytes != nil {
		f.MemoryBytes = *update.MemoryBytes
	}
	if update.RootDiskBytes != nil {
		f.RootDiskBytes = *update.RootDiskBytes
	}
	if update.Enabled != nil {
		f.Enabled = *update.Enabled
	}
	if err := validateFlavorSize(f.CPU, f.MemoryBytes, f.RootDiskBytes); err != nil {
		return domain.Flavor{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE flavors SET name = $2, cpu = $3, memory_bytes = $4, root_disk_bytes = $5, enabled = $6 WHERE id = $1`,
		id, f.Name, f.CPU, f.MemoryBytes, f.RootDiskBytes, f.Enabled); err != nil {
		if isUniqueViolation(err) {
			return domain.Flavor{}, ErrFlavorNameTaken
		}
		return domain.Flavor{}, err
	}
	return f, tx.Commit(ctx)
}

// DeleteFlavor removes a flavor no VM uses. A flavor still in use can be disabled instead.
func (p *Postgres) DeleteFlavor(ctx context.Context, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Locking the row waits out VM creations holding it FOR SHARE, so the in-use check below is final.
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM flavors WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFlavorNotFound
		}
		return err
	}
	var inUse bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM virtual_machines WHERE flavor_id = $1)`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrFlavorInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM flavors WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
