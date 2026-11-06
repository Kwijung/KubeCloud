// Package imageprobe reads a cloud image's virtual disk size from its URL without downloading it.
package imageprobe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrUnknownFormat is returned when the image is not qcow2, so its virtual size cannot be read from a header.
// Raw and compressed images fall here.
var ErrUnknownFormat = errors.New("image is not qcow2")

// qcow2 header: magic "QFI\xfb" at offset 0, big-endian virtual size in bytes at offset 24.
const qcow2HeaderLen = 32

var qcow2Magic = []byte{'Q', 'F', 'I', 0xfb}

type Prober struct {
	client *http.Client
}

func New() *Prober {
	return &Prober{client: &http.Client{Timeout: 15 * time.Second}}
}

// VirtualSize fetches the first bytes of the image with a range request and returns the qcow2 virtual size.
func (p *Prober) VirtualSize(ctx context.Context, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", qcow2HeaderLen-1))
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("fetch image header: %s", resp.Status)
	}
	// A server that ignores Range sends the whole file; only the header is read before closing.
	header := make([]byte, qcow2HeaderLen)
	if _, err := io.ReadFull(resp.Body, header); err != nil {
		return 0, ErrUnknownFormat
	}
	if string(header[:4]) != string(qcow2Magic) {
		return 0, ErrUnknownFormat
	}
	size := binary.BigEndian.Uint64(header[24:32])
	if size == 0 || size > 1<<50 {
		return 0, ErrUnknownFormat
	}
	return int64(size), nil
}
