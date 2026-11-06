package domain

import "testing"

func TestMinRootDiskBytesAddsFilesystemOverhead(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		virtual int64
		fits    int64
		tooBig  int64
	}{
		{virtual: 3758096384, fits: 10 * gib, tooBig: 2 * gib}, // Ubuntu 24.04: medium yes, small no
		{virtual: 10 * gib, fits: 20 * gib, tooBig: 10 * gib},  // Rocky 9: a 10GiB disk loses 5.5% to overhead
	}
	for _, tc := range cases {
		min := MinRootDiskBytes(tc.virtual)
		if min > tc.fits || min <= tc.tooBig {
			t.Fatalf("virtual %d: min %d should fit %d and exceed %d", tc.virtual, min, tc.fits, tc.tooBig)
		}
	}
}
