package domain

// filesystemOverhead is the share of a filesystem-mode PVC that CDI keeps free (its default
// filesystemOverhead). An image only fits when its virtual size fits in the rest of the root disk.
const filesystemOverhead = 0.055

// MinRootDiskBytes returns the smallest root disk that can hold an image of the given virtual size.
func MinRootDiskBytes(virtualSizeBytes int64) int64 {
	return int64(float64(virtualSizeBytes)/(1-filesystemOverhead)) + 1
}
