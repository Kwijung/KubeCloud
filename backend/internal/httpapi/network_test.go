package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
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
	createErr       error
}

func (p *recordingNetworkProvisioner) CreateNetwork(_ context.Context, n domain.Network) error {
	if p.createErr != nil {
		return p.createErr
	}
	p.createdNetworks = append(p.createdNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteNetwork(_ context.Context, n domain.Network) error {
	p.deletedNetworks = append(p.deletedNetworks, n)
	return nil
}
func (p *recordingNetworkProvisioner) CreateSubnet(_ context.Context, _ string, s domain.Subnet) error {
	if p.createErr != nil {
		return p.createErr
	}
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

func TestDeleteSubnetWrongNetworkReturns404(t *testing.T) {
	store := &fakeCatalogStore{
		networks: []domain.Network{
			{ID: "net-a", ProjectID: "platform", Name: "net-a", OVNVPCName: "kloudia-vpc-platform-net-a", Status: "Ready"},
			{ID: "net-b", ProjectID: "platform", Name: "net-b", OVNVPCName: "kloudia-vpc-platform-net-b", Status: "Ready"},
		},
		subnets: []domain.Subnet{
			{ID: "subnet-1", NetworkID: "net-a", ProjectID: "platform", Name: "web", CIDR: "10.0.0.0/24", Status: "Ready"},
		},
	}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/platform/networks/net-b/subnets/subnet-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "SUBNET_NOT_FOUND" {
		t.Fatalf("expected SUBNET_NOT_FOUND, got %q", body.Error.Code)
	}
	if len(store.subnets) != 1 {
		t.Fatalf("expected subnet to remain in store, got %d subnets", len(store.subnets))
	}
	if len(np.deletedSubnets) != 0 {
		t.Fatalf("expected provisioner DeleteSubnet not to be called, got %d calls", len(np.deletedSubnets))
	}
}

func TestCreateSubnetCIDROverlapReturns409(t *testing.T) {
	store := &fakeCatalogStore{
		networks:        []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}},
		createSubnetErr: storepkg.ErrSubnetCIDROverlap,
	}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	body := bytes.NewBufferString(`{"name":"web","cidr":"10.0.0.0/24","type":"private"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var respBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if respBody.Error.Code != "SUBNET_CIDR_OVERLAP" {
		t.Fatalf("expected SUBNET_CIDR_OVERLAP, got %q", respBody.Error.Code)
	}
}

func TestCreateNetworkRollsBackOnProvisionFailure(t *testing.T) {
	store := &fakeCatalogStore{}
	np := &recordingNetworkProvisioner{createErr: errors.New("provisioner down")}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})
	body := bytes.NewBufferString(`{"name":"app"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.networks) != 0 {
		t.Fatalf("expected network record rolled back, got %d networks", len(store.networks))
	}
}

func TestCreateSubnetRollsBackOnProvisionFailure(t *testing.T) {
	store := &fakeCatalogStore{
		networks: []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}},
	}
	np := &recordingNetworkProvisioner{createErr: errors.New("provisioner down")}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})
	body := bytes.NewBufferString(`{"name":"web","cidr":"10.0.0.0/24","type":"private"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.subnets) != 0 {
		t.Fatalf("expected subnet record rolled back, got %d subnets", len(store.subnets))
	}
}
