package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

// registerFlavorRoutes adds flavor management under /api/v1/admin. Like the other admin routes they are
// unauthenticated until accounts and roles exist; then only the admin role may call them.
func registerFlavorRoutes(router chi.Router, store Store) {
	router.Get("/api/v1/admin/flavors", func(w http.ResponseWriter, r *http.Request) {
		flavors, err := store.ListAllFlavors(r.Context())
		if err != nil {
			log.Printf("list flavors: %v", err)
			writeError(w, http.StatusInternalServerError, "FLAVORS_UNAVAILABLE", "failed to list flavors")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": flavors})
	})

	router.Post("/api/v1/admin/flavors", func(w http.ResponseWriter, r *http.Request) {
		var input domain.FlavorInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if strings.TrimSpace(input.Name) == "" || input.CPU == 0 || input.MemoryBytes == 0 || input.RootDiskBytes == 0 {
			writeError(w, http.StatusBadRequest, "INVALID_FLAVOR_REQUEST", "name, cpu, memoryBytes and rootDiskBytes are required")
			return
		}
		flavor, err := store.CreateFlavor(r.Context(), input)
		if err != nil {
			writeFlavorError(w, "create flavor "+input.Name, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": flavor})
	})

	router.Patch("/api/v1/admin/flavors/{flavorID}", func(w http.ResponseWriter, r *http.Request) {
		var update domain.FlavorUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		flavor, err := store.UpdateFlavor(r.Context(), chi.URLParam(r, "flavorID"), update)
		if err != nil {
			writeFlavorError(w, "update flavor "+chi.URLParam(r, "flavorID"), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": flavor})
	})

	router.Delete("/api/v1/admin/flavors/{flavorID}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.DeleteFlavor(r.Context(), chi.URLParam(r, "flavorID")); err != nil {
			writeFlavorError(w, "delete flavor "+chi.URLParam(r, "flavorID"), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeFlavorError(w http.ResponseWriter, action string, err error) {
	switch {
	case errors.Is(err, storepkg.ErrFlavorNotFound):
		writeError(w, http.StatusNotFound, "FLAVOR_NOT_FOUND", "flavor was not found")
	case errors.Is(err, storepkg.ErrFlavorInvalid):
		writeError(w, http.StatusBadRequest, "FLAVOR_INVALID", err.Error())
	case errors.Is(err, storepkg.ErrFlavorNameTaken):
		writeError(w, http.StatusConflict, "FLAVOR_NAME_TAKEN", "a flavor with this name already exists")
	case errors.Is(err, storepkg.ErrFlavorInUse):
		writeError(w, http.StatusConflict, "FLAVOR_IN_USE", "virtual machines use this flavor; disable it instead of resizing or deleting it")
	default:
		log.Printf("%s: %v", action, err)
		writeError(w, http.StatusInternalServerError, "FLAVOR_UPDATE_FAILED", "failed to change the flavor")
	}
}
