//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestImageCache registers an image, waits for it to be imported into the cluster, creates a VM that clones
// the cached copy instead of downloading, and checks the cache goes away with the image.
func TestImageCache(t *testing.T) {
	e := newEnv(t)
	const project = "platform"
	suffix := fmt.Sprintf("%d", time.Now().Unix()%100000)

	// A URL that cannot be fetched is refused at registration, not discovered by each VM later.
	if code := e.call(t, http.MethodPost, "/admin/images", map[string]string{
		"name": "e2e-missing-" + suffix, "sourceUrl": "https://download.cirros-cloud.net/0.6.2/does-not-exist.img",
	}, nil); code != http.StatusBadRequest {
		t.Fatalf("registering an unreachable URL: expected 400, got %d", code)
	}

	var created struct {
		Data struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			CacheVolume string `json:"cacheVolume"`
		} `json:"data"`
	}
	e.mustCall(t, http.MethodPost, "/admin/images", map[string]string{
		"name": "e2e-cirros-" + suffix, "osType": "linux",
		"sourceUrl": "https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img",
	}, http.StatusCreated, &created)
	imageID, cacheVolume := created.Data.ID, created.Data.CacheVolume
	// Registered before the VM's cleanup, so it runs after the VM is gone.
	t.Cleanup(func() {
		if code := e.call(t, http.MethodDelete, "/admin/images/"+imageID, nil, nil); code != http.StatusNoContent {
			t.Errorf("delete image %s: %d", imageID, code)
			return
		}
		eventually(t, 2*time.Minute, "the image cache to be deleted", func() (bool, string) {
			_, err := e.get(t, gvrDV, vmNamespace, cacheVolume)
			return err != nil, "cache still present"
		})
	})
	if created.Data.Status != "Importing" || cacheVolume == "" {
		t.Fatalf("a registered image of known size starts Importing with a cache, got %#v", created.Data)
	}

	// The image cannot be used until its cache is ready.
	if code := e.call(t, http.MethodPost, "/projects/"+project+"/vms", map[string]any{
		"name": "cache-early-" + suffix, "imageId": imageID, "flavorId": "small",
	}, nil); code != http.StatusConflict {
		t.Fatalf("creating a VM from an importing image: expected 409, got %d", code)
	}

	eventually(t, 5*time.Minute, "the image to be imported", func() (bool, string) {
		var resp struct {
			Data []struct {
				ID            string `json:"id"`
				Status        string `json:"status"`
				CacheStatus   string `json:"cacheStatus"`
				StatusMessage string `json:"statusMessage"`
			} `json:"data"`
		}
		e.mustCall(t, http.MethodGet, "/admin/images", nil, http.StatusOK, &resp)
		for _, img := range resp.Data {
			if img.ID == imageID {
				if img.Status == "Failed" {
					t.Fatalf("image import failed: %s", img.StatusMessage)
				}
				return img.Status == "Ready" && img.CacheStatus == "Ready", img.Status + "/" + img.CacheStatus
			}
		}
		return false, "image not listed"
	})

	var vm struct {
		Data vmView `json:"data"`
	}
	e.mustCall(t, http.MethodPost, "/projects/"+project+"/vms", map[string]any{
		"name": "cache-" + suffix, "imageId": imageID, "flavorId": "small",
		"cloudInit": map[string]string{"username": "cirros", "password": "gocubsgo"},
	}, http.StatusCreated, &vm)
	vmID := vm.Data.ID
	t.Cleanup(func() { e.deleteVM(t, project, vmID) })

	dv, err := e.get(t, gvrDV, vmNamespace, vmID+"-rootdisk")
	if err != nil {
		t.Fatalf("root disk: %v", err)
	}
	if name, _, _ := unstructured.NestedString(dv.Object, "spec", "source", "pvc", "name"); name != cacheVolume {
		source, _, _ := unstructured.NestedMap(dv.Object, "spec", "source")
		t.Fatalf("the root disk must be cloned from %s, source is %v", cacheVolume, source)
	}
	e.waitRunning(t, project, vmID)

	// The guest boots and sees the flavor's disk size (2GiB), not the smaller cache.
	eventually(t, 4*time.Minute, "the guest to boot", func() (bool, string) {
		var resp struct {
			Data struct {
				Log string `json:"log"`
			} `json:"data"`
		}
		e.call(t, http.MethodGet, "/projects/"+project+"/vms/"+vmID+"/console/log", nil, &resp)
		return strings.Contains(resp.Data.Log, "login as 'cirros'"), fmt.Sprintf("%d bytes", len(resp.Data.Log))
	})
	var resp struct {
		Data struct {
			Log string `json:"log"`
		} `json:"data"`
	}
	e.mustCall(t, http.MethodGet, "/projects/"+project+"/vms/"+vmID+"/console/log", nil, http.StatusOK, &resp)
	// virtio_blk prints e.g. "[vda] 3956736 512-byte logical blocks (2.03 GB/1.89 GiB)".
	var gib float64
	for _, line := range strings.Split(resp.Data.Log, "\n") {
		if i := strings.Index(line, "GB/"); strings.Contains(line, "[vda]") && i >= 0 {
			fmt.Sscanf(line[i+3:], "%f", &gib)
		}
	}
	if gib < 1.8 {
		t.Errorf("the guest disk should be the flavor's 2GiB, not the cache's size; boot log reports %.2f GiB", gib)
	}
}
