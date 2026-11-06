package kubevirt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/kloudia/kloudia/backend/internal/domain"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	imageCacheRoleLabel     = "kloudia.io/role"
	imageCacheRole          = "image-cache"
	imageIDAnnotation       = "kloudia.io/image-id"
	imageSourceAnnotation   = "kloudia.io/source-url"
	bindImmediateAnnotation = "cdi.kubevirt.io/storage.bind.immediate.requested"
)

var (
	dataVolumeGVR = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}
	pvcGVR        = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
)

// BuildImageCacheDataVolume imports an image once into the cluster. VMs then clone it instead of downloading
// the URL. It is sized with domain.ImageCacheBytes; KubeVirt grows a VM's disk image to its flavor's size
// when the VM starts. Nothing ever runs on the cache, so it asks CDI to bind its volume immediately even on
// storage classes that wait for a consumer.
func BuildImageCacheDataVolume(namespace, storageClassName, accessMode string, image domain.Image) map[string]any {
	size := int64(1) << 30
	if image.MinRootDiskBytes != nil {
		size = domain.ImageCacheBytes(*image.MinRootDiskBytes)
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
			// Image IDs can exceed the 63-character limit of label values, so they go in annotations.
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "kloudia",
				imageCacheRoleLabel:            imageCacheRole,
			},
			"annotations": map[string]any{
				imageIDAnnotation:       image.ID,
				imageSourceAnnotation:   image.SourceURL,
				bindImmediateAnnotation: "true",
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
	// ListImagesWithCache returns images of known size whose cache is to be built or is Ready.
	ListImagesWithCache(ctx context.Context) ([]domain.Image, error)
	// SetImageCacheState records state if the image still has the cache status in from. It reports false
	// when the image is gone or changed meanwhile.
	SetImageCacheState(ctx context.Context, imageID string, from string, state domain.ImageCacheState) (bool, error)
	// ImageCacheVolumes returns the cache volume names of all existing images.
	ImageCacheVolumes(ctx context.Context) (map[string]bool, error)
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
// Each pass has a deadline, so a hung API call delays caching instead of stopping it.
func (c *ImageCache) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := c.ReconcileOnce(passCtx); err != nil && ctx.Err() == nil {
			log.Printf("image cache: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileOnce moves every image's cache one step, then removes cache volumes no image owns.
func (c *ImageCache) ReconcileOnce(ctx context.Context) error {
	images, err := c.store.ListImagesWithCache(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, image := range images {
		if err := c.reconcileImage(ctx, image); err != nil {
			errs = append(errs, fmt.Errorf("image %s: %w", image.ID, err))
		}
	}
	if err := c.sweepOrphans(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (c *ImageCache) reconcileImage(ctx context.Context, image domain.Image) error {
	from := domain.ImageCacheState{CacheVolume: image.CacheVolume, CacheStatus: image.CacheStatus, Status: image.Status, StatusMessage: image.StatusMessage}
	state := from
	if state.CacheStatus == "" {
		// An image registered before caching existed: it stays usable (VMs download the URL) while its cache
		// is built in the background.
		state.CacheVolume = domain.ImageCacheVolumeName(image.ID)
		state.CacheStatus = domain.ImageCachePending
	}
	fail := func(message string) {
		state.CacheStatus = domain.ImageCacheFailed
		state.StatusMessage = message
		// A new image cannot be used; one that was already Ready keeps working by downloading its URL.
		if state.Status == "Importing" {
			state.Status = "Failed"
		}
	}
	ready := func() {
		state.CacheStatus = domain.ImageCacheReady
		if state.Status == "Importing" {
			state.Status, state.StatusMessage = "Ready", ""
		}
	}

	switch state.CacheStatus {
	case domain.ImageCacheReady:
		// VMs clone the cache's volume; if it is gone, import it again and let VMs download meanwhile.
		if _, err := c.reader.get(ctx, pvcGVR, state.CacheVolume); apierrors.IsNotFound(err) {
			state.CacheStatus = domain.ImageCachePending
		} else if err != nil {
			return err
		}
	case domain.ImageCacheImporting:
		dv, err := c.reader.get(ctx, dataVolumeGVR, state.CacheVolume)
		switch {
		case apierrors.IsNotFound(err):
			// CDI may remove a finished DataVolume and keep its volume.
			if bound, err := c.volumeBound(ctx, state.CacheVolume); err != nil {
				return err
			} else if bound {
				ready()
			} else {
				state.CacheStatus = domain.ImageCachePending
			}
		case err != nil:
			return err
		case dv.GetAnnotations()[imageSourceAnnotation] != image.SourceURL:
			// Left over from an earlier image of the same name: replace it.
			if err := c.deleteVolume(ctx, state.CacheVolume); err != nil {
				return err
			}
			state.CacheStatus = domain.ImageCachePending
		case statusField(dv, "phase") == "Succeeded":
			ready()
		case statusField(dv, "phase") == "Failed":
			fail("the image import failed")
			_ = c.deleteVolume(ctx, state.CacheVolume)
		default:
			pvc, err := c.reader.get(ctx, pvcGVR, state.CacheVolume)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if apierrors.IsNotFound(err) {
				pvc = nil
			}
			events, err := c.reader.listImportEvents(ctx, state.CacheVolume, dv, pvc)
			if err != nil {
				return err
			}
			if failure := importFailure(events); failure != "" {
				fail(failure)
				// CDI retries a failed import forever otherwise.
				if err := c.deleteVolume(ctx, state.CacheVolume); err != nil {
					log.Printf("image cache: delete failed import %s: %v", state.CacheVolume, err)
				}
			}
		}
	}

	applied := false
	if state.CacheStatus == domain.ImageCachePending {
		existing, err := c.reader.get(ctx, dataVolumeGVR, state.CacheVolume)
		switch {
		case err == nil && existing.GetAnnotations()[imageSourceAnnotation] == image.SourceURL:
			state.CacheStatus = domain.ImageCacheImporting // already importing this image
		case err == nil:
			// A DataVolume of the same name from an earlier image: remove it and import on the next pass.
			if err := c.deleteVolume(ctx, state.CacheVolume); err != nil {
				return err
			}
		case !apierrors.IsNotFound(err):
			return err
		default:
			err := c.applier.Apply(ctx, BuildImageCacheDataVolume(c.namespace, c.storageClassName, c.accessMode, image))
			switch {
			case err == nil:
				state.CacheStatus, applied = domain.ImageCacheImporting, true
			case apierrors.IsInvalid(err) || apierrors.IsForbidden(err) || apierrors.IsBadRequest(err):
				fail("the image cache could not be created: " + err.Error())
			default:
				return err
			}
		}
	}

	if state == from {
		return nil
	}
	saved, err := c.store.SetImageCacheState(ctx, image.ID, from.CacheStatus, state)
	if err != nil {
		return err
	}
	if !saved && applied {
		// The image was deleted (or changed) while its cache was being created; do not leave the import behind.
		return c.deleteVolume(ctx, state.CacheVolume)
	}
	return nil
}

// volumeBound reports whether the cache's volume exists and is bound.
func (c *ImageCache) volumeBound(ctx context.Context, name string) (bool, error) {
	pvc, err := c.reader.get(ctx, pvcGVR, name)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return statusField(pvc, "phase") == "Bound", nil
}

// sweepOrphans deletes cache DataVolumes and volumes whose image no longer exists, for instance when the
// image was deleted while its cache was being created, or a cache delete failed.
func (c *ImageCache) sweepOrphans(ctx context.Context) error {
	owned, err := c.store.ImageCacheVolumes(ctx)
	if err != nil {
		return err
	}
	selector := v1.ListOptions{LabelSelector: imageCacheRoleLabel + "=" + imageCacheRole}
	var errs []error
	for _, gvr := range []schema.GroupVersionResource{dataVolumeGVR, pvcGVR} {
		list, err := c.reader.client.Resource(gvr).Namespace(c.namespace).List(ctx, selector)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, item := range list.Items {
			if !owned[item.GetName()] {
				errs = append(errs, c.deleteVolume(ctx, item.GetName()))
			}
		}
	}
	return errors.Join(errs...)
}

// Delete removes an image's cache. VM disks cloned from it are independent volumes and stay.
func (c *ImageCache) Delete(ctx context.Context, imageID string) error {
	return c.deleteVolume(ctx, domain.ImageCacheVolumeName(imageID))
}

// deleteVolume removes a cache's DataVolume and its volume. The volume is deleted explicitly because CDI
// may already have removed a finished DataVolume, leaving the volume without an owner.
func (c *ImageCache) deleteVolume(ctx context.Context, name string) error {
	return errors.Join(
		c.applier.Delete(ctx, c.ref("cdi.kubevirt.io/v1beta1", "DataVolume", name)),
		c.applier.Delete(ctx, c.ref("v1", "PersistentVolumeClaim", name)),
	)
}

func (c *ImageCache) ref(apiVersion, kind, name string) map[string]any {
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": map[string]any{"name": name, "namespace": c.namespace}}
}
