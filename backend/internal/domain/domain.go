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
}

type Image struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	OSType    string    `json:"osType"`
	SourceURL string    `json:"sourceUrl"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type Network struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	Name       string    `json:"name"`
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
