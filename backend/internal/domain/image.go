package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// filesystemOverhead is the share of a filesystem-mode PVC that CDI keeps free (its default
// filesystemOverhead). An image only fits when its virtual size fits in the rest of the root disk.
const filesystemOverhead = 0.055

// MinRootDiskBytes returns the smallest root disk that can hold an image of the given virtual size.
func MinRootDiskBytes(virtualSizeBytes int64) int64 {
	return int64(float64(virtualSizeBytes)/(1-filesystemOverhead)) + 1
}

// ImageCacheVolumeName names the DataVolume that caches an image in the cluster. Kubernetes names are at most
// 63 characters, so a long image ID is shortened and made unique with a hash.
func ImageCacheVolumeName(imageID string) string {
	name := "image-" + imageID
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(imageID))
	return name[:54] + "-" + hex.EncodeToString(sum[:4])
}

// ImageCacheState is what the image cache reconciler records for an image.
type ImageCacheState struct {
	CacheVolume   string
	CacheStatus   string
	Status        string
	StatusMessage string
}
