package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

// syncedSubnetStore makes the fake store's subnet records safe for concurrent requests.
type syncedSubnetStore struct {
	*fakeCatalogStore
	mu sync.Mutex
}

func (s *syncedSubnetStore) CreateSubnet(ctx context.Context, projectID, networkID, name, cidr, subnetType, gateway string) (domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeCatalogStore.CreateSubnet(ctx, projectID, networkID, name, cidr, subnetType, gateway)
}

func (s *syncedSubnetStore) ListSubnets(ctx context.Context, projectID, networkID string) ([]domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeCatalogStore.ListSubnets(ctx, projectID, networkID)
}

func (s *syncedSubnetStore) publicCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sn := range s.subnets {
		if sn.Type == "public" {
			n++
		}
	}
	return n
}

// stallingNetworkProvisioner holds its first CreateSubnet call until a second public subnet exists in the
// store, which is the window where an unserialized second request could provision and finish first.
type stallingNetworkProvisioner struct {
	recordingNetworkProvisioner
	store    *syncedSubnetStore
	calls    atomic.Int32
	mu       sync.Mutex
	finished [][]domain.Subnet // publicSubnets of each CreateSubnet call, in the order the calls finished
}

func (p *stallingNetworkProvisioner) CreateSubnet(_ context.Context, _ string, _ domain.Subnet, publicSubnets []domain.Subnet) error {
	if p.calls.Add(1) == 1 {
		deadline := time.Now().Add(300 * time.Millisecond)
		for p.store.publicCount() < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finished = append(p.finished, publicSubnets)
	return nil
}

func TestConcurrentPublicSubnetsKeepEveryEgressRoute(t *testing.T) {
	store := &syncedSubnetStore{fakeCatalogStore: &fakeCatalogStore{
		networks: []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", CIDR: "10.30.0.0/16", OVNVPCName: "kloudia-vpc-platform-app"}},
	}}
	provisioner := &stallingNetworkProvisioner{store: store}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: provisioner})

	var wg sync.WaitGroup
	for i, body := range []string{
		`{"name":"pub-a","cidr":"10.30.1.0/24","type":"public"}`,
		`{"name":"pub-b","cidr":"10.30.2.0/24","type":"public"}`,
	} {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", bytes.NewBufferString(body)))
			if rec.Code != http.StatusCreated {
				t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
			}
		}(body)
		if i == 0 {
			time.Sleep(30 * time.Millisecond) // let the first request reach the provisioner
		}
	}
	wg.Wait()

	if len(provisioner.finished) != 2 {
		t.Fatalf("expected two provisioning calls, got %d", len(provisioner.finished))
	}
	// The VPC's egress routes are whatever the last call wrote, so it must cover both public subnets.
	if last := provisioner.finished[1]; len(last) != 2 {
		t.Fatalf("the last provisioning call routed %d public subnet(s), want 2: %#v", len(last), subnetIDs(last))
	}
}
