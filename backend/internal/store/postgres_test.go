package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

func TestNewPostgresRejectsEmptyDatabaseURL(t *testing.T) {
	_, err := NewPostgres("")
	if err == nil {
		t.Fatal("expected error for empty database URL")
	}
}

func TestPostgresAppliesSchemaAndSeedsCatalog(t *testing.T) {
	databaseURL := os.Getenv("KLOUDIA_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KLOUDIA_STORE_TEST_DATABASE_URL is required for store integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("expected seeded projects")
	}
	var platformFound bool
	for _, project := range projects {
		if project.ID == "platform" {
			platformFound = true
			break
		}
	}
	if !platformFound {
		t.Fatalf("expected platform project in seed data, got %#v", projects)
	}
	flavors, err := db.ListFlavors(ctx)
	if err != nil {
		t.Fatalf("list flavors: %v", err)
	}
	if len(flavors) < 3 {
		t.Fatalf("expected at least three seeded flavors, got %d", len(flavors))
	}
	var small domain.Flavor
	for _, flavor := range flavors {
		if flavor.ID == "small" {
			small = flavor
			break
		}
	}
	if small.ID == "" {
		t.Fatalf("expected small flavor in seed data, got %#v", flavors)
	}
	if small.CPU != 1 || small.MemoryBytes != 512*1024*1024 || small.RootDiskBytes != 2*1024*1024*1024 {
		t.Fatalf("unexpected small flavor sizing: %#v", small)
	}
	images, err := db.ListImages(ctx, "platform")
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("expected seeded images for platform project")
	}
}

func TestPostgresCreatesVirtualMachineProvisioningPlan(t *testing.T) {
	databaseURL := os.Getenv("KLOUDIA_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KLOUDIA_STORE_TEST_DATABASE_URL is required for store integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	defer db.Close()
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	plan, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{
		Name:     "web-01",
		ImageID:  "platform-cirros",
		FlavorID: "small",
	})
	if err != nil {
		t.Fatalf("create virtual machine: %v", err)
	}
	if plan.VirtualMachine.Status != "Provisioning" {
		t.Fatalf("expected Provisioning status, got %q", plan.VirtualMachine.Status)
	}
	if plan.Image.ID != "platform-cirros" {
		t.Fatalf("expected platform-cirros image, got %q", plan.Image.ID)
	}
	if plan.Flavor.ID != "small" {
		t.Fatalf("expected small flavor, got %q", plan.Flavor.ID)
	}
	vms, err := db.ListVirtualMachines(ctx, "platform")
	if err != nil {
		t.Fatalf("list virtual machines: %v", err)
	}
	var found bool
	for _, vm := range vms {
		if vm.Name == "web-01" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected web-01 in VM list, got %#v", vms)
	}
}

func TestPostgresDeletesVirtualMachineRecord(t *testing.T) {
	databaseURL := os.Getenv("KLOUDIA_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KLOUDIA_STORE_TEST_DATABASE_URL is required for store integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	defer db.Close()
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	plan, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{
		Name:     "delete-me",
		ImageID:  "platform-cirros",
		FlavorID: "small",
	})
	if err != nil {
		t.Fatalf("create virtual machine: %v", err)
	}
	vm, err := db.GetVirtualMachine(ctx, "platform", plan.VirtualMachine.ID)
	if err != nil {
		t.Fatalf("get virtual machine: %v", err)
	}
	if vm.ID != plan.VirtualMachine.ID {
		t.Fatalf("expected vm %q, got %q", plan.VirtualMachine.ID, vm.ID)
	}
	if err := db.DeleteVirtualMachine(ctx, "platform", plan.VirtualMachine.ID); err != nil {
		t.Fatalf("delete virtual machine: %v", err)
	}
	_, err = db.GetVirtualMachine(ctx, "platform", plan.VirtualMachine.ID)
	if err == nil {
		t.Fatal("expected get deleted virtual machine to fail")
	}
}

func TestPostgresRejectsDeletingNATGatewayAnchorWhileOtherPublicSubnetsRemain(t *testing.T) {
	databaseURL := os.Getenv("KLOUDIA_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KLOUDIA_STORE_TEST_DATABASE_URL is required for store integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	defer db.Close()
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	network, err := db.CreateNetwork(ctx, "platform", "natgw-anchor-"+time.Now().Format("150405.000000"), "10.0.0.0/16")
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	anchor, err := db.CreateSubnet(ctx, "platform", network.ID, "a", "10.0.0.0/24", "public", "")
	if err != nil {
		t.Fatalf("create anchor subnet: %v", err)
	}
	other, err := db.CreateSubnet(ctx, "platform", network.ID, "b", "10.0.1.0/24", "public", "")
	if err != nil {
		t.Fatalf("create second public subnet: %v", err)
	}

	if _, err := db.DeleteSubnet(ctx, "platform", anchor.ID); !errors.Is(err, ErrSubnetHostsNATGateway) {
		t.Fatalf("expected ErrSubnetHostsNATGateway, got %v", err)
	}
	if _, err := db.DeleteSubnet(ctx, "platform", other.ID); err != nil {
		t.Fatalf("delete non-anchor subnet: %v", err)
	}
	if _, err := db.DeleteSubnet(ctx, "platform", anchor.ID); err != nil {
		t.Fatalf("delete last public subnet: %v", err)
	}
	if _, err := db.DeleteNetwork(ctx, "platform", network.ID); err != nil {
		t.Fatalf("delete network: %v", err)
	}
}

func TestPostgresKeepsSubnetsInsideVPCCIDR(t *testing.T) {
	databaseURL := os.Getenv("KLOUDIA_STORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("KLOUDIA_STORE_TEST_DATABASE_URL is required for store integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := NewPostgres(databaseURL)
	if err != nil {
		t.Fatalf("new postgres: %v", err)
	}
	defer db.Close()
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	if _, err := db.CreateNetwork(ctx, "platform", "public-range", "8.8.0.0/16"); !errors.Is(err, ErrNetworkCIDRInvalid) {
		t.Fatalf("expected ErrNetworkCIDRInvalid for a public range, got %v", err)
	}
	network, err := db.CreateNetwork(ctx, "platform", "vpc-cidr-"+time.Now().Format("150405.000000"), "10.30.7.0/16")
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	if network.CIDR != "10.30.0.0/16" {
		t.Fatalf("expected masked vpc cidr 10.30.0.0/16, got %q", network.CIDR)
	}
	got, err := db.GetNetwork(ctx, "platform", network.ID)
	if err != nil || got.CIDR != "10.30.0.0/16" {
		t.Fatalf("expected stored vpc cidr, got %q (err %v)", got.CIDR, err)
	}

	if _, err := db.CreateSubnet(ctx, "platform", network.ID, "outside", "10.31.0.0/24", "private", ""); !errors.Is(err, ErrSubnetCIDROutsideNetwork) {
		t.Fatalf("expected ErrSubnetCIDROutsideNetwork, got %v", err)
	}
	inside, err := db.CreateSubnet(ctx, "platform", network.ID, "inside", "10.30.1.0/24", "private", "")
	if err != nil {
		t.Fatalf("create subnet inside vpc: %v", err)
	}
	if _, err := db.DeleteSubnet(ctx, "platform", inside.ID); err != nil {
		t.Fatalf("delete subnet: %v", err)
	}
	if _, err := db.DeleteNetwork(ctx, "platform", network.ID); err != nil {
		t.Fatalf("delete network: %v", err)
	}
}
