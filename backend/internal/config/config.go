package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

type Config struct {
	Addr                       string
	Version                    string
	DatabaseURL                string
	CORSOrigin                 string
	KubeVirtEnabled            bool
	KubeconfigPath             string
	KubeVirtNamespace          string
	KubeVirtStorageClassName   string
	NetworkProvisioningEnabled bool
	OVNExternalNetwork         string
	// ReservedCIDRs is a comma-separated list of ranges VPC CIDRs must not overlap: the cluster's pod,
	// service and kube-ovn internal ranges, plus the node network when NAT egress uses it.
	ReservedCIDRs string
	// VMDNSServers is a comma-separated list of DNS server IPs for VMs attached to a VPC subnet.
	VMDNSServers string
	// KubeVirtDiskAccessMode is the access mode of VM root disks: ReadWriteOnce or ReadWriteMany.
	KubeVirtDiskAccessMode string
}

func Load() Config {
	return Config{
		Addr:                       env("KLOUDIA_API_ADDR", ":8080"),
		Version:                    env("KLOUDIA_VERSION", "dev"),
		DatabaseURL:                env("DATABASE_URL", "postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable"),
		CORSOrigin:                 env("KLOUDIA_CORS_ORIGIN", "http://localhost:5173"),
		KubeVirtEnabled:            env("KLOUDIA_KUBEVIRT_ENABLED", "false") == "true",
		KubeconfigPath:             env("KUBECONFIG", ""),
		KubeVirtNamespace:          env("KLOUDIA_KUBEVIRT_NAMESPACE", "kloudia-vms"),
		KubeVirtStorageClassName:   env("KLOUDIA_KUBEVIRT_STORAGE_CLASS", "test-delete"),
		NetworkProvisioningEnabled: env("KLOUDIA_NETWORK_PROVISIONING_ENABLED", "false") == "true",
		OVNExternalNetwork:         env("KLOUDIA_OVN_EXTERNAL_NETWORK", "ovn-vpc-external-network"),
		// Defaults: Cilium pod CIDR, Kubernetes service CIDR, kube-ovn default and join subnets.
		VMDNSServers:           env("KLOUDIA_VM_DNS_SERVERS", ""),
		KubeVirtDiskAccessMode: env("KLOUDIA_KUBEVIRT_DISK_ACCESS_MODE", "ReadWriteOnce"),
		ReservedCIDRs:          env("KLOUDIA_RESERVED_CIDRS", "10.244.0.0/16,10.96.0.0/12,10.16.0.0/16,100.64.0.0/16"),
	}
}

// ReservedPrefixes parses ReservedCIDRs.
func (c Config) ReservedPrefixes() ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, raw := range strings.Split(c.ReservedCIDRs, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("KLOUDIA_RESERVED_CIDRS: invalid cidr %q: %w", raw, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// DiskAccessMode validates KubeVirtDiskAccessMode. ReadWriteMany needs a storage class that supports it,
// such as NFS, and makes VMs live-migratable.
func (c Config) DiskAccessMode() (string, error) {
	switch c.KubeVirtDiskAccessMode {
	case "ReadWriteOnce", "ReadWriteMany":
		return c.KubeVirtDiskAccessMode, nil
	default:
		return "", fmt.Errorf("KLOUDIA_KUBEVIRT_DISK_ACCESS_MODE: %q must be ReadWriteOnce or ReadWriteMany", c.KubeVirtDiskAccessMode)
	}
}

// VMDNSServerList parses VMDNSServers.
func (c Config) VMDNSServerList() ([]string, error) {
	var servers []string
	for _, raw := range strings.Split(c.VMDNSServers, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, fmt.Errorf("KLOUDIA_VM_DNS_SERVERS: invalid ip %q: %w", raw, err)
		}
		servers = append(servers, addr.String())
	}
	return servers, nil
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
