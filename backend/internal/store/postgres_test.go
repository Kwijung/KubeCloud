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

func TestPostgresCreateVirtualMachineReturnsCatalogSentinels(t *testing.T) {
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

	// No registered image is ever left Importing, so the test adds one to check the rule.
	if _, err := db.pool.Exec(ctx, `
INSERT INTO images (id, project_id, name, os_type, source_url, status, scope)
VALUES ('data-importing-fixture', 'data', 'Importing fixture', 'linux', 'https://example.com/x.img', 'Importing', 'project')
ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("insert importing fixture: %v", err)
	}
	defer func() { _, _ = db.pool.Exec(ctx, `DELETE FROM images WHERE id = 'data-importing-fixture'`) }()

	cases := []struct {
		name    string
		project string
		input   domain.CreateVirtualMachineInput
		want    error
	}{
		{"unknown image", "platform", domain.CreateVirtualMachineInput{Name: "x-1", ImageID: "nope", FlavorID: "small"}, ErrImageNotFound},
		{"image of another project", "platform", domain.CreateVirtualMachineInput{Name: "x-1", ImageID: "data-ubuntu-22", FlavorID: "small"}, ErrImageNotFound},
		{"importing image", "data", domain.CreateVirtualMachineInput{Name: "x-1", ImageID: "data-importing-fixture", FlavorID: "medium"}, ErrImageNotReady},
		{"unknown flavor", "platform", domain.CreateVirtualMachineInput{Name: "x-1", ImageID: "platform-cirros", FlavorID: "nope"}, ErrFlavorNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.CreateVirtualMachine(ctx, tc.project, tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestPostgresCreatesAndDeletesImages(t *testing.T) {
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

	input := domain.CreateImageInput{Name: "Debian 12", OSType: "linux", SourceURL: "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"}
	image, err := db.CreateImage(ctx, "platform", input)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if image.ID != "platform-debian-12" || image.Status != "Ready" || image.Name != "Debian 12" {
		t.Fatalf("unexpected image: %#v", image)
	}
	if _, err := db.CreateImage(ctx, "platform", input); !errors.Is(err, ErrImageNameTaken) {
		t.Fatalf("expected ErrImageNameTaken on duplicate, got %v", err)
	}
	if _, err := db.CreateImage(ctx, "platform", domain.CreateImageInput{Name: "bad", OSType: "linux", SourceURL: "file:///etc/passwd"}); !errors.Is(err, ErrImageSourceURLInvalid) {
		t.Fatalf("expected ErrImageSourceURLInvalid, got %v", err)
	}
	if _, err := db.CreateImage(ctx, "platform", domain.CreateImageInput{Name: "bad", OSType: "beos", SourceURL: "https://example.com/x.img"}); !errors.Is(err, ErrImageOSTypeInvalid) {
		t.Fatalf("expected ErrImageOSTypeInvalid, got %v", err)
	}

	if _, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{Name: "img-user", ImageID: image.ID, FlavorID: "medium"}); err != nil {
		t.Fatalf("create vm on new image: %v", err)
	}
	if err := db.DeleteImage(ctx, "platform", image.ID); !errors.Is(err, ErrImageInUse) {
		t.Fatalf("expected ErrImageInUse, got %v", err)
	}
	if err := db.DeleteVirtualMachine(ctx, "platform", "platform-img-user"); err != nil {
		t.Fatalf("delete vm: %v", err)
	}
	if err := db.DeleteImage(ctx, "platform", image.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if err := db.DeleteImage(ctx, "platform", image.ID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("expected ErrImageNotFound after delete, got %v", err)
	}
	if err := db.DeleteImage(ctx, "data", "platform-cirros"); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("expected ErrImageNotFound for another project's image, got %v", err)
	}
}

func TestPostgresRejectsFlavorTooSmallForImage(t *testing.T) {
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

	size := int64(3758096384)
	image, err := db.CreateImage(ctx, "platform", domain.CreateImageInput{Name: "Noble Sized", OSType: "linux", SourceURL: "https://example.com/noble.img", VirtualSizeBytes: &size})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if image.VirtualSizeBytes == nil || *image.VirtualSizeBytes != size || image.MinRootDiskBytes == nil {
		t.Fatalf("expected virtual and minimum root disk sizes, got %#v", image)
	}
	defer func() { _ = db.DeleteImage(ctx, "platform", image.ID) }()

	if _, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{Name: "too-small", ImageID: image.ID, FlavorID: "small"}); !errors.Is(err, ErrFlavorDiskTooSmall) {
		t.Fatalf("expected ErrFlavorDiskTooSmall, got %v", err)
	}
	if _, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{Name: "fits", ImageID: image.ID, FlavorID: "medium"}); err != nil {
		t.Fatalf("medium should fit: %v", err)
	}
	if err := db.DeleteVirtualMachine(ctx, "platform", "platform-fits"); err != nil {
		t.Fatalf("delete vm: %v", err)
	}

	unsized, err := db.CreateImage(ctx, "platform", domain.CreateImageInput{Name: "Unsized", OSType: "linux", SourceURL: "https://example.com/raw.img"})
	if err != nil {
		t.Fatalf("create unsized image: %v", err)
	}
	defer func() { _ = db.DeleteImage(ctx, "platform", unsized.ID) }()
	if err := db.SetImageVirtualSize(ctx, unsized.ID, size); err != nil {
		t.Fatalf("set virtual size: %v", err)
	}
	missing, err := db.ListImagesWithoutVirtualSize(ctx)
	if err != nil {
		t.Fatalf("list images without size: %v", err)
	}
	for _, m := range missing {
		if m.ID == unsized.ID {
			t.Fatalf("image %s should have a size now", unsized.ID)
		}
	}
}

func TestPostgresPublicImages(t *testing.T) {
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
	for i := 0; i < 2; i++ { // the schema must apply cleanly on an existing database too
		if err := db.ApplySchema(ctx); err != nil {
			t.Fatalf("apply schema (run %d): %v", i+1, err)
		}
	}

	findImage := func(images []domain.Image, id string) *domain.Image {
		for i := range images {
			if images[i].ID == id {
				return &images[i]
			}
		}
		return nil
	}

	// The seeded CirrOS image is public and keeps its ID, so existing VMs still reference it.
	dataImages, err := db.ListImages(ctx, "data")
	if err != nil {
		t.Fatalf("list data images: %v", err)
	}
	cirros := findImage(dataImages, "platform-cirros")
	if cirros == nil || cirros.Scope != "public" || cirros.ProjectID != "" {
		t.Fatalf("expected public platform-cirros in data's list, got %#v", cirros)
	}
	if own := findImage(dataImages, "data-ubuntu-22"); own == nil || own.Scope != "project" {
		t.Fatalf("expected data's own project image, got %#v", own)
	}
	platformImages, err := db.ListImages(ctx, "platform")
	if err != nil {
		t.Fatalf("list platform images: %v", err)
	}
	if findImage(platformImages, "data-ubuntu-22") != nil {
		t.Fatal("platform must not see data's project image")
	}

	public, err := db.CreatePublicImage(ctx, domain.CreateImageInput{Name: "Shared Debian", OSType: "linux", SourceURL: "https://example.com/debian.qcow2"})
	if err != nil {
		t.Fatalf("create public image: %v", err)
	}
	if public.ID != "public-shared-debian" || public.Scope != "public" || public.ProjectID != "" {
		t.Fatalf("unexpected public image: %#v", public)
	}
	if _, err := db.CreatePublicImage(ctx, domain.CreateImageInput{Name: "shared debian", OSType: "linux", SourceURL: "https://example.com/x.qcow2"}); !errors.Is(err, ErrImageNameTaken) {
		t.Fatalf("expected ErrImageNameTaken for a duplicate public name, got %v", err)
	}
	projectCopy, err := db.CreateImage(ctx, "platform", domain.CreateImageInput{Name: "Shared Debian", OSType: "linux", SourceURL: "https://example.com/debian.qcow2"})
	if err != nil {
		t.Fatalf("a project image may reuse a public image's name: %v", err)
	}
	defer func() { _ = db.DeleteImage(ctx, "platform", projectCopy.ID) }()

	if _, err := db.CreateVirtualMachine(ctx, "data", domain.CreateVirtualMachineInput{Name: "pub-img-vm", ImageID: public.ID, FlavorID: "small"}); err != nil {
		t.Fatalf("any project may use a public image: %v", err)
	}
	if _, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{Name: "x-1", ImageID: "data-ubuntu-22", FlavorID: "medium"}); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("another project's image must stay hidden, got %v", err)
	}

	if err := db.DeleteImage(ctx, "data", public.ID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("a project route must not delete a public image, got %v", err)
	}
	publicImages, err := db.ListPublicImages(ctx)
	if err != nil {
		t.Fatalf("list public images: %v", err)
	}
	listed := findImage(publicImages, public.ID)
	if listed == nil || listed.VMCount == nil || *listed.VMCount != 1 {
		t.Fatalf("expected the public image with 1 VM, got %#v", listed)
	}
	if findImage(publicImages, "data-ubuntu-22") != nil {
		t.Fatal("public list must not include project images")
	}
	if err := db.DeletePublicImage(ctx, public.ID); !errors.Is(err, ErrImageInUse) {
		t.Fatalf("expected ErrImageInUse, got %v", err)
	}
	if err := db.DeleteVirtualMachine(ctx, "data", "data-pub-img-vm"); err != nil {
		t.Fatalf("delete vm: %v", err)
	}
	if err := db.DeletePublicImage(ctx, public.ID); err != nil {
		t.Fatalf("delete public image: %v", err)
	}
	if err := db.DeletePublicImage(ctx, public.ID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("expected ErrImageNotFound after delete, got %v", err)
	}
	if err := db.DeletePublicImage(ctx, "data-ubuntu-22"); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("the admin route must not delete a project image, got %v", err)
	}
}

func TestPostgresSeedImagesAreReady(t *testing.T) {
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
	// Simulate a database seeded before the fix: the Ubuntu seed image stuck in Importing.
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE images SET status = 'Importing' WHERE id = 'data-ubuntu-22'`); err != nil {
		t.Fatalf("simulate old seed: %v", err)
	}
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("re-apply schema: %v", err)
	}
	images, err := db.ListImages(ctx, "data")
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	found := false
	for _, image := range images {
		if image.ID == "data-ubuntu-22" {
			found = true
			if image.Status != "Ready" {
				t.Fatalf("seeded Ubuntu image must be Ready, got %q", image.Status)
			}
		}
	}
	if !found {
		t.Fatal("seeded Ubuntu image data-ubuntu-22 is missing")
	}
}

