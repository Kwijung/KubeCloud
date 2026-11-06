package config

import "testing"

func TestLoadDisablesNetworkProvisioningByDefault(t *testing.T) {
	t.Setenv("KLOUDIA_NETWORK_PROVISIONING_ENABLED", "")

	cfg := Load()

	if cfg.NetworkProvisioningEnabled {
		t.Fatalf("expected network provisioning to be disabled by default")
	}
}

func TestLoadEnablesNetworkProvisioningFromEnv(t *testing.T) {
	t.Setenv("KLOUDIA_NETWORK_PROVISIONING_ENABLED", "true")

	cfg := Load()

	if !cfg.NetworkProvisioningEnabled {
		t.Fatalf("expected network provisioning to be enabled from env")
	}
}

func TestReservedPrefixesDefaultsToClusterRanges(t *testing.T) {
	t.Setenv("KLOUDIA_RESERVED_CIDRS", "")

	prefixes, err := Load().ReservedPrefixes()

	if err != nil || len(prefixes) != 4 || prefixes[0].String() != "10.244.0.0/16" {
		t.Fatalf("unexpected default reserved prefixes %v (err %v)", prefixes, err)
	}
}

func TestReservedPrefixesParsesEnvAndRejectsGarbage(t *testing.T) {
	t.Setenv("KLOUDIA_RESERVED_CIDRS", " 10.0.1.5/24 , 172.31.0.0/16,")
	prefixes, err := Load().ReservedPrefixes()
	if err != nil || len(prefixes) != 2 || prefixes[0].String() != "10.0.1.0/24" {
		t.Fatalf("unexpected reserved prefixes %v (err %v)", prefixes, err)
	}

	t.Setenv("KLOUDIA_RESERVED_CIDRS", "10.0.0.0/8,nope")
	if _, err := Load().ReservedPrefixes(); err == nil {
		t.Fatal("expected an error for an invalid reserved cidr")
	}
}

func TestLoadParsesVMDNSServers(t *testing.T) {
	t.Setenv("KLOUDIA_VM_DNS_SERVERS", " 10.0.0.2 , 8.8.8.8,")
	cfg := Load()
	servers, err := cfg.VMDNSServerList()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(servers) != 2 || servers[0] != "10.0.0.2" || servers[1] != "8.8.8.8" {
		t.Fatalf("unexpected servers: %#v", servers)
	}
	t.Setenv("KLOUDIA_VM_DNS_SERVERS", "dns.example.com")
	if _, err := Load().VMDNSServerList(); err == nil {
		t.Fatalf("expected error for a non-IP DNS server")
	}
}

func TestLoadValidatesDiskAccessMode(t *testing.T) {
	if mode, err := Load().DiskAccessMode(); err != nil || mode != "ReadWriteOnce" {
		t.Fatalf("expected the ReadWriteOnce default, got %q (%v)", mode, err)
	}
	t.Setenv("KLOUDIA_KUBEVIRT_DISK_ACCESS_MODE", "ReadWriteMany")
	if mode, err := Load().DiskAccessMode(); err != nil || mode != "ReadWriteMany" {
		t.Fatalf("expected ReadWriteMany, got %q (%v)", mode, err)
	}
	t.Setenv("KLOUDIA_KUBEVIRT_DISK_ACCESS_MODE", "ReadOnlyMany")
	if _, err := Load().DiskAccessMode(); err == nil {
		t.Fatal("a VM root disk cannot be ReadOnlyMany")
	}
}
