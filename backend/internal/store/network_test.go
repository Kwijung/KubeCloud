package store

import (
	"errors"
	"testing"
)

func TestNormalizeCIDRRejectsInvalid(t *testing.T) {
	if _, err := normalizeCIDR("not-a-cidr"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
	if _, err := normalizeCIDR("10.0.0.0/24"); err != nil {
		t.Fatalf("expected 10.0.0.0/24 to be valid, got %v", err)
	}
	// Host-bit masking: host bits must be zeroed to the network address.
	if got, err := normalizeCIDR("10.0.5.7/24"); err != nil {
		t.Fatalf("expected 10.0.5.7/24 to be valid, got %v", err)
	} else if got != "10.0.5.0/24" {
		t.Fatalf("expected host bits masked to 10.0.5.0/24, got %q", got)
	}
	// IPv6 rejection: only IPv4 CIDRs are accepted.
	if _, err := normalizeCIDR("2001:db8::/32"); err == nil {
		t.Fatal("expected error for IPv6 CIDR")
	}
}

func TestDefaultGatewayIsFirstHost(t *testing.T) {
	gw, err := defaultGateway("10.0.5.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gw != "10.0.5.1" {
		t.Fatalf("expected 10.0.5.1, got %q", gw)
	}
}

func TestCIDRsOverlapDetectsContainment(t *testing.T) {
	if !cidrsOverlap("10.0.0.0/16", "10.0.1.0/24") {
		t.Fatal("expected 10.0.0.0/16 and 10.0.1.0/24 to overlap")
	}
	if cidrsOverlap("10.0.0.0/24", "10.0.1.0/24") {
		t.Fatal("expected disjoint /24s not to overlap")
	}
}

func TestIsUsableHostGateway(t *testing.T) {
	cases := []struct {
		cidr, host string
		want       bool
		desc       string
	}{
		{"10.0.0.0/24", "10.0.0.1", true, "first usable host should be valid"},
		{"10.0.0.0/24", "10.0.0.0", false, "network address must be rejected"},
		{"10.0.0.0/24", "10.0.0.255", false, "broadcast address must be rejected"},
		{"10.0.0.0/24", "10.0.1.5", false, "address outside CIDR must be rejected"},
	}
	for _, tc := range cases {
		got := isUsableHostGateway(tc.cidr, tc.host)
		if got != tc.want {
			t.Errorf("isUsableHostGateway(%q, %q) = %v, want %v — %s",
				tc.cidr, tc.host, got, tc.want, tc.desc)
		}
	}
}

func TestNormalizeNetworkCIDRAcceptsPrivateRanges(t *testing.T) {
	cases := map[string]string{
		"10.10.0.0/16":    "10.10.0.0/16",
		"10.10.5.7/16":    "10.10.0.0/16",
		"172.20.0.0/20":   "172.20.0.0/20",
		"192.168.10.0/28": "192.168.10.0/28",
	}
	for in, want := range cases {
		got, err := NormalizeNetworkCIDR(in)
		if err != nil || got != want {
			t.Fatalf("%s: expected %s, got %q (err %v)", in, want, got, err)
		}
	}
}

func TestNormalizeNetworkCIDRRejectsPublicOrOutOfRange(t *testing.T) {
	for _, in := range []string{
		"8.8.8.0/24",     // public
		"172.32.0.0/16",  // just outside 172.16.0.0/12
		"10.0.0.0/8",     // larger than /16
		"10.0.0.0/15",    // larger than /16
		"10.0.0.0/29",    // smaller than /28
		"192.168.0.0/15", // spans beyond 192.168.0.0/16
		"bogus",
		"fd00::/48",
	} {
		if _, err := NormalizeNetworkCIDR(in); !errors.Is(err, ErrNetworkCIDRInvalid) {
			t.Fatalf("%s: expected ErrNetworkCIDRInvalid, got %v", in, err)
		}
	}
}

func TestCIDRContains(t *testing.T) {
	if !cidrContains("10.10.0.0/16", "10.10.3.0/24") {
		t.Fatal("expected /24 inside its /16")
	}
	if !cidrContains("10.10.0.0/16", "10.10.0.0/16") {
		t.Fatal("expected a cidr to contain itself")
	}
	if cidrContains("10.10.0.0/16", "10.11.0.0/24") {
		t.Fatal("expected sibling range to be outside")
	}
	if cidrContains("10.10.0.0/16", "10.0.0.0/8") {
		t.Fatal("expected a larger range to be outside")
	}
}
