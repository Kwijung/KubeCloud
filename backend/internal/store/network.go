package store

import (
	"errors"
	"net/netip"
)

var (
	// ErrSubnetCIDRInvalid is returned when a subnet CIDR cannot be parsed.
	ErrSubnetCIDRInvalid = errors.New("subnet cidr is invalid")
	// ErrSubnetCIDROverlap is returned when a subnet CIDR overlaps another subnet in the same VPC.
	ErrSubnetCIDROverlap = errors.New("subnet cidr overlaps an existing subnet in the same vpc")
	// ErrSubnetInUse is returned when deleting a subnet that still has VMs attached.
	ErrSubnetInUse = errors.New("subnet still has virtual machines attached")
	// ErrNetworkInUse is returned when deleting a VPC that still has subnets.
	ErrNetworkInUse = errors.New("network still has subnets")
	// ErrNotFound is returned when a network or subnet does not exist.
	ErrNotFound = errors.New("not found")
)

// normalizeCIDR validates a CIDR and returns its canonical masked form (e.g. 10.0.5.7/24 -> 10.0.5.0/24).
func normalizeCIDR(cidr string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return "", ErrSubnetCIDRInvalid
	}
	return prefix.Masked().String(), nil
}

// defaultGateway returns the first usable host address of a CIDR (network address + 1).
func defaultGateway(cidr string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return "", ErrSubnetCIDRInvalid
	}
	return prefix.Masked().Addr().Next().String(), nil
}

// cidrsOverlap reports whether two CIDRs share any address. Invalid inputs are treated as non-overlapping.
func cidrsOverlap(a, b string) bool {
	pa, err := netip.ParsePrefix(a)
	if err != nil {
		return false
	}
	pb, err := netip.ParsePrefix(b)
	if err != nil {
		return false
	}
	return pa.Overlaps(pb)
}
