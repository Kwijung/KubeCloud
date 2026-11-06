package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/kloudia/kloudia/backend/internal/domain"
	"github.com/kloudia/kloudia/backend/internal/kubevirt"
	storepkg "github.com/kloudia/kloudia/backend/internal/store"
)

type Store interface {
	Ping(ctx context.Context) error
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ListFlavors(ctx context.Context) ([]domain.Flavor, error)
	ListAllFlavors(ctx context.Context) ([]domain.Flavor, error)
	CreateFlavor(ctx context.Context, input domain.FlavorInput) (domain.Flavor, error)
	UpdateFlavor(ctx context.Context, id string, update domain.FlavorUpdate) (domain.Flavor, error)
	DeleteFlavor(ctx context.Context, id string) error
	ListImages(ctx context.Context, projectID string) ([]domain.Image, error)
	CreateImage(ctx context.Context, projectID string, input domain.CreateImageInput) (domain.Image, error)
	DeleteImage(ctx context.Context, projectID string, imageID string) error
	ListPublicImages(ctx context.Context) ([]domain.Image, error)
	CreatePublicImage(ctx context.Context, input domain.CreateImageInput) (domain.Image, error)
	DeletePublicImage(ctx context.Context, imageID string) error
	ListVirtualMachines(ctx context.Context, projectID string) ([]domain.VirtualMachine, error)
	CreateVirtualMachine(ctx context.Context, projectID string, input domain.CreateVirtualMachineInput) (domain.VirtualMachineProvisioningPlan, error)
	GetVirtualMachine(ctx context.Context, projectID string, vmID string) (domain.VirtualMachine, error)
	DeleteVirtualMachine(ctx context.Context, projectID string, vmID string) error
	ListNetworks(ctx context.Context, projectID string) ([]domain.Network, error)
	GetNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	CreateNetwork(ctx context.Context, projectID string, name string, cidr string) (domain.Network, error)
	DeleteNetwork(ctx context.Context, projectID string, networkID string) (domain.Network, error)
	ListSubnets(ctx context.Context, projectID string, networkID string) ([]domain.Subnet, error)
	GetSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
	CreateSubnet(ctx context.Context, projectID string, networkID string, name string, cidr string, subnetType string, gateway string) (domain.Subnet, error)
	DeleteSubnet(ctx context.Context, projectID string, subnetID string) (domain.Subnet, error)
}

// ImageProber reads an image's virtual disk size from its URL.
type ImageProber interface {
	VirtualSize(ctx context.Context, url string) (int64, error)
}

type Provisioner interface {
	CreateVirtualMachine(ctx context.Context, plan domain.VirtualMachineProvisioningPlan) error
	DeleteVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
	StartVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
	StopVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
	RestartVirtualMachine(ctx context.Context, vm domain.VirtualMachine) error
}

// ImageCache keeps registered images imported in the cluster so VMs clone them instead of downloading.
type ImageCache interface {
	// Delete removes an image's cache once the image is deleted.
	Delete(ctx context.Context, imageID string) error
}

// BootLogReader reads a VM's serial console output since it last started.
type BootLogReader interface {
	BootLog(ctx context.Context, vmID string) (string, error)
}

type StatusReader interface {
	GetVirtualMachineStatus(ctx context.Context, vm domain.VirtualMachine) (string, error)
	GetVirtualMachineDetail(ctx context.Context, vm domain.VirtualMachine) (domain.VirtualMachineDetail, error)
	GetPreflight(ctx context.Context) (domain.PreflightResult, error)
}

