package imageprobe

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func qcow2Header(virtualSize uint64) []byte {
	header := make([]byte, 512)
	copy(header, []byte{'Q', 'F', 'I', 0xfb})
	binary.BigEndian.PutUint32(header[4:8], 3)
	binary.BigEndian.PutUint64(header[24:32], virtualSize)
	return header
}

func TestVirtualSizeReadsQCOW2Header(t *testing.T) {
	var gotRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(qcow2Header(3758096384)[:32])
	}))
	defer server.Close()

	size, err := New().VirtualSize(context.Background(), server.URL+"/noble.img")
	if err != nil {
		t.Fatalf("virtual size: %v", err)
	}
	if size != 3758096384 {
		t.Fatalf("expected 3758096384, got %d", size)
	}
	if gotRange != "bytes=0-31" {
		t.Fatalf("expected a range request for the header, got %q", gotRange)
	}
}

func TestVirtualSizeWorksWhenServerIgnoresRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(qcow2Header(2147483648))
	}))
	defer server.Close()

	size, err := New().VirtualSize(context.Background(), server.URL)
	if err != nil || size != 2147483648 {
		t.Fatalf("expected 2147483648, got %d (%v)", size, err)
	}
}

func TestVirtualSizeRejectsNonQCOW2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 64))
	}))
	defer server.Close()

	if _, err := New().VirtualSize(context.Background(), server.URL); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("expected ErrUnknownFormat, got %v", err)
	}
}

func TestVirtualSizeReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	if _, err := New().VirtualSize(context.Background(), server.URL); err == nil || errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("expected an HTTP error, got %v", err)
	}
}
