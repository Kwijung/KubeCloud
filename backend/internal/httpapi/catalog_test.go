package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type fakeCatalogStore struct {
	projects    []domain.Project
	flavors     []domain.Flavor
	images      []domain.Image
	vms         []domain.VirtualMachine
	createdPlan domain.VirtualMachineProvisioningPlan
	networks    []domain.Network
	subnets     []domain.Subnet
}

type fakeStatusReader struct {
	statuses map[string]string
	details  map[string]domain.VirtualMachineDetail
}

type recordingConsoleStreamer struct {
	called bool
	vmID   string
}

func (s *recordingConsoleStreamer) StreamVirtualMachineConsole(w http.ResponseWriter, _ *http.Request, vm domain.VirtualMachine) error {
	s.called = true
	s.vmID = vm.ID
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (r fakeStatusReader) GetVirtualMachineStatus(_ context.Context, vm domain.VirtualMachine) (string, error) {
	if status, ok := r.statuses[vm.ID]; ok {
		return status, nil
	}
	return vm.Status, nil
}

func (r fakeStatusReader) GetVirtualMachineDetail(_ context.Context, vm domain.VirtualMachine) (domain.VirtualMachineDetail, error) {
	if detail, ok := r.details[vm.ID]; ok {
		return detail, nil
	}
	return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, nil
}

func (r fakeStatusReader) GetPreflight(context.Context) (domain.PreflightResult, error) {
	return domain.PreflightResult{
		Status: "OK",
		Checks: []domain.PreflightCheck{{
			Name:    "kubevirt",
			Status:  "OK",
			Message: "available",
		}},
	}, nil
}

func (s fakeCatalogStore) Ping(context.Context) error {
	return nil
}

func (s fakeCatalogStore) ListProjects(context.Context) ([]domain.Project, error) {
	return s.projects, nil
}

func (s fakeCatalogStore) ListFlavors(context.Context) ([]domain.Flavor, error) {
	return s.flavors, nil
}

func (s fakeCatalogStore) ListImages(_ context.Context, projectID string) ([]domain.Image, error) {
	var images []domain.Image
	for _, image := range s.images {
		if image.ProjectID == projectID {
			images = append(images, image)
		}
	}
	return images, nil
}

func (s fakeCatalogStore) ListVirtualMachines(_ context.Context, projectID string) ([]domain.VirtualMachine, error) {
	var vms []domain.VirtualMachine
	for _, vm := range s.vms {
		if vm.ProjectID == projectID {
			vms = append(vms, vm)
		}
	}
	return vms, nil
}

func (s fakeCatalogStore) CreateVirtualMachine(_ context.Context, projectID string, input domain.CreateVirtualMachineInput) (domain.VirtualMachineProvisioningPlan, error) {
	plan := s.createdPlan
	if plan.VirtualMachine.ID == "" {
		plan.VirtualMachine = domain.VirtualMachine{
			ID:        projectID + "-" + input.Name,
			ProjectID: projectID,
			Name:      input.Name,
			ImageID:   input.ImageID,
			FlavorID:  input.FlavorID,
			Status:    "Provisioning",
		}
	}
	return plan, nil
}

func (s *fakeCatalogStore) GetVirtualMachine(_ context.Context, projectID string, vmID string) (domain.VirtualMachine, error) {
	for _, vm := range s.vms {
		if vm.ProjectID == projectID && vm.ID == vmID {
			return vm, nil
		}
	}
	return domain.VirtualMachine{}, errors.New("virtual machine not found")
}

func (s *fakeCatalogStore) DeleteVirtualMachine(_ context.Context, projectID string, vmID string) error {
	for i, vm := range s.vms {
		if vm.ProjectID == projectID && vm.ID == vmID {
			s.vms = append(s.vms[:i], s.vms[i+1:]...)
			return nil
		}
	}
	return errors.New("virtual machine not found")
}

func (s *fakeCatalogStore) ListNetworks(_ context.Context, projectID string) ([]domain.Network, error) {
	var out []domain.Network
	for _, n := range s.networks {
		if n.ProjectID == projectID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *fakeCatalogStore) GetNetwork(_ context.Context, projectID string, networkID string) (domain.Network, error) {
	for _, n := range s.networks {
		if n.ProjectID == projectID && n.ID == networkID {
			return n, nil
		}
	}
	return domain.Network{}, errors.New("network not found")
}

func (s *fakeCatalogStore) CreateNetwork(_ context.Context, projectID string, name string) (domain.Network, error) {
	n := domain.Network{ID: projectID + "-" + name, ProjectID: projectID, Name: name, OVNVPCName: "kloudia-vpc-" + projectID + "-" + name, Status: "Ready"}
	s.networks = append(s.networks, n)
	return n, nil
}

func (s *fakeCatalogStore) DeleteNetwork(_ context.Context, projectID string, networkID string) (domain.Network, error) {
	for i, n := range s.networks {
		if n.ProjectID == projectID && n.ID == networkID {
			s.networks = append(s.networks[:i], s.networks[i+1:]...)
			return n, nil
		}
	}
	return domain.Network{}, errors.New("network not found")
}

func (s *fakeCatalogStore) ListSubnets(_ context.Context, projectID string, networkID string) ([]domain.Subnet, error) {
	var out []domain.Subnet
	for _, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.NetworkID == networkID {
			out = append(out, sn)
		}
	}
	return out, nil
}

func (s *fakeCatalogStore) GetSubnet(_ context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	for _, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.ID == subnetID {
			return sn, nil
		}
	}
	return domain.Subnet{}, errors.New("subnet not found")
}

