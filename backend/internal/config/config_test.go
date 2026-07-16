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
