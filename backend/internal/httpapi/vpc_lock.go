package httpapi

import (
	"context"
	"sync"
)

// vpcLocks serializes subnet changes within one VPC. Creating or deleting a public subnet reads the VPC's
// public subnets and rewrites the VPC's egress routes from that list; two such requests running at once
// could each write a list missing the other's subnet, and the last writer would drop that subnet's route
// to the NAT gateway. Holding the lock from before the subnet is saved also makes save order equal
// provisioning order, so the NAT gateway's anchor (the earliest public subnet) is always provisioned first.
// The API runs as a single replica, so an in-process lock is enough.
type vpcLocks struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

// lock waits for the VPC's lock and returns the function that releases it. It gives up when ctx ends, so a
// request whose client has gone does not queue behind a slow one.
func (v *vpcLocks) lock(ctx context.Context, networkID string) (func(), error) {
	v.mu.Lock()
	if v.locks == nil {
		v.locks = map[string]chan struct{}{}
	}
	sem, ok := v.locks[networkID]
	if !ok {
		sem = make(chan struct{}, 1)
		v.locks[networkID] = sem
	}
	v.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