func (s *fakeCatalogStore) CreateSubnet(_ context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error) {
	sn := domain.Subnet{ID: networkID + "-" + name, NetworkID: networkID, ProjectID: projectID, Name: name, CIDR: cidr, Gateway: gateway, Type: subnetType, OVNSubnetName: "kloudia-subnet-" + networkID + "-" + name, NADName: "kloudia-net-" + networkID + "-" + name, Status: "Ready"}
	s.subnets = append(s.subnets, sn)
	return sn, nil
}

func (s *fakeCatalogStore) DeleteSubnet(_ context.Context, projectID string, subnetID string) (domain.Subnet, error) {
	for i, sn := range s.subnets {
		if sn.ProjectID == projectID && sn.ID == subnetID {
			s.subnets = append(s.subnets[:i], s.subnets[i+1:]...)
			return sn, nil
		}
	}
	return domain.Subnet{}, errors.New("subnet not found")
}

func TestListProjectsReturnsProjectCatalog(t *testing.T) {
	createdAt := time.Date(2026, 6, 17, 9, 30, 0, 0, time.UTC)
	router := NewRouter("test", &fakeCatalogStore{
		projects: []domain.Project{{
			ID:          "platform",
			Name:        "Platform",
			Description: "Shared platform engineering workspace",
			CreatedAt:   createdAt,
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body struct {
		Data []domain.Project `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected one project, got %d", len(body.Data))
	}
	if body.Data[0].ID != "platform" {
		t.Fatalf("expected project platform, got %q", body.Data[0].ID)
	}
	if body.Data[0].CreatedAt != createdAt {
		t.Fatalf("expected created_at to round-trip, got %s", body.Data[0].CreatedAt)
	}
}

func TestListFlavorsReturnsEnabledComputeOptions(t *testing.T) {
	router := NewRouter("test", &fakeCatalogStore{
		flavors: []domain.Flavor{{
			ID:            "small",
			Name:          "Small",
			CPU:           2,
			MemoryBytes:   4 * 1024 * 1024 * 1024,
			RootDiskBytes: 20 * 1024 * 1024 * 1024,
			Enabled:       true,
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flavors", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body struct {
		Data []domain.Flavor `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected one flavor, got %d", len(body.Data))
	}
	if body.Data[0].CPU != 2 {
		t.Fatalf("expected 2 vCPU, got %d", body.Data[0].CPU)
	}
	if body.Data[0].MemoryBytes != 4*1024*1024*1024 {
		t.Fatalf("expected 4GiB memory, got %d", body.Data[0].MemoryBytes)
	}
}

func TestListProjectImagesFiltersByProject(t *testing.T) {
	router := NewRouter("test", &fakeCatalogStore{
		images: []domain.Image{
			{ID: "ubuntu-22", ProjectID: "platform", Name: "Ubuntu 22.04", OSType: "linux", SourceURL: "https://example.invalid/ubuntu.qcow2", Status: "Ready"},
			{ID: "debian-12", ProjectID: "data", Name: "Debian 12", OSType: "linux", SourceURL: "https://example.invalid/debian.qcow2", Status: "Importing"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/images", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body struct {
		Data []domain.Image `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected one image, got %d", len(body.Data))
	}
	if body.Data[0].ProjectID != "platform" {
		t.Fatalf("expected platform image, got project %q", body.Data[0].ProjectID)
	}
}

func TestListProjectVirtualMachinesFiltersByProject(t *testing.T) {
	router := NewRouter("test", &fakeCatalogStore{
		vms: []domain.VirtualMachine{
			{ID: "vm-1", ProjectID: "platform", Name: "build-runner-01", ImageID: "ubuntu-22", FlavorID: "small", Status: "Running"},
			{ID: "vm-2", ProjectID: "data", Name: "warehouse-01", ImageID: "debian-12", FlavorID: "medium", Status: "Stopped"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body struct {
		Data []domain.VirtualMachine `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected one vm, got %d", len(body.Data))
	}
	if body.Data[0].Name != "build-runner-01" {
		t.Fatalf("expected build-runner-01, got %q", body.Data[0].Name)
	}
}

func TestListProjectVirtualMachinesOverlaysRuntimeStatus(t *testing.T) {
	router := NewRouterWithProvisionerAndStatusReader("test", &fakeCatalogStore{
		vms: []domain.VirtualMachine{
			{ID: "platform-portal-02", ProjectID: "platform", Name: "portal-02", ImageID: "platform-cirros", FlavorID: "small", Status: "Provisioning"},
		},
	}, noopProvisioner{}, fakeStatusReader{
		statuses: map[string]string{"platform-portal-02": "Running"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body struct {
		Data []domain.VirtualMachine `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected one vm, got %d", len(body.Data))
	}
	if body.Data[0].Status != "Running" {
		t.Fatalf("expected runtime status Running, got %q", body.Data[0].Status)
	}
}

func TestGetPreflightReturnsClusterReadinessChecks(t *testing.T) {
	router := NewRouterWithProvisionerAndStatusReader("test", &fakeCatalogStore{}, noopProvisioner{}, fakeStatusReader{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/preflight", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data domain.PreflightResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "OK" {
		t.Fatalf("expected preflight OK, got %q", body.Data.Status)
	}
	if len(body.Data.Checks) != 1 || body.Data.Checks[0].Name != "kubevirt" {
		t.Fatalf("unexpected preflight checks: %#v", body.Data.Checks)
	}
}

func TestGetVirtualMachineReturnsRuntimeDetail(t *testing.T) {
	vm := domain.VirtualMachine{ID: "platform-portal-02", ProjectID: "platform", Name: "portal-02", ImageID: "platform-cirros", FlavorID: "small", Status: "Provisioning"}
	router := NewRouterWithProvisionerAndStatusReader("test", &fakeCatalogStore{
		vms: []domain.VirtualMachine{vm},
	}, noopProvisioner{}, fakeStatusReader{
		details: map[string]domain.VirtualMachineDetail{
			"platform-portal-02": {
				VirtualMachine:   vm,
				RuntimeStatus:    "Running",
				FailureReason:    "Unschedulable",
				FailureMessage:   "0/1 nodes are available",
				VMIPhase:         "Running",
				VMIIP:            "10.244.1.183",
				NodeName:         "test-worker-ca9c9bc3",
				DataVolumePhase:  "Succeeded",
				DataVolumeStatus: "100.0%",
				PVCPhase:         "Bound",
				PVCCapacity:      "2Gi",
				ConsoleAvailable: true,
				Events: []domain.RuntimeEvent{{
					Type:    "Normal",
					Reason:  "Started",
					Message: "VM started",
				}},
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms/platform-portal-02", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data domain.VirtualMachineDetail `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.RuntimeStatus != "Running" {
		t.Fatalf("expected Running, got %q", body.Data.RuntimeStatus)
	}
	if body.Data.VMIIP != "10.244.1.183" {
		t.Fatalf("expected VMI IP, got %q", body.Data.VMIIP)
	}
	if body.Data.FailureReason != "Unschedulable" || body.Data.FailureMessage != "0/1 nodes are available" {
		t.Fatalf("expected failure detail, got reason=%q message=%q", body.Data.FailureReason, body.Data.FailureMessage)
	}
	if !body.Data.ConsoleAvailable {
		t.Fatalf("expected console available")
	}
	if len(body.Data.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(body.Data.Events))
	}
}

func TestStreamConsoleRejectsStoppedVirtualMachine(t *testing.T) {
	vm := domain.VirtualMachine{ID: "platform-portal-02", ProjectID: "platform", Name: "portal-02", ImageID: "platform-cirros", FlavorID: "small", Status: "Stopped"}
	streamer := &recordingConsoleStreamer{}
	router := NewRouterWithProvisionerStatusReaderAndConsoleStreamer("test", &fakeCatalogStore{
		vms: []domain.VirtualMachine{vm},
	}, noopProvisioner{}, fakeStatusReader{
		details: map[string]domain.VirtualMachineDetail{
			"platform-portal-02": {
				VirtualMachine:   vm,
				RuntimeStatus:    "Stopped",
				ConsoleAvailable: false,
			},
		},
	}, streamer)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms/platform-portal-02/console/stream", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if streamer.called {
		t.Fatalf("expected console streamer not to be called")
	}
}

func TestStreamConsoleDelegatesRunningVirtualMachine(t *testing.T) {
	vm := domain.VirtualMachine{ID: "platform-portal-02", ProjectID: "platform", Name: "portal-02", ImageID: "platform-cirros", FlavorID: "small", Status: "Running"}
	streamer := &recordingConsoleStreamer{}
	router := NewRouterWithProvisionerStatusReaderAndConsoleStreamer("test", &fakeCatalogStore{
		vms: []domain.VirtualMachine{vm},
	}, noopProvisioner{}, fakeStatusReader{
		details: map[string]domain.VirtualMachineDetail{
			"platform-portal-02": {
				VirtualMachine:   vm,
				RuntimeStatus:    "Running",
				ConsoleAvailable: true,
			},
		},
	}, streamer)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms/platform-portal-02/console/stream", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !streamer.called || streamer.vmID != "platform-portal-02" {
		t.Fatalf("expected console streamer for platform-portal-02, called=%t vmID=%q", streamer.called, streamer.vmID)
	}
}