type ConsoleStreamer interface {
	StreamVirtualMachineConsole(w http.ResponseWriter, r *http.Request, vm domain.VirtualMachine) error
	// ConsoleInUse reports whether a browser is connected to the VM's console; connecting again cuts it off.
	ConsoleInUse(vmID string) bool
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

func (noopProvisioner) RestartVirtualMachine(context.Context, domain.VirtualMachine) error {
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

func (noopConsoleStreamer) ConsoleInUse(string) bool {
	return false
}

type NetworkProvisioner interface {
	CreateNetwork(ctx context.Context, network domain.Network) error
	DeleteNetwork(ctx context.Context, network domain.Network) error
	// publicSubnets are the VPC's public subnets including the new one; they drive the shared NAT gateway and egress routes.
	CreateSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet, publicSubnets []domain.Subnet) error
	// remainingPublicSubnets are the VPC's public subnets left once this one is deleted.
	DeleteSubnet(ctx context.Context, ovnVPCName string, subnet domain.Subnet, remainingPublicSubnets []domain.Subnet) error
}

type noopNetworkProvisioner struct{}

func (noopNetworkProvisioner) CreateNetwork(context.Context, domain.Network) error { return nil }
func (noopNetworkProvisioner) DeleteNetwork(context.Context, domain.Network) error { return nil }
func (noopNetworkProvisioner) CreateSubnet(context.Context, string, domain.Subnet, []domain.Subnet) error {
	return nil
}
func (noopNetworkProvisioner) DeleteSubnet(context.Context, string, domain.Subnet, []domain.Subnet) error {
	return nil
}

// overlappingReservedCIDR returns the first reserved range that cidr overlaps.
func overlappingReservedCIDR(cidr string, reserved []netip.Prefix) (string, bool) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", false
	}
	for _, r := range reserved {
		if r.Overlaps(prefix) {
			return r.String(), true
		}
	}
	return "", false
}

// publicSubnetsOf lists the public subnets in subnet's VPC, leaving out excludeID. Private subnets do not
// touch the NAT gateway, so they get nil without a lookup.
// rollbackSubnet removes a subnet record whose provisioning failed. The caller holds the VPC lock from
// before the record was saved, so no later public subnet exists yet and the store does not refuse the
// delete for hosting the NAT gateway.
func rollbackSubnet(ctx context.Context, store Store, subnet domain.Subnet) {
	if _, err := store.DeleteSubnet(ctx, subnet.ProjectID, subnet.ID); err != nil {
		log.Printf("roll back subnet %s: %v", subnet.ID, err)
	}
}

func publicSubnetsOf(ctx context.Context, store Store, subnet domain.Subnet, excludeID string) ([]domain.Subnet, error) {
	if subnet.Type != "public" {
		return nil, nil
	}
	subnets, err := store.ListSubnets(ctx, subnet.ProjectID, subnet.NetworkID)
	if err != nil {
		return nil, err
	}
	var public []domain.Subnet
	for _, s := range subnets {
		if s.Type == "public" && s.ID != excludeID {
			public = append(public, s)
		}
	}
	return public, nil
}

