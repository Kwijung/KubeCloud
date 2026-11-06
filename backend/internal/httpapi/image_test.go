package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

func (s *fakeCatalogStore) CreateImage(_ context.Context, projectID string, input domain.CreateImageInput) (domain.Image, error) {
	if s.createImageErr != nil {
		return domain.Image{}, s.createImageErr
	}
	image := domain.Image{ID: projectID + "-" + input.Name, ProjectID: projectID, Name: input.Name, OSType: input.OSType, SourceURL: input.SourceURL, Status: "Ready", VirtualSizeBytes: input.VirtualSizeBytes}
	s.images = append(s.images, image)
	return image, nil
}

func (s *fakeCatalogStore) DeleteImage(_ context.Context, projectID string, imageID string) error {
	if s.deleteImageErr != nil {
		return s.deleteImageErr
	}
	for i, image := range s.images {
		if image.ProjectID == projectID && image.ID == imageID {
			s.images = append(s.images[:i], s.images[i+1:]...)
			return nil
		}
	}
	return storepkg.ErrImageNotFound
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code
}

func TestCreateImageReturnsReadyImage(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithProvisioner("test", store, &recordingProvisioner{})
	body := bytes.NewBufferString(`{"name":"ubuntu-24","osType":"linux","sourceUrl":"https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/images", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data domain.Image `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Name != "ubuntu-24" || resp.Data.Status != "Ready" || resp.Data.ProjectID != "platform" {
		t.Fatalf("unexpected image: %#v", resp.Data)
	}
}

func TestCreateImageRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		storeErr error
		status   int
		code     string
	}{
		{"missing fields", `{"name":"","osType":"linux","sourceUrl":""}`, nil, http.StatusBadRequest, "INVALID_IMAGE_REQUEST"},
		{"bad url", `{"name":"x","osType":"linux","sourceUrl":"ftp://example.com/x.img"}`, nil, http.StatusBadRequest, "IMAGE_SOURCE_URL_INVALID"},
		{"bad os type", `{"name":"x","osType":"plan9","sourceUrl":"https://example.com/x.img"}`, nil, http.StatusBadRequest, "IMAGE_OS_TYPE_INVALID"},
		{"name taken", `{"name":"x","osType":"linux","sourceUrl":"https://example.com/x.img"}`, storepkg.ErrImageNameTaken, http.StatusConflict, "IMAGE_NAME_TAKEN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouterWithProvisioner("test", &fakeCatalogStore{createImageErr: tc.storeErr}, &recordingProvisioner{})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/images", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
			if code := errorCode(t, rec); code != tc.code {
				t.Fatalf("expected %s, got %q", tc.code, code)
			}
		})
	}
}

func TestDeleteImage(t *testing.T) {
	cases := []struct {
		name     string
		imageID  string
		storeErr error
		status   int
		code     string
	}{
		{"deletes", "platform-cirros", nil, http.StatusNoContent, ""},
		{"not found", "platform-nope", nil, http.StatusNotFound, "IMAGE_NOT_FOUND"},
		{"in use", "platform-cirros", storepkg.ErrImageInUse, http.StatusConflict, "IMAGE_IN_USE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCatalogStore{
				images:         []domain.Image{{ID: "platform-cirros", ProjectID: "platform", Name: "cirros", Status: "Ready"}},
				deleteImageErr: tc.storeErr,
			}
			router := NewRouterWithProvisioner("test", store, &recordingProvisioner{})
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/platform/images/"+tc.imageID, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
			if tc.code != "" {
				if code := errorCode(t, rec); code != tc.code {
					t.Fatalf("expected %s, got %q", tc.code, code)
				}
			}
		})
	}
}

type fakeImageProber struct {
	size int64
	err  error
}

func (p fakeImageProber) VirtualSize(context.Context, string) (int64, error) {
	return p.size, p.err
}

func TestCreateImageStoresProbedVirtualSize(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, ImageProber: fakeImageProber{size: 3758096384}})
	body := bytes.NewBufferString(`{"name":"noble","osType":"linux","sourceUrl":"https://example.com/noble.img"}`)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/images", body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.images) != 1 || store.images[0].VirtualSizeBytes == nil || *store.images[0].VirtualSizeBytes != 3758096384 {
		t.Fatalf("expected the probed size to be stored, got %#v", store.images)
	}
}

