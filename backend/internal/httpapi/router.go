package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/kloudia/kloudia/backend/internal/domain"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

type Store interface {
	Ping(ctx context.Context) error
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ListFlavors(ctx context.Context) ([]domain.Flavor, error)
	ListImages(ctx context.Context, projectID string) ([]domain.Image, error)
	ListVirtualMachines(ctx context.Context, projectID string) ([]domain.VirtualMachine, error)
	CreateVirtualMachine(ctx context.Context, projectID string, input domain.CreateVirtualMachineInput) (domain.VirtualMachineProvisioningPlan, error)
	GetVirtualMachine(ctx context.Context, projectID string, vmID string) (domain.VirtualMachine, error)
	DeleteVirtualMachine(ctx context.Context, projectID string, vmID string) error
	ListNetworks(ctx context.Context, projectID string) ([]domain.Network, error)
	GetNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	CreateNetwork(ctx context.Context, projectID string, name string) (domain.Network, error)
	DeleteNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	ListSubnets(ctx context.Context, projectID string, networkID string) ([]domain.Subnet, error)
	GetSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
	CreateSubnet(ctx context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error)
	DeleteSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
}

type Provisioner interface {
	CreateVirtualMachine(ctx context.Context, plan domain.VirtualMachineProvisioningPlan) error
	DeleteVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
	StartVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
	StopVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
}

type StatusReader interface {
	GetVirtualMachineStatus(ctx context.Context, vm domain.VirtualMachine) (string, error)
	GetVirtualMachineDetail(ctx context.Context, vm domain.VirtualMachine) (domain.VirtualMachineDetail, error)
	GetPreflight(ctx context.Context) (domain.PreflightResult, error)
}

type ConsoleStreamer interface {
	StreamVirtualMachineConsole(w http.ResponseWriter, r *http.Request, vm domain.VirtualMachine) error
}

type noopProvisioner struct{}

func (noopProvisioner) CreateVirtualMachine(context.Context, domain.VirtualMachineProvisioningPlan) error {
	return nil
}

func (noopProvisioner) DeleteVirtualMachine(context.Context, domain.VirtualMachine) error {
	return nil
}

func (noopProvisioner) StartVirtualMachine(context.Context, domain.VirtualMachine) error {
	return nil
}

func (noopProvisioner) StopVirtualMachine(context.Context, domain.VirtualMachine) error {
	return nil
}

type noopStatusReader struct{}

func (noopStatusReader) GetVirtualMachineStatus(_ context.Context, vm domain.VirtualMachine) (string, error) {
	return vm.Status, nil
}

func (noopStatusReader) GetVirtualMachineDetail(_ context.Context, vm domain.VirtualMachine) (domain.VirtualMachineDetail, error) {
	return domain.VirtualMachineDetail{VirtualMachine: vm, RuntimeStatus: vm.Status}, nil
}

func (noopStatusReader) GetPreflight(context.Context) (domain.PreflightResult, error) {
	return domain.PreflightResult{
		Status: "Warning",
		Checks: []domain.PreflightCheck{{
			Name:    "kubevirt",
			Status:  "Unknown",
			Message: "KubeVirt integration is disabled",
		}},
	}, nil
}

type noopConsoleStreamer struct{}

func (noopConsoleStreamer) StreamVirtualMachineConsole(http.ResponseWriter, *http.Request, domain.VirtualMachine) error {
	return nil
}

type NetworkProvisioner interface {
	CreateNetwork(ctx context.Context, network domain.Network) error
	DeleteNetwork(ctx context.Context, network domain.Network) error
	CreateSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error
	DeleteSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet) error
}

type noopNetworkProvisioner struct{}

func (noopNetworkProvisioner) CreateNetwork(context.Context, domain.Network) error       { return nil }
func (noopNetworkProvisioner) DeleteNetwork(context.Context, domain.Network) error       { return nil }
func (noopNetworkProvisioner) CreateSubnet(context.Context, string, domain.Subnet) error { return nil }
func (noopNetworkProvisioner) DeleteSubnet(context.Context, string, domain.Subnet) error { return nil }

