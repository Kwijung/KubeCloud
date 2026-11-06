package domain

import "time"

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Flavor struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CPU           int    `json:"cpu"`
	MemoryBytes   int64  `json:"memoryBytes"`
	RootDiskBytes int64  `json:"rootDiskBytes"`
	Enabled       bool   `json:"enabled"`
	// VMCount is the number of VMs using the flavor; only the admin flavor list sets it.
	VMCount *int `json:"vmCount,omitempty"`
}

// FlavorInput defines a new flavor.
type FlavorInput struct {
	Name          string `json:"name"`
	CPU           int    `json:"cpu"`
	MemoryBytes   int64  `json:"memoryBytes"`
	RootDiskBytes int64  `json:"rootDiskBytes"`
}

// FlavorUpdate changes the fields that are set. CPU, memory and disk can only change while no VM uses the
// flavor; the name and whether new VMs may use it can always change.
type FlavorUpdate struct {
	Name          *string `json:"name"`
	CPU           *int    `json:"cpu"`
	MemoryBytes   *int64  `json:"memoryBytes"`
	RootDiskBytes *int64  `json:"rootDiskBytes"`
	Enabled       *bool   `json:"enabled"`
}

type Image struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	OSType    string    `json:"osType"`
	SourceURL string    `json:"sourceUrl"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	// VirtualSizeBytes is the disk size the image expands to; nil when it could not be read (non-qcow2).
	VirtualSizeBytes *int64 `json:"virtualSizeBytes,omitempty"`
	// MinRootDiskBytes is the smallest flavor root disk that holds the image; nil when the size is unknown.
	MinRootDiskBytes *int64 `json:"minRootDiskBytes,omitempty"`
	// Scope is "public" (registered by an admin, readable by every project; ProjectID is empty) or "project".
	Scope string `json:"scope"`
	// VMCount is the number of VMs using the image across all projects; only the admin image list sets it.
	VMCount *int `json:"vmCount,omitempty"`
}

// CreateImageInput registers a cloud image by URL. CDI downloads it when a VM using the image is created.
type CreateImageInput struct {
	Name             string
	OSType           string
	SourceURL        string
	VirtualSizeBytes *int64
}

type Network struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	Name       string    `json:"name"`
	CIDR       string    `json:"cidr"` // empty for VPCs created before VPC CIDRs existed
	OVNVPCName string    `json:"-"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Subnet struct {
	ID            string    `json:"id"`
	NetworkID     string    `json:"networkId"`
	ProjectID     string    `json:"projectId"`
	Name          string    `json:"name"`
	CIDR          string    `json:"cidr"`
	Gateway       string    `json:"gateway"`
	Type          string    `json:"type"` // private | public
	OVNSubnetName string    `json:"-"`
	NADName       string    `json:"-"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
}

// NATGatewayAnchor returns the public subnet that hosts its VPC's shared NAT gateway: the earliest
// created public subnet, ties broken by ID. The anchor cannot be deleted while other public subnets
// remain, so it stays the earliest one for the gateway's whole lifetime.
func NATGatewayAnchor(subnets []Subnet) (Subnet, bool) {
	var anchor Subnet
	found := false
	for _, s := range subnets {
		if s.Type != "public" {
			continue
		}
		if !found || s.CreatedAt.Before(anchor.CreatedAt) || (s.CreatedAt.Equal(anchor.CreatedAt) && s.ID < anchor.ID) {
			anchor = s
			found = true
		}
	}
	return anchor, found
}

type VirtualMachine struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	ImageID   string    `json:"imageId"`
	FlavorID  string    `json:"flavorId"`
	SubnetID  string    `json:"subnetId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateVirtualMachineInput struct {
	Name      string
	ImageID   string
	FlavorID  string
	SubnetID  string
	CloudInit *CloudInitConfig
}

type CloudInitConfig struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	SSHPublicKey string `json:"sshPublicKey"`
}

type VirtualMachineProvisioningPlan struct {
	VirtualMachine VirtualMachine
	Image          Image
	Flavor         Flavor
	Subnet         *Subnet
	CloudInit      *CloudInitConfig
}

type VirtualMachineDetail struct {
	VirtualMachine   VirtualMachine `json:"virtualMachine"`
	RuntimeStatus    string         `json:"runtimeStatus"`
	FailureReason    string         `json:"failureReason"`
	FailureMessage   string         `json:"failureMessage"`
	VMIPhase         string         `json:"vmiPhase"`
	VMIIP            string         `json:"vmiIp"`
	NodeName         string         `json:"nodeName"`
	DataVolumePhase  string         `json:"dataVolumePhase"`
	DataVolumeStatus string         `json:"dataVolumeStatus"`
	PVCPhase         string         `json:"pvcPhase"`
	PVCCapacity      string         `json:"pvcCapacity"`
	ConsoleAvailable bool           `json:"consoleAvailable"`
	Events           []RuntimeEvent `json:"events"`
	// Subnet and Network describe where the VM is attached; both are nil for a VM on the pod network.
	Subnet  *Subnet  `json:"subnet,omitempty"`
	Network *Network `json:"network,omitempty"`
}

type RuntimeEvent struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type PreflightCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type PreflightResult struct {
	Status string           `json:"status"`
	Checks []PreflightCheck `json:"checks"`
}
