package store

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/kloudia/kloudia/backend/internal/domain"
)

var (
	// ErrImageNameTaken is returned when the project, or the public images, already have an image with the same name.
	ErrImageNameTaken = errors.New("an image with this name already exists in the project")
	// ErrImageInUse is returned when deleting an image that virtual machines still reference.
	ErrImageInUse = errors.New("image is used by virtual machines")
	// ErrImageSourceURLInvalid is returned when the image URL is not an absolute http(s) URL.
	ErrImageSourceURLInvalid = errors.New("image source url must be an http or https url")
	// ErrImageOSTypeInvalid is returned for an OS type other than linux or windows.
	ErrImageOSTypeInvalid = errors.New("image os type must be linux or windows")
)

// ValidateImageInput checks the fields of an image registration and returns it trimmed.
func ValidateImageInput(input domain.CreateImageInput) (domain.CreateImageInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.OSType = strings.TrimSpace(input.OSType)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	parsed, err := url.Parse(input.SourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return input, ErrImageSourceURLInvalid
	}
	if input.OSType != "linux" && input.OSType != "windows" {
		return input, ErrImageOSTypeInvalid
	}
	return input, nil
}

// CreateImage registers a project image as Ready. There is no separate import step: CDI pulls the URL into
// each VM's root disk when the VM is created.
func (p *Postgres) CreateImage(ctx context.Context, projectID string, input domain.CreateImageInput) (domain.Image, error) {
	return p.createImage(ctx, &projectID, input)
}

// CreatePublicImage registers an image every project can use. Its ID is public-<name>.
func (p *Postgres) CreatePublicImage(ctx context.Context, input domain.CreateImageInput) (domain.Image, error) {
	return p.createImage(ctx, nil, input)
}

// createImage inserts a project image (projectID set) or a public image (projectID nil). Names are unique
// within a project and among public images; a project image may share a public image's name.
func (p *Postgres) createImage(ctx context.Context, projectID *string, input domain.CreateImageInput) (domain.Image, error) {
	input, err := ValidateImageInput(input)
	if err != nil {
		return domain.Image{}, err
	}
	if input.Name == "" {
		return domain.Image{}, errors.New("image name is required")
	}
	scope, idPrefix := "public", "public"
	if projectID != nil {
		scope, idPrefix = "project", *projectID
	}
	id := makeVirtualMachineID(idPrefix, input.Name)
	// An image of known size is imported into the cluster first and is Importing until its cache is Ready.
	// One of unknown size (not qcow2) cannot be sized for a cache; it is Ready at once and VMs download it.
	status, cacheVolume, cacheStatus := "Ready", "", ""
	if input.Cache && input.VirtualSizeBytes != nil {
		status, cacheVolume, cacheStatus = "Importing", domain.ImageCacheVolumeName(id), domain.ImageCachePending
	}
	var image domain.Image
	err = p.pool.QueryRow(ctx, `
INSERT INTO images (id, project_id, name, os_type, source_url, status, virtual_size_bytes, scope, cache_volume, cache_status)
SELECT $1, $2, $3, $4, $5, $8, $6, $7, NULLIF($9, ''), $10
WHERE NOT EXISTS (
  SELECT 1 FROM images
  WHERE scope = $7 AND project_id IS NOT DISTINCT FROM $2 AND lower(name) = lower($3)
)
ON CONFLICT (id) DO NOTHING
RETURNING `+imageSelectColumns, id, projectID, input.Name, input.OSType, input.SourceURL, input.VirtualSizeBytes, scope, status, cacheVolume, cacheStatus).
		Scan(imageColumns(&image)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Image{}, ErrImageNameTaken
	}
	setMinRootDisk(&image)
	return image, err
}

