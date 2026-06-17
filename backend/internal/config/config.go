package config

import "os"

type Config struct {
	Addr                     string
	Version                  string
	DatabaseURL              string
	CORSOrigin               string
	KubeVirtEnabled          bool
	KubeconfigPath           string
	KubeVirtNamespace        string
	KubeVirtStorageClassName string
	OVNExternalNetwork       string
}

func Load() Config {
	return Config{
		Addr:                     env("KLOUDIA_API_ADDR", ":8080"),
		Version:                  env("KLOUDIA_VERSION", "dev"),
		DatabaseURL:              env("DATABASE_URL", "postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable"),
		CORSOrigin:               env("KLOUDIA_CORS_ORIGIN", "http://localhost:5173"),
		KubeVirtEnabled:          env("KLOUDIA_KUBEVIRT_ENABLED", "false") == "true",
		KubeconfigPath:           env("KUBECONFIG", ""),
		KubeVirtNamespace:        env("KLOUDIA_KUBEVIRT_NAMESPACE", "kloudia-vms"),
		KubeVirtStorageClassName: env("KLOUDIA_KUBEVIRT_STORAGE_CLASS", "test-delete"),
		OVNExternalNetwork:       env("KLOUDIA_OVN_EXTERNAL_NETWORK", "ovn-vpc-external-network"),
	}
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
