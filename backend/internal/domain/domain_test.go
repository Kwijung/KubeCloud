package domain

import (
	"testing"
	"time"
)

func TestNATGatewayAnchorPicksEarliestPublicSubnet(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	subnets := []Subnet{
		{ID: "c", Type: "public", CreatedAt: t0.Add(2 * time.Minute)},
		{ID: "a", Type: "private", CreatedAt: t0},
		{ID: "b", Type: "public", CreatedAt: t0.Add(time.Minute)},
		{ID: "d", Type: "public", CreatedAt: t0.Add(time.Minute)},
	}
	anchor, ok := NATGatewayAnchor(subnets)
	if !ok || anchor.ID != "b" {
		t.Fatalf("expected anchor b, got %q (ok=%v)", anchor.ID, ok)
	}
}

func TestNATGatewayAnchorNoPublicSubnet(t *testing.T) {
	if _, ok := NATGatewayAnchor([]Subnet{{ID: "a", Type: "private"}}); ok {
		t.Fatal("expected no anchor without public subnets")
	}
}