func TestPostgresFlavorAdmin(t *testing.T) {
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
	const gib = int64(1) << 30

	flavor, err := db.CreateFlavor(ctx, domain.FlavorInput{Name: "XLarge Test", CPU: 8, MemoryBytes: 8 * gib, RootDiskBytes: 40 * gib})
	if err != nil {
		t.Fatalf("create flavor: %v", err)
	}
	defer func() { _ = db.DeleteFlavor(ctx, flavor.ID) }()
	if flavor.ID != "xlarge-test" || !flavor.Enabled {
		t.Fatalf("unexpected flavor: %#v", flavor)
	}
	if _, err := db.CreateFlavor(ctx, domain.FlavorInput{Name: "xlarge test", CPU: 1, MemoryBytes: gib, RootDiskBytes: gib}); !errors.Is(err, ErrFlavorNameTaken) {
		t.Fatalf("expected ErrFlavorNameTaken, got %v", err)
	}
	if _, err := db.CreateFlavor(ctx, domain.FlavorInput{Name: "bad", CPU: 0, MemoryBytes: gib, RootDiskBytes: gib}); !errors.Is(err, ErrFlavorInvalid) {
		t.Fatalf("expected ErrFlavorInvalid for 0 vCPU, got %v", err)
	}

	if _, err := db.CreateVirtualMachine(ctx, "platform", domain.CreateVirtualMachineInput{Name: "flavor-user", ImageID: "platform-cirros", FlavorID: flavor.ID}); err != nil {
		t.Fatalf("create vm: %v", err)
	}
	all, err := db.ListAllFlavors(ctx)
	if err != nil {
		t.Fatalf("list all flavors: %v", err)
	}
	var listed *domain.Flavor
	for i := range all {
		if all[i].ID == flavor.ID {
			listed = &all[i]
		}
	}
	if listed == nil || listed.VMCount == nil || *listed.VMCount != 1 {
		t.Fatalf("expected the flavor with 1 VM, got %#v", listed)
	}

	cpu := 16
	if _, err := db.UpdateFlavor(ctx, flavor.ID, domain.FlavorUpdate{CPU: &cpu}); !errors.Is(err, ErrFlavorInUse) {
		t.Fatalf("resizing a flavor in use must fail, got %v", err)
	}
	if err := db.DeleteFlavor(ctx, flavor.ID); !errors.Is(err, ErrFlavorInUse) {
		t.Fatalf("deleting a flavor in use must fail, got %v", err)
	}
	name, disabled := "XLarge Old", false
	updated, err := db.UpdateFlavor(ctx, flavor.ID, domain.FlavorUpdate{Name: &name, Enabled: &disabled})
	if err != nil || updated.Name != name || updated.Enabled {
		t.Fatalf("renaming and disabling a flavor in use should work, got %#v (%v)", updated, err)
	}
	enabled, err := db.ListFlavors(ctx)
	if err != nil {
		t.Fatalf("list flavors: %v", err)
	}
	for _, f := range enabled {
		if f.ID == flavor.ID {
			t.Fatal("a disabled flavor must not be offered")
		}
	}
	if err := db.DeleteVirtualMachine(ctx, "platform", "platform-flavor-user"); err != nil {
		t.Fatalf("delete vm: %v", err)
	}
	if _, err := db.UpdateFlavor(ctx, flavor.ID, domain.FlavorUpdate{CPU: &cpu}); err != nil {
		t.Fatalf("resizing an unused flavor should work: %v", err)
	}
	if err := db.DeleteFlavor(ctx, flavor.ID); err != nil {
		t.Fatalf("delete flavor: %v", err)
	}
	if err := db.DeleteFlavor(ctx, flavor.ID); !errors.Is(err, ErrFlavorNotFound) {
		t.Fatalf("expected ErrFlavorNotFound after delete, got %v", err)
	}

	// Seeded flavors keep an admin's edits across restarts.
	original := "Small"
	renamed := "Small (edited)"
	if _, err := db.UpdateFlavor(ctx, "small", domain.FlavorUpdate{Name: &renamed}); err != nil {
		t.Fatalf("rename small: %v", err)
	}
	defer func() { _, _ = db.UpdateFlavor(ctx, "small", domain.FlavorUpdate{Name: &original}) }()
	if err := db.ApplySchema(ctx); err != nil {
		t.Fatalf("re-apply schema: %v", err)
	}
	all, _ = db.ListAllFlavors(ctx)
	for _, f := range all {
		if f.ID == "small" && f.Name != renamed {
			t.Fatalf("restart reverted the admin's edit: %q", f.Name)
		}
	}
}
