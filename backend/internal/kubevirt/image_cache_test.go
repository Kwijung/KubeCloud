package kubevirt

import (
	"context"
	"strings"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestImageCacheVolumeName(t *testing.T) {
	if got := domain.ImageCacheVolumeName("public-debian-12"); got != "image-public-debian-12" {
		t.Fatalf("short id: got %q", got)
	}
	long := strings.Repeat("a", 80)
	name := domain.ImageCacheVolumeName(long)
	if len(name) > 63 || !strings.HasPrefix(name, "image-") || name == domain.ImageCacheVolumeName(long+"b") {
		t.Fatalf("long ids must give distinct DNS-1123 names of at most 63 characters, got %q", name)
	}
}

func TestBuildImageCacheDataVolume(t *testing.T) {
	size := int64(3758096384) // Ubuntu 24.04: 3.5GiB
	min := domain.MinRootDiskBytes(size)
	image := domain.Image{ID: "public-noble", SourceURL: "https://example.com/noble.img", VirtualSizeBytes: &size, MinRootDiskBytes: &min}

	dv := BuildImageCacheDataVolume("kloudia-vms", "kv", "ReadWriteMany", image)

	meta := dv["metadata"].(map[string]any)
	if meta["name"] != "image-public-noble" || meta["namespace"] != "kloudia-vms" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	labels := meta["labels"].(map[string]any)
	annotations := meta["annotations"].(map[string]any)
	if labels["kloudia.io/role"] != "image-cache" || annotations["kloudia.io/image-id"] != "public-noble" ||
		annotations["kloudia.io/source-url"] != image.SourceURL || annotations["cdi.kubevirt.io/storage.bind.immediate.requested"] != "true" {
		t.Fatalf("unexpected labels %#v / annotations %#v", labels, annotations)
	}
	spec := dv["spec"].(map[string]any)
	if spec["source"].(map[string]any)["http"].(map[string]any)["url"] != image.SourceURL {
		t.Fatalf("unexpected source: %#v", spec["source"])
	}
	pvc := spec["pvc"].(map[string]any)
	if got := pvc["resources"].(map[string]any)["requests"].(map[string]any)["storage"]; got != "4Gi" {
		t.Fatalf("the cache must hold the image (3.5GiB + overhead) rounded up to whole GiB, got %v", got)
	}
	if pvc["storageClassName"] != "kv" || pvc["accessModes"].([]any)[0] != "ReadWriteMany" {
		t.Fatalf("unexpected pvc: %#v", pvc)
	}
}

func TestBuildResourcesClonesAReadyImageCache(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01"},
		Image:          domain.Image{ID: "public-noble", SourceURL: "https://example.com/noble.img", CacheVolume: "image-public-noble", CacheStatus: domain.ImageCacheReady},
		Flavor:         domain.Flavor{ID: "medium", CPU: 2, MemoryBytes: 2 << 30, RootDiskBytes: 10 << 30},
	}
	source := BuildResources("kloudia-vms", "kv", plan).DataVolume["spec"].(map[string]any)["source"].(map[string]any)
	pvc, ok := source["pvc"].(map[string]any)
	if !ok || pvc["name"] != "image-public-noble" || pvc["namespace"] != "kloudia-vms" {
		t.Fatalf("expected a clone of the image cache, got %#v", source)
	}

	plan.Image.CacheStatus = domain.ImageCacheImporting
	source = BuildResources("kloudia-vms", "kv", plan).DataVolume["spec"].(map[string]any)["source"].(map[string]any)
	if _, ok := source["http"]; !ok {
		t.Fatalf("without a ready cache the VM downloads the image, got %#v", source)
	}
}

type fakeImageCacheStore struct {
	images  []domain.Image
	states  map[string]domain.ImageCacheState
	deleted map[string]bool // images deleted while the reconciler worked
}

func (s *fakeImageCacheStore) ListImagesWithCache(context.Context) ([]domain.Image, error) {
	return s.images, nil
}

func (s *fakeImageCacheStore) SetImageCacheState(_ context.Context, id string, _ string, state domain.ImageCacheState) (bool, error) {
	if s.deleted[id] {
		return false, nil
	}
	if s.states == nil {
		s.states = map[string]domain.ImageCacheState{}
	}
	s.states[id] = state
	return true, nil
}

func (s *fakeImageCacheStore) ImageCacheVolumes(context.Context) (map[string]bool, error) {
	owned := map[string]bool{}
	for _, img := range s.images {
		if !s.deleted[img.ID] {
			owned[domain.ImageCacheVolumeName(img.ID)] = true
		}
	}
	return owned, nil
}

func cacheObject(apiVersion, kind, name, uid, sourceURL string, status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion, "kind": kind,
		"metadata": map[string]any{
			"name": name, "namespace": "kloudia-vms", "uid": uid,
			"labels":      map[string]any{"kloudia.io/role": "image-cache"},
			"annotations": map[string]any{"kloudia.io/source-url": sourceURL},
		},
		"status": status,
	}}
}

func dataVolumeObject(id, uid, phase string) *unstructured.Unstructured {
	return cacheObject("cdi.kubevirt.io/v1beta1", "DataVolume", "image-"+id, uid, "https://example.com/"+id, map[string]any{"phase": phase})
}

