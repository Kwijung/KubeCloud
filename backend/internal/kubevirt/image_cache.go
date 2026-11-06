package kubevirt

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/kloudia/kloudia/backend/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const gib = int64(1) << 30

// BuildImageCacheDataVolume imports an image once into the cluster. VMs then clone it instead of downloading
// the URL. It is sized to the smallest root disk that holds the image, rounded up to whole GiB; KubeVirt
// grows a VM's disk image to its flavor's size when the VM starts.
func BuildImageCacheDataVolume(namespace, storageClassName, accessMode string, image domain.Image) map[string]any {
	size := gib
	if image.MinRootDiskBytes != nil {
		size = (*image.MinRootDiskBytes + gib - 1) / gib * gib
	}
	if accessMode == "" {
		accessMode = "ReadWriteOnce"
	}
	return map[string]any{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
		"metadata": map[string]any{
			"name":      domain.ImageCacheVolumeName(image.ID),
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				"kloudia.io/role":              "image-cache",
				"kloudia.io/image-id":          image.ID,
			},
		},
		"spec": map[string]any{
			"source": map[string]any{"http": map[string]any{"url": image.SourceURL}},
			"pvc": map[string]any{
				"accessModes":      []any{accessMode},
				"resources":        map[string]any{"requests": map[string]any{"storage": formatBytes(size)}},
				"storageClassName": storageClassName,
			},
		},
	}
}

// ImageCacheStore is what the image cache needs from the database.
type ImageCacheStore interface {
	// ListImagesNeedingCache returns images of known size whose cache is not yet Ready or Failed.
	ListImagesNeedingCache(ctx context.Context) ([]domain.Image, error)
	SetImageCacheState(ctx context.Context, imageID string, state domain.ImageCacheState) error
}

// ImageCache imports images into the cluster and tracks their caches.
type ImageCache struct {
	namespace        string
	storageClassName string
	accessMode       string
	applier          Applier
	reader           *StatusReader
	store            ImageCacheStore
}

func NewImageCache(namespace, storageClassName, accessMode string, applier Applier, reader *StatusReader, store ImageCacheStore) *ImageCache {
	return &ImageCache{namespace: namespace, storageClassName: storageClassName, accessMode: accessMode, applier: applier, reader: reader, store: store}
}

// Run reconciles every interval until ctx ends. The API runs as a single replica, so one loop owns caches.
func (c *ImageCache) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("image cache: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileOnce moves every image that needs a cache one step: start the import, or record its outcome.
func (c *ImageCache) ReconcileOnce(ctx context.Context) error {
	images, err := c.store.ListImagesNeedingCache(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, image := range images {
		if err := c.reconcileImage(ctx, image); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *ImageCache) reconcileImage(ctx context.Context, image domain.Image) error {
	state := domain.ImageCacheState{
		CacheVolume: image.CacheVolume, CacheStatus: image.CacheStatus, Status: image.Status, StatusMessage: image.StatusMessage,
	}
	if state.CacheVolume == "" {
		// An image registered before caching existed: it stays usable (VMs download the URL) while its cache
		// is built in the background.
		state.CacheVolume = domain.ImageCacheVolumeName(image.ID)
		state.CacheStatus = domain.ImageCachePending
	}
	if state.CacheStatus == domain.ImageCacheImporting {
		dv, err := c.reader.get(ctx, dataVolumeGVR, state.CacheVolume)
		switch {
		case apierrors.IsNotFound(err):
			state.CacheStatus = domain.ImageCachePending // deleted from under us: import again
		case err != nil:
			return err
		case statusField(dv, "phase") == "Succeeded":
			state.CacheStatus = domain.ImageCacheReady
			if state.Status == "Importing" {
				state.Status, state.StatusMessage = "Ready", ""
			}
		default:
			pvc, err := c.reader.get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, state.CacheVolume)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			events, err := c.reader.listImportEvents(ctx, state.CacheVolume, pvc)
			if err != nil {
				return err
			}
			if failure := importFailure(events); failure != "" {
				state.CacheStatus = domain.ImageCacheFailed
				state.StatusMessage = failure
				// A new image cannot be used; one that was already Ready keeps working by downloading its URL.
				if state.Status == "Importing" {
					state.Status = "Failed"
				}
				// The failed import retries forever otherwise.
				_ = c.applier.Delete(ctx, c.dataVolumeRef(state.CacheVolume))
			}
		}
	}
	if state.CacheStatus == domain.ImageCachePending {
		if err := c.applier.Apply(ctx, BuildImageCacheDataVolume(c.namespace, c.storageClassName, c.accessMode, image)); err != nil {
			return err
		}
		state.CacheStatus = domain.ImageCacheImporting
	}
	if state == (domain.ImageCacheState{CacheVolume: image.CacheVolume, CacheStatus: image.CacheStatus, Status: image.Status, StatusMessage: image.StatusMessage}) {
		return nil
	}
	return c.store.SetImageCacheState(ctx, image.ID, state)
}

// Delete removes an image's cache. VM disks cloned from it are independent volumes and stay.
func (c *ImageCache) Delete(ctx context.Context, imageID string) error {
	return c.applier.Delete(ctx, c.dataVolumeRef(domain.ImageCacheVolumeName(imageID)))
}

func (c *ImageCache) dataVolumeRef(name string) map[string]any {
	return map[string]any{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
		"metadata":   map[string]any{"name": name, "namespace": c.namespace},
	}
}

var dataVolumeGVR = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}