func TestCreateImageSucceedsWhenProbeFails(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, ImageProber: fakeImageProber{err: errors.New("not qcow2")}})
	body := bytes.NewBufferString(`{"name":"raw","osType":"linux","sourceUrl":"https://example.com/raw.img"}`)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/images", body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.images[0].VirtualSizeBytes != nil {
		t.Fatalf("expected no size for an unprobed image, got %d", *store.images[0].VirtualSizeBytes)
	}
}

func (s *fakeCatalogStore) ListPublicImages(context.Context) ([]domain.Image, error) {
	var images []domain.Image
	for _, image := range s.images {
		if image.Scope == "public" {
			images = append(images, image)
		}
	}
	return images, nil
}

func (s *fakeCatalogStore) CreatePublicImage(_ context.Context, input domain.CreateImageInput) (domain.Image, error) {
	s.lastImageInput = input
	if s.createImageErr != nil {
		return domain.Image{}, s.createImageErr
	}
	image := domain.Image{ID: "public-" + input.Name, Name: input.Name, OSType: input.OSType, SourceURL: input.SourceURL, Status: "Ready", Scope: "public", VirtualSizeBytes: input.VirtualSizeBytes}
	s.images = append(s.images, image)
	return image, nil
}

func (s *fakeCatalogStore) DeletePublicImage(_ context.Context, imageID string) error {
	if s.deleteImageErr != nil {
		return s.deleteImageErr
	}
	for i, image := range s.images {
		if image.Scope == "public" && image.ID == imageID {
			s.images = append(s.images[:i], s.images[i+1:]...)
			return nil
		}
	}
	return storepkg.ErrImageNotFound
}

func TestAdminPublicImageRoutes(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, ImageProber: fakeImageProber{size: 2147483648}})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/images", bytes.NewBufferString(`{"name":"noble","osType":"linux","sourceUrl":"https://example.com/noble.img"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.images) != 1 || store.images[0].Scope != "public" || store.images[0].VirtualSizeBytes == nil {
		t.Fatalf("expected a probed public image, got %#v", store.images)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/images", bytes.NewBufferString(`{"name":"bad","osType":"linux","sourceUrl":"ftp://x/y"}`)))
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "IMAGE_SOURCE_URL_INVALID" {
		t.Fatalf("expected 400 IMAGE_SOURCE_URL_INVALID, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/images", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	var list struct {
		Data []domain.Image `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Fatalf("expected one public image, got %s (%v)", rec.Body.String(), err)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/images/public-noble", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/images/public-noble", nil))
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "IMAGE_NOT_FOUND" {
		t.Fatalf("expected 404 IMAGE_NOT_FOUND, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDeletePublicImageInUse(t *testing.T) {
	store := &fakeCatalogStore{deleteImageErr: storepkg.ErrImageInUse}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/images/public-noble", nil))
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "IMAGE_IN_USE" {
		t.Fatalf("expected 409 IMAGE_IN_USE, got %d: %s", rec.Code, rec.Body.String())
	}
}

type recordingImageCache struct{ deleted []string }

func (c *recordingImageCache) Delete(_ context.Context, imageID string) error {
	c.deleted = append(c.deleted, imageID)
	return nil
}

func TestImageCacheIsRequestedAndCleanedUp(t *testing.T) {
	store := &fakeCatalogStore{}
	cache := &recordingImageCache{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, ImageCache: cache, ImageProber: fakeImageProber{size: 1 << 30}})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/images", bytes.NewBufferString(`{"name":"noble","sourceUrl":"https://example.com/noble.img"}`)))
	if rec.Code != http.StatusCreated || !store.lastImageInput.Cache {
		t.Fatalf("expected a cached registration, got %d %#v", rec.Code, store.lastImageInput)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/images/public-noble", nil))
	if rec.Code != http.StatusNoContent || len(cache.deleted) != 1 || cache.deleted[0] != "public-noble" {
		t.Fatalf("deleting the image must delete its cache, got %d %#v", rec.Code, cache.deleted)
	}
	// A failed delete leaves the cache alone.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/images/public-noble", nil))
	if rec.Code != http.StatusNotFound || len(cache.deleted) != 1 {
		t.Fatalf("expected 404 and no cache delete, got %d %#v", rec.Code, cache.deleted)
	}
}
