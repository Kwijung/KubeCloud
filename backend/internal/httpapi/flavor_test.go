package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

func (s *fakeCatalogStore) ListAllFlavors(context.Context) ([]domain.Flavor, error) {
	return s.flavors, nil
}

func (s *fakeCatalogStore) CreateFlavor(_ context.Context, input domain.FlavorInput) (domain.Flavor, error) {
	if s.flavorErr != nil {
		return domain.Flavor{}, s.flavorErr
	}
	f := domain.Flavor{ID: input.Name, Name: input.Name, CPU: input.CPU, MemoryBytes: input.MemoryBytes, RootDiskBytes: input.RootDiskBytes, Enabled: true}
	s.flavors = append(s.flavors, f)
	return f, nil
}

func (s *fakeCatalogStore) UpdateFlavor(_ context.Context, id string, update domain.FlavorUpdate) (domain.Flavor, error) {
	if s.flavorErr != nil {
		return domain.Flavor{}, s.flavorErr
	}
	for i := range s.flavors {
		if s.flavors[i].ID == id {
			if update.Name != nil {
				s.flavors[i].Name = *update.Name
			}
			if update.Enabled != nil {
				s.flavors[i].Enabled = *update.Enabled
			}
			return s.flavors[i], nil
		}
	}
	return domain.Flavor{}, storepkg.ErrFlavorNotFound
}

func (s *fakeCatalogStore) DeleteFlavor(_ context.Context, id string) error {
	if s.flavorErr != nil {
		return s.flavorErr
	}
	for i := range s.flavors {
		if s.flavors[i].ID == id {
			s.flavors = append(s.flavors[:i], s.flavors[i+1:]...)
			return nil
		}
	}
	return storepkg.ErrFlavorNotFound
}

func TestAdminFlavorRoutes(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return rec
	}

	if rec := do(http.MethodPost, "/api/v1/admin/flavors", `{"name":"xlarge","cpu":8,"memoryBytes":8589934592,"rootDiskBytes":42949672960}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/v1/admin/flavors", `{"name":"","cpu":8}`); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "INVALID_FLAVOR_REQUEST" {
		t.Fatalf("missing fields: expected 400 INVALID_FLAVOR_REQUEST, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPatch, "/api/v1/admin/flavors/xlarge", `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.flavors[0].Enabled {
		t.Fatal("the flavor should be disabled")
	}
	if rec := do(http.MethodGet, "/api/v1/admin/flavors", ""); rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/v1/admin/flavors/xlarge", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodDelete, "/api/v1/admin/flavors/xlarge", ""); rec.Code != http.StatusNotFound || errorCode(t, rec) != "FLAVOR_NOT_FOUND" {
		t.Fatalf("expected 404 FLAVOR_NOT_FOUND, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminFlavorErrors(t *testing.T) {
	cases := []struct {
		err    error
		method string
		status int
		code   string
	}{
		{storepkg.ErrFlavorInvalid, http.MethodPost, http.StatusBadRequest, "FLAVOR_INVALID"},
		{storepkg.ErrFlavorNameTaken, http.MethodPost, http.StatusConflict, "FLAVOR_NAME_TAKEN"},
		{storepkg.ErrFlavorInUse, http.MethodPatch, http.StatusConflict, "FLAVOR_IN_USE"},
		{storepkg.ErrFlavorInUse, http.MethodDelete, http.StatusConflict, "FLAVOR_IN_USE"},
	}
	for _, tc := range cases {
		router := NewRouterWithDependencies(Dependencies{Version: "test", Store: &fakeCatalogStore{flavorErr: tc.err}})
		path, body := "/api/v1/admin/flavors/small", `{"cpu":4}`
		if tc.method == http.MethodPost {
			path, body = "/api/v1/admin/flavors", `{"name":"x","cpu":1,"memoryBytes":1073741824,"rootDiskBytes":1073741824}`
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(tc.method, path, bytes.NewBufferString(body)))
		if rec.Code != tc.status || errorCode(t, rec) != tc.code {
			t.Errorf("%s with %v: expected %d %s, got %d: %s", tc.method, tc.err, tc.status, tc.code, rec.Code, rec.Body.String())
		}
	}
}