// ListPublicImages returns the public images with how many VMs use each, across all projects.
func (p *Postgres) ListPublicImages(ctx context.Context) ([]domain.Image, error) {
	rows, err := p.pool.Query(ctx, `
SELECT `+imageSelectColumns+`, (SELECT count(*) FROM virtual_machines v WHERE v.image_id = images.id)
FROM images
WHERE scope = 'public'
ORDER BY created_at DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []domain.Image
	for rows.Next() {
		var image domain.Image
		var vmCount int
		if err := rows.Scan(append(imageColumns(&image), &vmCount)...); err != nil {
			return nil, err
		}
		setMinRootDisk(&image)
		image.VMCount = &vmCount
		images = append(images, image)
	}
	return images, rows.Err()
}

// DeletePublicImage removes a public image that no VM in any project references.
func (p *Postgres) DeletePublicImage(ctx context.Context, imageID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists, inUse bool
	if err := tx.QueryRow(ctx, `
SELECT
  EXISTS (SELECT 1 FROM images WHERE scope = 'public' AND id = $1),
  EXISTS (SELECT 1 FROM virtual_machines WHERE image_id = $1)`, imageID).Scan(&exists, &inUse); err != nil {
		return err
	}
	if !exists {
		return ErrImageNotFound
	}
	if inUse {
		return ErrImageInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM images WHERE scope = 'public' AND id = $1`, imageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteImage removes an image that no virtual machine references.
func (p *Postgres) DeleteImage(ctx context.Context, projectID string, imageID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists, inUse bool
	if err := tx.QueryRow(ctx, `
SELECT
  EXISTS (SELECT 1 FROM images WHERE project_id = $1 AND id = $2),
  EXISTS (SELECT 1 FROM virtual_machines WHERE image_id = $2)`, projectID, imageID).Scan(&exists, &inUse); err != nil {
		return err
	}
	if !exists {
		return ErrImageNotFound
	}
	if inUse {
		return ErrImageInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM images WHERE project_id = $1 AND id = $2`, projectID, imageID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// imageSelectColumns is the column list imageColumns scans. A public image has no project; it reads as "".
const imageSelectColumns = "id, COALESCE(project_id, ''), name, os_type, source_url, status, created_at, virtual_size_bytes, scope, COALESCE(cache_volume, ''), cache_status, status_message"

// imageColumns lists scan targets in the order of imageSelectColumns.
func imageColumns(image *domain.Image) []any {
	return []any{&image.ID, &image.ProjectID, &image.Name, &image.OSType, &image.SourceURL, &image.Status, &image.CreatedAt, &image.VirtualSizeBytes, &image.Scope, &image.CacheVolume, &image.CacheStatus, &image.StatusMessage}
}

func scanImage(row pgx.Row, image *domain.Image) error {
	if err := row.Scan(imageColumns(image)...); err != nil {
		return err
	}
	setMinRootDisk(image)
	return nil
}

func setMinRootDisk(image *domain.Image) {
	image.MinRootDiskBytes = nil
	if image.VirtualSizeBytes != nil {
		min := domain.MinRootDiskBytes(*image.VirtualSizeBytes)
		image.MinRootDiskBytes = &min
	}
}

// SetImageVirtualSize records the probed virtual size of an image.
func (p *Postgres) SetImageVirtualSize(ctx context.Context, imageID string, virtualSizeBytes int64) error {
	_, err := p.pool.Exec(ctx, `UPDATE images SET virtual_size_bytes = $2 WHERE id = $1`, imageID, virtualSizeBytes)
	return err
}

// ListImagesWithoutVirtualSize returns images of every project whose size has not been probed yet.
func (p *Postgres) ListImagesWithoutVirtualSize(ctx context.Context) ([]domain.Image, error) {
	rows, err := p.pool.Query(ctx, `
SELECT `+imageSelectColumns+`
FROM images
WHERE virtual_size_bytes IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []domain.Image
	for rows.Next() {
		var image domain.Image
		if err := scanImage(rows, &image); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

// ListImagesWithCache returns images of known size whose cache is to be built, is importing, or is Ready
// (checked for a lost volume). Failed caches are not retried.
func (p *Postgres) ListImagesWithCache(ctx context.Context) ([]domain.Image, error) {
	rows, err := p.pool.Query(ctx, `
SELECT `+imageSelectColumns+`
FROM images
WHERE virtual_size_bytes IS NOT NULL
  AND (cache_status IN ('Pending', 'Importing', 'Ready') OR (cache_status = '' AND status = 'Ready'))
ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []domain.Image
	for rows.Next() {
		var image domain.Image
		if err := scanImage(rows, &image); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

// SetImageCacheState records the image cache reconciler's view of an image, only if the image still has the
// cache status the reconciler read (from). It reports false when the image was deleted or changed meanwhile.
func (p *Postgres) SetImageCacheState(ctx context.Context, imageID string, from string, state domain.ImageCacheState) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
UPDATE images SET cache_volume = NULLIF($2, ''), cache_status = $3, status = $4, status_message = $5
WHERE id = $1 AND cache_status = $6`,
		imageID, state.CacheVolume, state.CacheStatus, state.Status, state.StatusMessage, from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ImageCacheVolumes returns the cache volume names that existing images own.
func (p *Postgres) ImageCacheVolumes(ctx context.Context) (map[string]bool, error) {
	rows, err := p.pool.Query(ctx, `SELECT cache_volume FROM images WHERE cache_volume IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owned := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		owned[name] = true
	}
	return owned, rows.Err()
}
