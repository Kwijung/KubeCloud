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

type recordingProvisioner struct {
	plans      []domain.VirtualMachineProvisioningPlan
	deletedVMs []domain.VirtualMachine
	startedVMs []domain.VirtualMachine
	stoppedVMs []domain.VirtualMachine
}

func (p *recordingProvisioner) CreateVirtualMachine(_ context.Context, plan domain.VirtualMachineProvisioningPlan) error {
	p.plans = append(p.plans, plan)
	return nil
}

func (p *recordingProvisioner) DeleteVirtualMachine(_ context.Context, vm domain.VirtualMachine) error {
	p.deletedVMs = append(p.deletedVMs, vm)
	return nil
}

func (p *recordingProvisioner) StartVirtualMachine(_ context.Context, vm domain.VirtualMachine) error {
	p.startedVMs = append(p.startedVMs, vm)
	return nil
}

func (p *recordingProvisioner) StopVirtualMachine(_ context.Context, vm domain.VirtualMachine) error {
	p.stoppedVMs = append(p.stoppedVMs, vm)
	return nil
}

func TestCreateVirtualMachinePersistsProvisioningRecordAndCallsProvisioner(t *testing.T) {
	plan := domain.VirtualMachineProvisioningPlan{
		VirtualMachine: domain.VirtualMachine{
			ID:        "platform-web-01",
			ProjectID: "platform",
			Name:      "web-01",
			ImageID:   "platform-cirros",
			FlavorID:  "small",
			Status:    "Provisioning",
		},
		Image: domain.Image{
			ID:        "platform-cirros",
			ProjectID: "platform",
			Name:      "CirrOS",
			SourceURL: "https://download.cirros-cloud.net/0.6.2/cirros-0.6.2-x86_64-disk.img",
			Status:    "Ready",
		},
		Flavor: domain.Flavor{
			ID:            "small",
			Name:          "Small",
			CPU:           2,
			MemoryBytes:   4 * 1024 * 1024 * 1024,
			RootDiskBytes: 20 * 1024 * 1024 * 1024,
			Enabled:       true,
		},
	}
	store := &fakeCatalogStore{createdPlan: plan}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)
	body := bytes.NewBufferString(`{"name":"web-01","imageId":"platform-cirros","flavorId":"small"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data domain.VirtualMachine `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.Status != "Provisioning" {
		t.Fatalf("expected Provisioning status, got %q", response.Data.Status)
	}
	if len(provisioner.plans) != 1 {
		t.Fatalf("expected provisioner to be called once, got %d", len(provisioner.plans))
	}
	if provisioner.plans[0].VirtualMachine.Name != "web-01" {
		t.Fatalf("expected provisioned VM web-01, got %q", provisioner.plans[0].VirtualMachine.Name)
	}
}

func TestCreateVirtualMachineRejectsMissingFields(t *testing.T) {
	router := NewRouterWithProvisioner("test", &fakeCatalogStore{}, &recordingProvisioner{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms", bytes.NewBufferString(`{"name":""}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestDeleteVirtualMachineDeletesRuntimeResourcesThenStoreRecord(t *testing.T) {
	store := &fakeCatalogStore{
		vms: []domain.VirtualMachine{{
			ID:        "platform-portal-01",
			ProjectID: "platform",
			Name:      "portal-01",
			ImageID:   "platform-cirros",
			FlavorID:  "small",
			Status:    "Missing",
		}},
	}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/platform/vms/platform-portal-01", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(provisioner.deletedVMs) != 1 {
		t.Fatalf("expected provisioner delete once, got %d", len(provisioner.deletedVMs))
	}
	if provisioner.deletedVMs[0].ID != "platform-portal-01" {
		t.Fatalf("expected deleted vm platform-portal-01, got %q", provisioner.deletedVMs[0].ID)
	}
	if len(store.vms) != 0 {
		t.Fatalf("expected store VM record deleted, got %#v", store.vms)
	}
}

func TestCreateVirtualMachinePassesSubnetID(t *testing.T) {
	store := &fakeCatalogStore{}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)
	body := bytes.NewBufferString(`{"name":"web-09","imageId":"platform-cirros","flavorId":"small","subnetId":"platform-app-web"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.createInputs) != 1 || store.createInputs[0].SubnetID != "platform-app-web" {
		t.Fatalf("expected subnetId to reach the store, got %#v", store.createInputs)
	}
}

func TestStartAndStopVirtualMachineCallsProvisioner(t *testing.T) {
	store := &fakeCatalogStore{
		vms: []domain.VirtualMachine{{
			ID:        "platform-portal-02",
			ProjectID: "platform",
			Name:      "portal-02",
			ImageID:   "platform-cirros",
			FlavorID:  "small",
			Status:    "Stopped",
		}},
	}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)

	for _, item := range []struct {
		path string
		want string
	}{
		{path: "/api/v1/projects/platform/vms/platform-portal-02/start", want: "start"},
		{path: "/api/v1/projects/platform/vms/platform-portal-02/stop", want: "stop"},
	} {
		req := httptest.NewRequest(http.MethodPost, item.path, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: expected status 202, got %d: %s", item.want, rec.Code, rec.Body.String())
		}
	}

	if len(provisioner.startedVMs) != 1 || provisioner.startedVMs[0].ID != "platform-portal-02" {
		t.Fatalf("expected start provisioner call, got %#v", provisioner.startedVMs)
	}
	if len(provisioner.stoppedVMs) != 1 || provisioner.stoppedVMs[0].ID != "platform-portal-02" {
		t.Fatalf("expected stop provisioner call, got %#v", provisioner.stoppedVMs)
	}
}

func TestCreateVirtualMachineUnknownSubnetReturns400(t *testing.T) {
	store := &fakeCatalogStore{}
	provisioner := &recordingProvisioner{}
	router := NewRouterWithProvisioner("test", store, provisioner)
	body := bytes.NewBufferString(`{"name":"web-99","imageId":"platform-cirros","flavorId":"small","subnetId":"missing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms", body)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var respBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if respBody.Error.Code != "INVALID_SUBNET" {
		t.Fatalf("expected INVALID_SUBNET, got %q", respBody.Error.Code)
	}
}
