package httpapi

import "sync"

// vpcLocks serializes subnet provisioning within one VPC. Creating or deleting a public subnet reads the
// VPC's public subnets and rewrites the VPC's egress routes from that list; two such requests running at
// once could each write a list missing the other's subnet, and the last writer would drop that subnet's
// route to the NAT gateway. The API runs as a single replica, so an in-process lock is enough.
type vpcLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// lock blocks until the VPC's lock is held and returns the function that releases it.
func (v *vpcLocks) lock(networkID string) func() {
	v.mu.Lock()
	if v.locks == nil {
		v.locks = map[string]*sync.Mutex{}
	}
	m, ok := v.locks[networkID]
	if !ok {
		m = &sync.Mutex{}
		v.locks[networkID] = m
	}
	v.mu.Unlock()
	m.Lock()
	return m.Unlock
}
