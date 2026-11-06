package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

// createImage handles an image registration request for either scope: it validates the body, reads the
// image's virtual size when a prober is configured, and stores the image through create.
func createImage(w http.ResponseWriter, r *http.Request, prober ImageProber, create func(context.Context, domain.CreateImageInput) (domain.Image, error)) {
	var request struct {
		Name      string `json:"name"`
		OSType    string `json:"osType"`
		SourceURL string `json:"sourceUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
		return
	}
	input := domain.CreateImageInput{Name: request.Name, OSType: request.OSType, SourceURL: request.SourceURL}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.SourceURL) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_IMAGE_REQUEST", "name and sourceUrl are required")
		return
	}
	if input.OSType == "" {
		input.OSType = "linux"
	}
	input, err := storepkg.ValidateImageInput(input)
	switch {
	case errors.Is(err, storepkg.ErrImageSourceURLInvalid):
		writeError(w, http.StatusBadRequest, "IMAGE_SOURCE_URL_INVALID", "sourceUrl must be an http or https url")
		return
	case errors.Is(err, storepkg.ErrImageOSTypeInvalid):
		writeError(w, http.StatusBadRequest, "IMAGE_OS_TYPE_INVALID", "osType must be linux or windows")
		return
	}
	if prober != nil {
		// A failed probe still registers the image; VM creation then skips the disk size check.
		if size, err := prober.VirtualSize(r.Context(), input.SourceURL); err == nil {
			input.VirtualSizeBytes = &size
		} else {
			log.Printf("image %q: virtual size unknown: %v", input.Name, err)
		}
	}
	image, err := create(r.Context(), input)
	if err != nil {
		if errors.Is(err, storepkg.ErrImageNameTaken) {
			writeError(w, http.StatusConflict, "IMAGE_NAME_TAKEN", "an image with this name already exists")
			return
		}
		log.Printf("create image %q: %v", input.Name, err)
		writeError(w, http.StatusInternalServerError, "IMAGE_CREATE_FAILED", "failed to create image")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": image})
}

// writeImageDeleteResult maps the result of deleting an image of either scope to a response.
func writeImageDeleteResult(w http.ResponseWriter, imageID string, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, storepkg.ErrImageNotFound):
		writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image was not found")
	case errors.Is(err, storepkg.ErrImageInUse):
		writeError(w, http.StatusConflict, "IMAGE_IN_USE", "delete the virtual machines using this image first")
	default:
		log.Printf("delete image %s: %v", imageID, err)
		writeError(w, http.StatusInternalServerError, "IMAGE_DELETE_FAILED", "failed to delete image")
	}
}
