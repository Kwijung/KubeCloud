package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

func TestCreateNetworkPersistsAndProvisions(t *testing.T) {
	store := &fakeCatalogStore{}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})
	body := bytes.NewBufferString(`{"name":"app","cidr":"10.0.0.0/16"}`)
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
	// public subnet lists passed alongside createdSubnets / deletedSubnets
	createdPublic [][]domain.Subnet
	deletedPublic [][]domain.Subnet
	createErr     error
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
func (p *recordingNetworkProvisioner) CreateSubnet(_ context.Context, _ string, s domain.Subnet, publicSubnets []domain.Subnet) error {
	if p.createErr != nil {
		return p.createErr
	}
	p.createdSubnets = append(p.createdSubnets, s)
	p.createdPublic = append(p.createdPublic, publicSubnets)
	return nil
}
func (p *recordingNetworkProvisioner) DeleteSubnet(_ context.Context, _ string, s domain.Subnet, remainingPublicSubnets []domain.Subnet) error {
	p.deletedSubnets = append(p.deletedSubnets, s)
	p.deletedPublic = append(p.deletedPublic, remainingPublicSubnets)
	return nil
}

func subnetIDs(subnets []domain.Subnet) []string {
	ids := []string{}
	for _, s := range subnets {
		ids = append(ids, s.ID)
	}
	return ids
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
	body := bytes.NewBufferString(`{"name":"app","cidr":"10.0.0.0/16"}`)
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

func TestCreateSubnetPassesVPCPublicSubnets(t *testing.T) {
	store := &fakeCatalogStore{networks: []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}}}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	for _, body := range []string{
		`{"name":"a","cidr":"10.0.0.0/24","type":"public"}`,
		`{"name":"b","cidr":"10.0.1.0/24","type":"private"}`,
		`{"name":"c","cidr":"10.0.2.0/24","type":"public"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	want := [][]string{
		{"platform-app-a"},
		{},
		{"platform-app-a", "platform-app-c"},
	}
	for i, w := range want {
		got := subnetIDs(np.createdPublic[i])
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Fatalf("subnet %d: expected public subnets %v, got %v", i, w, got)
		}
	}
}

func TestDeleteSubnetPassesRemainingPublicSubnets(t *testing.T) {
	store := &fakeCatalogStore{
		networks: []domain.Network{{ID: "net-a", ProjectID: "platform", Name: "net-a", OVNVPCName: "kloudia-vpc-platform-net-a", Status: "Ready"}},
		subnets: []domain.Subnet{
			{ID: "subnet-1", NetworkID: "net-a", ProjectID: "platform", Name: "web", CIDR: "10.0.0.0/24", Type: "public", Status: "Ready"},
			{ID: "subnet-2", NetworkID: "net-a", ProjectID: "platform", Name: "db", CIDR: "10.0.1.0/24", Type: "private", Status: "Ready"},
			{ID: "subnet-3", NetworkID: "net-a", ProjectID: "platform", Name: "api", CIDR: "10.0.2.0/24", Type: "public", Status: "Ready"},
		},
	}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	for _, id := range []string{"subnet-3", "subnet-1"} {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/platform/networks/net-a/subnets/"+id, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete %s: expected 204, got %d: %s", id, rec.Code, rec.Body.String())
		}
	}

	if got := subnetIDs(np.deletedPublic[0]); strings.Join(got, ",") != "subnet-1" {
		t.Fatalf("expected subnet-1 to remain public after deleting subnet-3, got %v", got)
	}
	if got := np.deletedPublic[1]; len(got) != 0 {
		t.Fatalf("expected no public subnets left after deleting subnet-1, got %v", subnetIDs(got))
	}
}

func TestDeleteGatewayAnchorSubnetReturns409(t *testing.T) {
	store := &fakeCatalogStore{
		networks:        []domain.Network{{ID: "net-a", ProjectID: "platform", Name: "net-a", OVNVPCName: "kloudia-vpc-platform-net-a", Status: "Ready"}},
		subnets:         []domain.Subnet{{ID: "subnet-1", NetworkID: "net-a", ProjectID: "platform", Name: "web", CIDR: "10.0.0.0/24", Type: "public", Status: "Ready"}},
		deleteSubnetErr: storepkg.ErrSubnetHostsNATGateway,
	}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store, NetworkProvisioner: np})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/platform/networks/net-a/subnets/subnet-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "SUBNET_HOSTS_NAT_GATEWAY" {
		t.Fatalf("expected SUBNET_HOSTS_NAT_GATEWAY, got %q", body.Error.Code)
	}
	if len(np.deletedSubnets) != 0 {
		t.Fatalf("expected provisioner DeleteSubnet not to be called, got %d calls", len(np.deletedSubnets))
	}
}

func postNetwork(t *testing.T, router http.Handler, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp.Error.Code
}

func TestCreateNetworkRequiresCIDRAndStoresIt(t *testing.T) {
	store := &fakeCatalogStore{}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})

	if code, errCode := postNetwork(t, router, `{"name":"app"}`); code != http.StatusBadRequest || errCode != "INVALID_NETWORK_REQUEST" {
		t.Fatalf("expected 400 INVALID_NETWORK_REQUEST without cidr, got %d %s", code, errCode)
	}
	if code, _ := postNetwork(t, router, `{"name":"app","cidr":"10.20.0.0/16"}`); code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", code)
	}
	if code, _ := postNetwork(t, router, `{"name":"app2","cidr":"10.21.9.0/16"}`); code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", code)
	}
	if store.networks[0].CIDR != "10.20.0.0/16" || store.networks[1].CIDR != "10.21.0.0/16" {
		t.Fatalf("expected masked cidrs passed to store, got %q %q", store.networks[0].CIDR, store.networks[1].CIDR)
	}
}

func TestCreateNetworkRejectsReservedCIDR(t *testing.T) {
	store := &fakeCatalogStore{}
	np := &recordingNetworkProvisioner{}
	router := NewRouterWithDependencies(Dependencies{
		Version: "test", Store: store, NetworkProvisioner: np,
		ReservedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.244.0.0/16"), netip.MustParsePrefix("10.96.0.0/12")},
	})

	code, errCode := postNetwork(t, router, `{"name":"app","cidr":"10.100.0.0/16"}`)

	if code != http.StatusBadRequest || errCode != "NETWORK_CIDR_RESERVED" {
		t.Fatalf("expected 400 NETWORK_CIDR_RESERVED for a range inside 10.96.0.0/12, got %d %s", code, errCode)
	}
	if len(store.networks) != 0 || len(np.createdNetworks) != 0 {
		t.Fatal("reserved cidr must not be stored or provisioned")
	}
}

func TestCreateNetworkInvalidCIDRReturns400(t *testing.T) {
	router := NewRouterWithDependencies(Dependencies{
		Version: "test", Store: &fakeCatalogStore{},
		ReservedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.244.0.0/16")},
	})

	// 10.0.0.0/8 also overlaps a reserved range; the size error must win so the user learns the real problem.
	for _, cidr := range []string{"8.8.8.0/24", "10.0.0.0/8", "nope"} {
		code, errCode := postNetwork(t, router, `{"name":"app","cidr":"`+cidr+`"}`)
		if code != http.StatusBadRequest || errCode != "NETWORK_CIDR_INVALID" {
			t.Fatalf("%s: expected 400 NETWORK_CIDR_INVALID, got %d %s", cidr, code, errCode)
		}
	}
}

func TestCreateSubnetOutsideVPCReturns400(t *testing.T) {
	store := &fakeCatalogStore{
		networks:        []domain.Network{{ID: "platform-app", ProjectID: "platform", Name: "app", CIDR: "10.0.0.0/16", OVNVPCName: "kloudia-vpc-platform-app", Status: "Ready"}},
		createSubnetErr: storepkg.ErrSubnetCIDROutsideNetwork,
	}
	router := NewRouterWithDependencies(Dependencies{Version: "test", Store: store})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/networks/platform-app/subnets", bytes.NewBufferString(`{"name":"web","cidr":"10.1.0.0/24"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "SUBNET_CIDR_OUTSIDE_VPC") {
		t.Fatalf("expected 400 SUBNET_CIDR_OUTSIDE_VPC, got %d: %s", rec.Code, rec.Body.String())
	}
}
