package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/kloudia/kloudia/backend/internal/config"
	"github.com/kloudia/kloudia/backend/internal/httpapi"
	"github.com/kloudia/kloudia/backend/internal/kubevirt"
	"github.com/kloudia/kloudia/backend/internal/store"
)

func main() {
	cfg := config.Load()
	db, err := store.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database setup failed: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.ApplySchema(ctx); err != nil {
		log.Fatalf("database schema setup failed: %v", err)
	}

	handler := httpapi.NewRouter(cfg.Version, db)
	if cfg.KubeVirtEnabled {
		applier, err := kubevirt.NewDynamicApplier(cfg.KubeconfigPath)
		if err != nil {
			log.Fatalf("kubevirt applier setup failed: %v", err)
		}
		statusReader, err := kubevirt.NewDynamicStatusReader(cfg.KubeVirtNamespace, cfg.KubeVirtStorageClassName, cfg.KubeconfigPath)
		if err != nil {
			log.Fatalf("kubevirt status reader setup failed: %v", err)
		}
		consoleStreamer, err := kubevirt.NewConsoleStreamer(cfg.KubeVirtNamespace, cfg.KubeconfigPath)
		if err != nil {
			log.Fatalf("kubevirt console streamer setup failed: %v", err)
		}
		provisioner := kubevirt.NewProvisioner(cfg.KubeVirtNamespace, cfg.KubeVirtStorageClassName, applier)
		deps := httpapi.Dependencies{
			Version:         cfg.Version,
			Store:           db,
			Provisioner:     provisioner,
			StatusReader:    statusReader,
			ConsoleStreamer: consoleStreamer,
		}
		if cfg.NetworkProvisioningEnabled {
			deps.NetworkProvisioner = kubevirt.NewNetworkProvisioner(cfg.KubeVirtNamespace, cfg.OVNExternalNetwork, applier)
			log.Printf("network provisioning enabled: externalNetwork=%s", cfg.OVNExternalNetwork)
		} else {
			log.Printf("network provisioning disabled: VPC/subnet records are stored without kube-ovn resources")
		}
		handler = httpapi.NewRouterWithDependencies(deps)
		log.Printf("kubevirt provisioning enabled: namespace=%s storageClass=%s", cfg.KubeVirtNamespace, cfg.KubeVirtStorageClassName)
	}

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}

	log.Printf("kloudia api listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("api server failed: %v", err)
	}
}