func errorEvent(name, uid string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata":       map[string]any{"name": name + "." + uid, "namespace": "kloudia-vms"},
		"involvedObject": map[string]any{"kind": "DataVolume", "name": name, "uid": uid},
		"type":           "Warning", "reason": "Error", "message": "Unable to connect to http data source: 404",
		"count": int64(1), "lastTimestamp": "2026-11-07T00:00:00Z",
	}}
}

func TestImageCacheReconcile(t *testing.T) {
	size := int64(117440512)
	min := domain.MinRootDiskBytes(size)
	image := func(id, status, cacheStatus string) domain.Image {
		img := domain.Image{ID: id, Status: status, CacheStatus: cacheStatus, SourceURL: "https://example.com/" + id, VirtualSizeBytes: &size, MinRootDiskBytes: &min}
		if cacheStatus != "" {
			img.CacheVolume = domain.ImageCacheVolumeName(id)
		}
		return img
	}
	store := &fakeImageCacheStore{
		images: []domain.Image{
			image("old-ready", "Ready", ""),                                // registered before caching: start a cache, stay Ready
			image("new-pending", "Importing", domain.ImageCachePending),    // just registered: start the import
			image("done", "Importing", domain.ImageCacheImporting),         // import finished
			image("broken", "Importing", domain.ImageCacheImporting),       // import keeps failing
			image("vanished", "Importing", domain.ImageCacheImporting),     // its DataVolume was deleted
			image("reregistered", "Importing", domain.ImageCacheImporting), // only old events of an earlier image
			image("stale", "Importing", domain.ImageCachePending),          // a DataVolume of an earlier image is in the way
			image("lost", "Ready", domain.ImageCacheReady),                 // its ready volume disappeared
			image("gone", "Importing", domain.ImageCachePending),           // deleted while its cache was created
		},
		deleted: map[string]bool{"gone": true},
	}
	stale := dataVolumeObject("stale", "uid-stale", "Succeeded")
	stale.SetAnnotations(map[string]string{"kloudia.io/source-url": "https://example.com/an-earlier-image"})
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Version: "v1", Resource: "events"}:                                     "EventList",
			{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}: "DataVolumeList",
			{Version: "v1", Resource: "persistentvolumeclaims"}:                     "PersistentVolumeClaimList",
		},
		dataVolumeObject("done", "uid-done", "Succeeded"),
		dataVolumeObject("broken", "uid-broken", "ImportInProgress"),
		errorEvent("image-broken", "uid-broken"),
		dataVolumeObject("reregistered", "uid-new", "ImportInProgress"),
		errorEvent("image-reregistered", "uid-old"),
		stale,
		cacheObject("cdi.kubevirt.io/v1beta1", "DataVolume", "image-orphan", "uid-orphan", "https://example.com/orphan", nil),
	)
	applier := &recordingApplier{}
	cache := NewImageCache("kloudia-vms", "kv", "ReadWriteMany", applier, NewStatusReader("kloudia-vms", "kv", client), store)

	if err := cache.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	want := map[string]domain.ImageCacheState{
		"old-ready":   {CacheVolume: "image-old-ready", CacheStatus: domain.ImageCacheImporting, Status: "Ready"},
		"new-pending": {CacheVolume: "image-new-pending", CacheStatus: domain.ImageCacheImporting, Status: "Importing"},
		"done":        {CacheVolume: "image-done", CacheStatus: domain.ImageCacheReady, Status: "Ready"},
		"broken":      {CacheVolume: "image-broken", CacheStatus: domain.ImageCacheFailed, Status: "Failed", StatusMessage: "Unable to connect to http data source: 404"},
		"lost":        {CacheVolume: "image-lost", CacheStatus: domain.ImageCacheImporting, Status: "Ready"},
	}
	for id, w := range want {
		if got := store.states[id]; got != w {
			t.Errorf("%s: got %#v, want %#v", id, got, w)
		}
	}
	// Nothing to record: re-imported (vanished), still importing (reregistered: the error event belongs to
	// an earlier DataVolume), or waiting for the old DataVolume to go (stale).
	for _, id := range []string{"vanished", "reregistered", "stale"} {
		if got, written := store.states[id]; written {
			t.Errorf("%s: expected no state change, got %#v", id, got)
		}
	}

	applied := map[string]bool{}
	for _, r := range applier.resources {
		applied[r["metadata"].(map[string]any)["name"].(string)] = true
	}
	for _, name := range []string{"image-old-ready", "image-new-pending", "image-vanished", "image-lost", "image-gone"} {
		if !applied[name] {
			t.Errorf("expected %s to be imported, applied %v", name, applied)
		}
	}
	if applied["image-stale"] {
		t.Error("a stale DataVolume must be removed before importing again")
	}
	deleted := map[string]bool{}
	for _, r := range applier.deletedResources {
		deleted[r["kind"].(string)+"/"+r["metadata"].(map[string]any)["name"].(string)] = true
	}
	for _, name := range []string{"image-broken", "image-stale", "image-gone", "image-orphan"} {
		if !deleted["DataVolume/"+name] || !deleted["PersistentVolumeClaim/"+name] {
			t.Errorf("expected %s's DataVolume and volume to be deleted, deleted %v", name, deleted)
		}
	}
	if deleted["DataVolume/image-done"] || deleted["DataVolume/image-reregistered"] {
		t.Errorf("caches in use must stay, deleted %v", deleted)
	}
}
