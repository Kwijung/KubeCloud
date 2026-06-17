package store

import "testing"

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
