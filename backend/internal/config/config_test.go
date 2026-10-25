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