type Dependencies struct {
	Version            string
	Store              Store
	Provisioner        Provisioner
	StatusReader       StatusReader
	ConsoleStreamer    ConsoleStreamer
	NetworkProvisioner NetworkProvisioner
	// ReservedCIDRs are ranges a VPC CIDR must not overlap (cluster pod/service/kube-ovn ranges).
	ReservedCIDRs []netip.Prefix
	// ImageProber reads image sizes at registration; nil skips it.
	ImageProber ImageProber
	// VPCNetworkingUnavailable is set when VMs are provisioned but VPC networking (kube-ovn) is not: a VM
	// attached to a subnet would never start, so VM creation with a subnet is refused.
	VPCNetworkingUnavailable bool
	// BootLogReader serves the VM boot log; nil answers 501.
	BootLogReader BootLogReader
	// ImageCache imports newly registered images into the cluster; nil keeps images download-only.
	ImageCache ImageCache
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
	locks := &vpcLocks{}
	router := chi.NewRouter()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:5174", "http://127.0.0.1:5173", "http://127.0.0.1:5174"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
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

	// Projects choose from enabled flavors. includeDisabled=true also returns hidden ones, so the console can
	// still show the size of VMs created from a flavor that was later hidden.
	router.Get("/api/v1/flavors", func(w http.ResponseWriter, r *http.Request) {
		list := store.ListFlavors
		if r.URL.Query().Get("includeDisabled") == "true" {
			list = store.ListAllFlavors
		}
		flavors, err := list(r.Context())
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

	router.Post("/api/v1/projects/{projectID}/images", func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		createImage(w, r, deps.ImageProber, deps.ImageCache != nil, func(ctx context.Context, input domain.CreateImageInput) (domain.Image, error) {
			return store.CreateImage(ctx, projectID, input)
		})
	})

	// A public image is not deletable here: DeleteImage only matches the project's own images.
	router.Delete("/api/v1/projects/{projectID}/images/{imageID}", func(w http.ResponseWriter, r *http.Request) {
		imageID := chi.URLParam(r, "imageID")
		err := store.DeleteImage(r.Context(), chi.URLParam(r, "projectID"), imageID)
		deleteImageCache(r.Context(), deps.ImageCache, imageID, err)
		writeImageDeleteResult(w, imageID, err)
	})

	// Public images. These admin routes are unauthenticated until accounts and roles land; then only the
	// admin role may call them.
	registerFlavorRoutes(router, store)

	router.Get("/api/v1/admin/images", func(w http.ResponseWriter, r *http.Request) {
		images, err := store.ListPublicImages(r.Context())
		if err != nil {
			log.Printf("list public images: %v", err)
			writeError(w, http.StatusInternalServerError, "IMAGES_UNAVAILABLE", "failed to list public images")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": images})
	})

	router.Post("/api/v1/admin/images", func(w http.ResponseWriter, r *http.Request) {
		createImage(w, r, deps.ImageProber, deps.ImageCache != nil, store.CreatePublicImage)
	})

	router.Delete("/api/v1/admin/images/{imageID}", func(w http.ResponseWriter, r *http.Request) {
		imageID := chi.URLParam(r, "imageID")
		err := store.DeletePublicImage(r.Context(), imageID)
		deleteImageCache(r.Context(), deps.ImageCache, imageID, err)
		writeImageDeleteResult(w, imageID, err)
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
		attachVirtualMachineNetwork(r.Context(), store, vm, &detail)
		writeJSON(w, http.StatusOK, map[string]any{"data": detail})
	})

	router.Post("/api/v1/projects/{projectID}/vms", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Name      string                  `json:"name"`
			ImageID   string                  `json:"imageId"`
			FlavorID  string                  `json:"flavorId"`
			SubnetID  string                  `json:"subnetId"`
			CloudInit *domain.CloudInitConfig `json:"cloudInit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" || request.ImageID == "" || request.FlavorID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_VM_REQUEST", "name, imageId, and flavorId are required")
			return
		}
		if request.SubnetID != "" && deps.VPCNetworkingUnavailable {
			writeError(w, http.StatusConflict, "VPC_NETWORKING_UNAVAILABLE", "this cluster has no VPC networking; create the VM without a subnet")
			return
		}
		plan, err := store.CreateVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), domain.CreateVirtualMachineInput{
			Name:      request.Name,
			ImageID:   request.ImageID,
			FlavorID:  request.FlavorID,
			SubnetID:  request.SubnetID,
			CloudInit: request.CloudInit,
		})
		if err != nil {
			switch {
			case errors.Is(err, storepkg.ErrNotFound):
				writeError(w, http.StatusBadRequest, "INVALID_SUBNET", "subnetId not found in this project")
			case errors.Is(err, storepkg.ErrImageNotFound):
				writeError(w, http.StatusBadRequest, "INVALID_IMAGE", "imageId not found in this project")
			case errors.Is(err, storepkg.ErrImageNotReady):
				writeError(w, http.StatusConflict, "IMAGE_NOT_READY", "image has not finished importing")
			case errors.Is(err, storepkg.ErrFlavorNotFound):
				writeError(w, http.StatusBadRequest, "INVALID_FLAVOR", "flavorId not found or disabled")
			case errors.Is(err, storepkg.ErrFlavorDiskTooSmall):
				writeError(w, http.StatusBadRequest, "FLAVOR_DISK_TOO_SMALL", "the flavor's root disk is smaller than the image; pick a larger flavor")
			default:
				log.Printf("create virtual machine %q in project %s: %v", request.Name, chi.URLParam(r, "projectID"), err)
				writeError(w, http.StatusInternalServerError, "VM_CREATE_FAILED", "failed to create virtual machine")
			}
			return
		}
		if provisioner != nil {
			if err := provisioner.CreateVirtualMachine(r.Context(), plan); err != nil {
				log.Printf("provision virtual machine %s: %v", plan.VirtualMachine.ID, err)
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

	router.Post("/api/v1/projects/{projectID}/vms/{vmID}/restart", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		status, err := statusReader.GetVirtualMachineStatus(r.Context(), vm)
		if err != nil {
			log.Printf("restart %s: read status: %v", vm.ID, err)
			writeError(w, http.StatusInternalServerError, "VM_STATUS_UNAVAILABLE", "failed to read virtual machine status")
			return
		}
		if status != "Running" {
			writeError(w, http.StatusConflict, "VM_NOT_RUNNING", "only a running virtual machine can be restarted")
			return
		}
		if err := provisioner.RestartVirtualMachine(r.Context(), vm); err != nil {
			if errors.Is(err, kubevirt.ErrVirtualMachineNotRunning) {
				writeError(w, http.StatusConflict, "VM_NOT_RUNNING", "only a running virtual machine can be restarted")
				return
			}
			log.Printf("restart %s: %v", vm.ID, err)
			writeError(w, http.StatusBadGateway, "VM_RESTART_FAILED", "failed to restart virtual machine")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	router.Get("/api/v1/projects/{projectID}/vms/{vmID}/console/log", func(w http.ResponseWriter, r *http.Request) {
		vm, err := store.GetVirtualMachine(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "vmID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "VM_NOT_FOUND", "virtual machine was not found")
			return
		}
		if deps.BootLogReader == nil {
			writeError(w, http.StatusNotImplemented, "BOOT_LOG_NOT_CONFIGURED", "boot logs are not available without KubeVirt")
			return
		}
		bootLog, err := deps.BootLogReader.BootLog(r.Context(), vm.ID)
		if err != nil {
			if errors.Is(err, kubevirt.ErrBootLogUnavailable) {
				writeError(w, http.StatusConflict, "BOOT_LOG_UNAVAILABLE", "the virtual machine is not running")
				return
			}
			log.Printf("boot log %s: %v", vm.ID, err)
			writeError(w, http.StatusBadGateway, "BOOT_LOG_FAILED", "failed to read the boot log")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"log": bootLog}})
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
				"inUse":     consoleStreamer.ConsoleInUse(vm.ID),
				"message":   "KubeVirt console streaming is available while the VM is running",
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
		projectID := chi.URLParam(r, "projectID")
		var request struct {
			Name string `json:"name"`
			CIDR string `json:"cidr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be valid JSON")
			return
		}
		if request.Name == "" || request.CIDR == "" {
			writeError(w, http.StatusBadRequest, "INVALID_NETWORK_REQUEST", "name and cidr are required")
			return
		}
		cidr, err := storepkg.NormalizeNetworkCIDR(request.CIDR)
		if err != nil {
			writeError(w, http.StatusBadRequest, "NETWORK_CIDR_INVALID", "vpc cidr must be a private ipv4 range from /16 to /28")
			return
		}
		if reserved, ok := overlappingReservedCIDR(cidr, deps.ReservedCIDRs); ok {
			writeError(w, http.StatusBadRequest, "NETWORK_CIDR_RESERVED", "vpc cidr overlaps the cluster range "+reserved)
			return
		}
		network, err := store.CreateNetwork(r.Context(), projectID, request.Name, cidr)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NETWORK_CREATE_FAILED", "failed to create network")
			return
		}
		if err := networkProvisioner.CreateNetwork(r.Context(), network); err != nil {
			_, _ = store.DeleteNetwork(r.Context(), projectID, network.ID)
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
			if errors.Is(err, storepkg.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			} else {
				writeError(w, http.StatusInternalServerError, "NETWORK_LOOKUP_FAILED", "failed to look up network")
			}
			return
		}
		unlock, err := locks.lock(r.Context(), network.ID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "NETWORK_BUSY", "the vpc is busy; try again")
			return
		}
		defer unlock()
		if _, err := store.DeleteNetwork(r.Context(), projectID, networkID); err != nil {
			switch {
			case errors.Is(err, storepkg.ErrNotFound):
				writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			case errors.Is(err, storepkg.ErrNetworkInUse):
				writeError(w, http.StatusConflict, "NETWORK_IN_USE", "network still has subnets")
			default:
				writeError(w, http.StatusInternalServerError, "NETWORK_DELETE_FAILED", "failed to delete network")
			}
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
			if errors.Is(err, storepkg.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			} else {
				writeError(w, http.StatusInternalServerError, "NETWORK_LOOKUP_FAILED", "failed to look up network")
			}
			return
		}
		unlock, err := locks.lock(r.Context(), network.ID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "NETWORK_BUSY", "the vpc is busy; try again")
			return
		}
		defer unlock()
		subnet, err := store.CreateSubnet(r.Context(), projectID, networkID, request.Name, request.CIDR, request.Type, request.Gateway)
		if err != nil {
			switch {
			case errors.Is(err, storepkg.ErrNotFound):
				writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			case errors.Is(err, storepkg.ErrSubnetCIDRInvalid):
				writeError(w, http.StatusBadRequest, "SUBNET_CIDR_INVALID", "subnet cidr is invalid")
			case errors.Is(err, storepkg.ErrSubnetCIDROutsideNetwork):
				writeError(w, http.StatusBadRequest, "SUBNET_CIDR_OUTSIDE_VPC", "subnet cidr must be inside the vpc cidr")
			case errors.Is(err, storepkg.ErrSubnetCIDROverlap):
				writeError(w, http.StatusConflict, "SUBNET_CIDR_OVERLAP", "subnet cidr overlaps an existing subnet in this vpc")
			default:
				writeError(w, http.StatusInternalServerError, "SUBNET_CREATE_FAILED", "failed to create subnet")
			}
			return
		}
		publicSubnets, err := publicSubnetsOf(r.Context(), store, subnet, "")
		if err != nil {
			rollbackSubnet(r.Context(), store, subnet)
			writeError(w, http.StatusInternalServerError, "SUBNET_CREATE_FAILED", "failed to create subnet")
			return
		}
		if err := networkProvisioner.CreateSubnet(r.Context(), network.OVNVPCName, subnet, publicSubnets); err != nil {
			log.Printf("provision subnet %s: %v", subnet.ID, err)
			rollbackSubnet(r.Context(), store, subnet)
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
			if errors.Is(err, storepkg.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NETWORK_NOT_FOUND", "network was not found")
			} else {
				writeError(w, http.StatusInternalServerError, "NETWORK_LOOKUP_FAILED", "failed to look up network")
			}
			return
		}
		subnet, err := store.GetSubnet(r.Context(), projectID, subnetID)
		if err != nil {
			if errors.Is(err, storepkg.ErrNotFound) {
				writeError(w, http.StatusNotFound, "SUBNET_NOT_FOUND", "subnet was not found")
			} else {
				writeError(w, http.StatusInternalServerError, "SUBNET_LOOKUP_FAILED", "failed to look up subnet")
			}
			return
		}
		if subnet.NetworkID != networkID {
			writeError(w, http.StatusNotFound, "SUBNET_NOT_FOUND", "subnet was not found")
			return
		}
		// The store rejects deleting the NAT gateway anchor while other public subnets remain, so the
		// gateway goes only with the VPC's last public subnet.
		unlock, err := locks.lock(r.Context(), network.ID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "NETWORK_BUSY", "the vpc is busy; try again")
			return
		}
		defer unlock()
		remainingPublicSubnets, err := publicSubnetsOf(r.Context(), store, subnet, subnet.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SUBNET_DELETE_FAILED", "failed to delete subnet")
			return
		}
		if _, err := store.DeleteSubnet(r.Context(), projectID, subnetID); err != nil {
			switch {
			case errors.Is(err, storepkg.ErrNotFound):
				writeError(w, http.StatusNotFound, "SUBNET_NOT_FOUND", "subnet was not found")
			case errors.Is(err, storepkg.ErrSubnetInUse):
				writeError(w, http.StatusConflict, "SUBNET_IN_USE", "subnet still has virtual machines attached")
			case errors.Is(err, storepkg.ErrSubnetHostsNATGateway):
				writeError(w, http.StatusConflict, "SUBNET_HOSTS_NAT_GATEWAY", "subnet hosts the vpc nat gateway; delete the other public subnets first")
			default:
				writeError(w, http.StatusInternalServerError, "SUBNET_DELETE_FAILED", "failed to delete subnet")
			}
			return
		}
		if err := networkProvisioner.DeleteSubnet(r.Context(), network.OVNVPCName, subnet, remainingPublicSubnets); err != nil {
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

// attachVirtualMachineNetwork fills in the subnet and VPC a VM is attached to. A lookup failure leaves them
// empty instead of failing the detail view, since the runtime detail is still useful on its own.
func attachVirtualMachineNetwork(ctx context.Context, store Store, vm domain.VirtualMachine, detail *domain.VirtualMachineDetail) {
	if vm.SubnetID == "" {
		return
	}
	subnet, err := store.GetSubnet(ctx, vm.ProjectID, vm.SubnetID)
	if err != nil {
		log.Printf("vm %s: read subnet %s: %v", vm.ID, vm.SubnetID, err)
		return
	}
	detail.Subnet = &subnet
	network, err := store.GetNetwork(ctx, vm.ProjectID, subnet.NetworkID)
	if err != nil {
		log.Printf("vm %s: read network %s: %v", vm.ID, subnet.NetworkID, err)
		return
	}
	detail.Network = &network
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