type Dependencies struct {
	Version            string
	Store              Store
	Provisioner        Provisioner
	StatusReader       StatusReader
	ConsoleStreamer    ConsoleStreamer
	NetworkProvisioner NetworkProvisioner
}

func NewRouter(version string, store Store) http.Handler {
	return NewRouterWithProvisioner(version, store, noopProvisioner{})
}

func NewRouterWithProvisioner(version string, store Store, provisioner Provisioner) http.Handler {
	return NewRouterWithProvisionerAndStatusReader(version, store, provisioner, noopStatusReader{})
}

func NewRouterWithProvisionerAndStatusReader(version string, store Store, provisioner Provisioner, statusReader StatusReader) http.Handler {
	return NewRouterWithProvisionerStatusReaderAndConsoleStreamer(version, store, provisioner, statusReader, noopConsoleStreamer{})
}

func NewRouterWithProvisionerStatusReaderAndConsoleStreamer(version string, store Store, provisioner Provisioner, statusReader StatusReader, consoleStreamer ConsoleStreamer) http.Handler {
	return NewRouterWithDependencies(Dependencies{
		Version:         version,
		Store:           store,
		Provisioner:     provisioner,
		StatusReader:    statusReader,
		ConsoleStreamer: consoleStreamer,
	})
}

func NewRouterWithDependencies(deps Dependencies) http.Handler {
	version := deps.Version
	store := deps.Store
	provisioner := deps.Provisioner
	statusReader := deps.StatusReader
	consoleStreamer := deps.ConsoleStreamer
	networkProvisioner := deps.NetworkProvisioner
	if provisioner == nil {
		provisioner = noopProvisioner{}
	}
	if statusReader == nil {
		statusReader = noopStatusReader{}
	}
	if consoleStreamer == nil {
		consoleStreamer = noopConsoleStreamer{}
	}
	if networkProvisioner == nil {
		networkProvisioner = noopNetworkProvisioner{}
	}
	router := chi.NewRouter()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "kloudia-api",
			"status":  "ok",
		})
	})

	router.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		if store != nil {
			if err := store.Ping(r.Context()); err != nil {
				status = "degraded"
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "kloudia-api",
			"status":  status,
			"version": version,
		})
	})

	router.Get("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		projects, err := store.ListProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PROJECTS_UNAVAILABLE", "failed to list projects")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": projects})
	})

	router.Get("/api/v1/flavors", func(w http.ResponseWriter, r *http.Request) {
		flavors, err := store.ListFlavors(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "FLAVORS_UNAVAILABLE", "failed to list flavors")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": flavors})
	})

	router.Get("/api/v1/preflight", func(w http.ResponseWriter, r *http.Request) {
		result, err := statusReader.GetPreflight(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PREFLIGHT_UNAVAILABLE", "failed to run preflight checks")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": result})
	})

	router.Get("/api/v1/projects/{projectID}/images", func(w http.ResponseWriter, r *http.Request) {
		images, err := store.ListImages(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "IMAGES_UNAVAILABLE", "failed to list images")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": images})
	})

	router.Get("/api/v1/projects/{projectID}/vms", func(w http.ResponseWriter, r *http.Request) {
		vms, err := store.ListVirtualMachines(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VMS_UNAVAILABLE", "failed to list virtual machines")
			return
		}
		for i := range vms {
			status, err := statusReader.GetVirtualMachineStatus(r.Context(), vms[i])
			if err == nil && status != "" {
				vms[i].Status = status
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": vms})
	})

	router.Get("/api/v1/projects/{projectID}/vms/{vmID}", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		detail, err := statusReader.GetVirtualMachineDetail(r.Context(), vm)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VM_DETAIL_UNAVAILABLE", "failed to read virtual machine detail")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": detail})
	})

	router.Post("/api/v1/projects/{projectID}/vms", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name     string `json:"name"`
			ImageID  string `json:"imageId"`
			FlavorID string `json:"flavorId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" || request.ImageID == "" || request.FlavorID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_VM_REQUEST", "name, imageId, and flavorId are required")
			return
		}
		plan, err := store.CreateVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), domain.CreateVirtualMachineInput{
			Name:     request.Name,
			ImageID:  request.ImageID,
			FlavorID: request.FlavorID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VM_CREATE_FAILED", "failed to create virtual machine")
			return
		}
		if provisioner != nil {
			if err := provisioner.CreateVirtualMachine(r.Context(), plan); err != nil {
				writeError(w, http.StatusBadGateway, "VM_PROVISION_FAILED", "failed to provision virtual machine")
				return
			}
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": plan.VirtualMachine})
	})

	router.Delete("/api/v1/projects/{projectID}/vms/{vmID}", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		vmID := chi.URLParam(r, "vmID")
		vm, err := store.GetVirtualMachine(r.Context(), projectID, vmID)
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		if provisioner != nil {
			if err := provisioner.DeleteVirtualMachine(r.Context(), vm); err != nil {
				writeError(w, http.StatusBadGateway, "VM_DELETE_FAILED", "failed to delete virtual machine resources")
				return
			}
		}
		if err := store.DeleteVirtualMachine(r.Context(), projectID, vmID); err != nil {
			writeError(w, http.StatusInternalServerError, "VM_RECORD_DELETE_FAILED", "failed to delete virtual machine record")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	router.Post("/api/v1/projects/{projectID}/vms/{vmID}/start", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		if err := provisioner.StartVirtualMachine(r.Context(), vm); err != nil {
			writeError(w, http.StatusBadGateway, "VM_START_FAILED", "failed to start virtual machine")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	router.Post("/api/v1/projects/{projectID}/vms/{vmID}/stop", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		if err := provisioner.StopVirtualMachine(r.Context(), vm); err != nil {
			writeError(w, http.StatusBadGateway, "VM_STOP_FAILED", "failed to stop virtual machine")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	router.Get("/api/v1/projects/{projectID}/vms/{vmID}/console", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		detail, err := statusReader.GetVirtualMachineDetail(r.Context(), vm)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VM_CONSOLE_UNAVAILABLE", "failed to read console capability")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"available": detail.ConsoleAvailable && detail.RuntimeStatus == "Running",
				"message":   "KubeVirt console subresource is available; browser streaming is not implemented yet",
			},
		})
	})

	router.Get("/api/v1/projects/{projectID}/vms/{vmID}/console/stream", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		detail, err := statusReader.GetVirtualMachineDetail(r.Context(), vm)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VM_CONSOLE_UNAVAILABLE", "failed to read console capability")
			return
		}
		if !detail.ConsoleAvailable || detail.RuntimeStatus != "Running" {
			writeError(w, http.StatusConflict, "VM_CONSOLE_UNAVAILABLE", "virtual machine console is available only while the VM is running")
			return
		}
		if consoleStreamer == nil {
			writeError(w, http.StatusNotImplemented, "VM_CONSOLE_NOT_CONFIGURED", "console streamer is not configured")
			return
		}
		if err := consoleStreamer.StreamVirtualMachineConsole(w, r, vm); err != nil {
			writeError(w, http.StatusBadGateway, "VM_CONSOLE_STREAM_FAILED", "failed to stream virtual machine console")
			return
		}
	})

	router.Get("/api/v1/projects/{projectID}/networks", func(w http.ResponseWriter, r *http.Request) {
		networks, err := store.ListNetworks(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NETWORKS_UNAVAILABLE", "failed to list networks")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": networks})
	})

	router.Post("/api/v1/projects/{projectID}/networks", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_NETWORK_REQUEST", "name is required")
			return
		}
		network, err := store.CreateNetwork(r.Context(), chi.URLParam(r, "projectID"), request.Name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NETWORK_CREATE_FAILED", "failed to create network")
			return
		}
		if err := networkProvisioner.CreateNetwork(r.Context(), network); err != nil {
			writeError(w, http.StatusBadGateway, "NETWORK_PROVISION_FAILED", "failed to provision network")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": network})
	})

	router.Delete("/api/v1/projects/{projectID}/networks/{networkID}", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		if _, err := store.DeleteNetwork(r.Context(), projectID, networkID); err != nil {
			if errors.Is(err, storepkg.ErrNetworkInUse) {
				writeError(w, http.StatusConflict, "NETWORK_IN_USE", "network still has subnets")
				return
			}
			writeError(w, http.StatusInternalServerError, "NETWORK_DELETE_FAILED", "failed to delete network")
			return
		}
		if err := networkProvisioner.DeleteNetwork(r.Context(), network); err != nil {
			writeError(w, http.StatusBadGateway, "NETWORK_DEPROVISION_FAILED", "failed to delete network resources")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	router.Get("/api/v1/projects/{projectID}/networks/{networkID}/subnets", func(w http.ResponseWriter, r *http.Request) {
		subnets, err := store.ListSubnets(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "networkID"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SUBNETS_UNAVAILABLE", "failed to list subnets")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": subnets})
	})

	router.Post("/api/v1/projects/{projectID}/networks/{networkID}/subnets", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		var request struct {
			Name    string `json:"name"`
			CIDR    string `json:"cidr"`
			Type    string `json:"type"`
			Gateway string `json:"gateway"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" || request.CIDR == "" {
			writeError(w, http.StatusBadRequest, "INVALID_SUBNET_REQUEST", "name and cidr are required")
			return
		}
		if request.Type == "" {
			request.Type = "private"
		}
		if request.Type != "private" && request.Type != "public" {
			writeError(w, http.StatusBadRequest, "SUBNET_TYPE_INVALID", "type must be private or public")
			return
		}
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		subnet, err := store.CreateSubnet(r.Context(), projectID, networkID, request.Name, request.CIDR, request.Type, request.Gateway)
		if err != nil {
			switch {
			case errors.Is(err, storepkg.ErrSubnetCIDRInvalid):
				writeError(w, http.StatusBadRequest, "SUBNET_CIDR_INVALID", "subnet cidr is invalid")
			case errors.Is(err, storepkg.ErrSubnetCIDROverlap):
				writeError(w, http.StatusConflict, "SUBNET_CIDR_OVERLAP", "subnet cidr overlaps an existing subnet in this vpc")
			default:
				writeError(w, http.StatusInternalServerError, "SUBNET_CREATE_FAILED", "failed to create subnet")
			}
			return
		}
		if err := networkProvisioner.CreateSubnet(r.Context(), network.OVNVPCName, subnet); err != nil {
			writeError(w, http.StatusBadGateway, "SUBNET_PROVISION_FAILED", "failed to provision subnet")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"data": subnet})
	})

	router.Delete("/api/v1/projects/{projectID}/networks/{networkID}/subnets/{subnetID}", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		networkID := chi.URLParam(r, "networkID")
		subnetID := chi.URLParam(r, "subnetID")
		network, err := store.GetNetwork(r.Context(), projectID, networkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			return
		}
		subnet, err := store.GetSubnet(r.Context(), projectID, subnetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "SUBNET_NOT_FOUND", "subnet was not found")
			return
		}
		if _, err := store.DeleteSubnet(r.Context(), projectID, subnetID); err != nil {
			if errors.Is(err, storepkg.ErrSubnetInUse) {
				writeError(w, http.StatusConflict, "SUBNET_IN_USE", "subnet still has virtual machines attached")
				return
			}
			writeError(w, http.StatusInternalServerError, "SUBNET_DELETE_FAILED", "failed to delete subnet")
			return
		}
		if err := networkProvisioner.DeleteSubnet(r.Context(), network.OVNVPCName, subnet); err != nil {
			writeError(w, http.StatusBadGateway, "SUBNET_DEPROVISION_FAILED", "failed to delete subnet resources")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return router
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
