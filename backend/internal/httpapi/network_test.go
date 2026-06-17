package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestCreateNetworkPersistsAndProvisions(t *testing.T) {
	store := &fakeCatalogStore{}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})
	body := bytes.NewBufferString(`{"name":"app"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(np.createdNetworks) != 1 {
		t.Fatalf("expected provisioner CreateNetwork once, got %d", len(np.createdNetworks))
	}
}

func TestListNetworksFiltersByProject(t *testing.T) {
	store := &fakeCatalogStore{networks: []domain.Network{
		{ID: "platform-app", ProjectID: "platform", Name: "app", Status: "Ready"},
		{ID: "data-lake", ProjectID: "data", Name: "lake", Status: "Ready"},
	}}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/networks", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	var body struct {
		Data []domain.Network `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "platform-app" {
		t.Fatalf("unexpected networks: %#v", body.Data)
	}
}

type recordingNetworkProvisioner struct {
	createdNetworks []domain.Network
	deletedNetworks []domain.Network
	createdSubnets  []domain.Subnet
	deletedSubnets  []domain.Subnet
}

func (p *recordingNetworkProvisioner) CreateNetwork(_ context.Context, n domain.Network) error {
	p.createdNetworks = append(p.createdNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteNetwork(_ context.Context, n domain.Network) error {
	p.deletedNetworks = append(p.deletedNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) CreateSubnet(_ context.Context, _ string, s domain.Subnet) error {
	p.createdSubnets = append(p.createdSubnets, s)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteSubnet(_ context.Context, _ string, s domain.Subnet) error {
	p.deletedSubnets = append(p.deletedSubnets, s)
	return nil
}

func TestCreateSubnetValidatesTypeAndProvisions(t *testing.T) {
	store := &fakeCatalogStore{networks: []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}}}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	ok := bytes.NewBufferString(`{"name":"web","cidr":"10.0.0.0/24","type":"private"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", ok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(np.createdSubnets) != 1 {
		t.Fatalf("expected provisioner CreateSubnet once, got %d", len(np.createdSubnets))
	}

	bad := bytes.NewBufferString(`{"name":"web2","cidr":"10.0.1.0/24","type":"weird"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", bad)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid type, got %d", rec2.Code)
	}
}
