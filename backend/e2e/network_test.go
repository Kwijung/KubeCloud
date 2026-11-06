//go:build e2e

package e2e

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestVPCEgressDNSAndLiveMigratableDisks builds a VPC with two public subnets created at the same time and a
// private subnet, runs a VM in a public and a private subnet, and checks from inside the guests that the
// public VM reaches the internet and DNS while the private VM does not. It also checks that both public
// subnets kept their egress route and that root disks are ReadWriteMany and live-migratable.
func TestVPCEgressDNSAndLiveMigratableDisks(t *testing.T) {
	e := newEnv(t)
	const project = "platform"
	octet := 60 + rand.Intn(30)
	name := fmt.Sprintf("e2e%d", time.Now().Unix()%100000)
	networkID := project + "-" + name

	e.mustCall(t, http.MethodPost, "/projects/"+project+"/networks",
		map[string]string{"name": name, "cidr": fmt.Sprintf("10.%d.0.0/16", octet)}, http.StatusCreated, nil)
	t.Cleanup(func() { e.call(t, http.MethodDelete, "/projects/"+project+"/networks/"+networkID, nil, nil) })

	subnetPath := "/projects/" + project + "/networks/" + networkID + "/subnets"
	// Registered after the VPC's cleanup and before any subnet exists, so it runs before the VPC's cleanup
	// (last-in first-out) and covers subnets created by a test that stops half way.
	t.Cleanup(func() { e.deleteSubnets(t, subnetPath) })

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i, s := range []struct{ name, third string }{{"pub-a", "1"}, {"pub-b", "2"}} {
		wg.Add(1)
		go func(i int, sname, third string) {
			defer wg.Done()
			statuses[i] = e.call(t, http.MethodPost, subnetPath, map[string]string{
				"name": sname, "cidr": fmt.Sprintf("10.%d.%s.0/24", octet, third), "type": "public",
			}, nil)
		}(i, s.name, s.third)
	}
	wg.Wait()
	if statuses[0] != http.StatusCreated || statuses[1] != http.StatusCreated {
		t.Fatalf("concurrent public subnet creates returned %v, want 201 for both", statuses)
	}
	e.mustCall(t, http.MethodPost, subnetPath, map[string]string{
		"name": "priv", "cidr": fmt.Sprintf("10.%d.3.0/24", octet), "type": "private",
	}, http.StatusCreated, nil)

	vpc, err := e.get(t, gvrVPC, "", "kloudia-vpc-"+networkID)
	if err != nil {
		t.Fatalf("vpc: %v", err)
	}
	routes, _, _ := unstructured.NestedSlice(vpc.Object, "spec", "policyRoutes")
	if len(routes) != 2 {
		t.Fatalf("both concurrently created public subnets need an egress route, got %d: %v", len(routes), routes)
	}

	pubVM := e.createVM(t, project, name+"-pub", networkID+"-pub-a")
	privVM := e.createVM(t, project, name+"-priv", networkID+"-priv")
	e.waitRunning(t, project, pubVM)
	e.waitRunning(t, project, privVM)

	// The disk checks apply when the deployment uses ReadWriteMany root disks (KLOUDIA_KUBEVIRT_DISK_ACCESS_MODE).
	wantMode := os.Getenv("KLOUDIA_E2E_DISK_ACCESS_MODE")
	for _, id := range []string{pubVM, privVM} {
		if wantMode == "" {
			break
		}
		dv, err := e.get(t, gvrDV, vmNamespace, id+"-rootdisk")
		if err != nil {
			t.Fatalf("datavolume %s: %v", id, err)
		}
		modes, _, _ := unstructured.NestedStringSlice(dv.Object, "spec", "pvc", "accessModes")
		if len(modes) != 1 || modes[0] != wantMode {
			t.Errorf("%s root disk access modes = %v, want [%s]", id, modes, wantMode)
		}
		if wantMode != "ReadWriteMany" {
			continue
		}
		vmi, err := e.get(t, gvrVMI, vmNamespace, id)
		if err != nil {
			t.Fatalf("vmi %s: %v", id, err)
		}
		if !conditionTrue(vmi, "LiveMigratable") {
			t.Errorf("%s is not live-migratable", id)
		}
	}

	pub := e.openConsole(t, project, pubVM)
	pub.login()
	// The login prompt appears a few seconds before DHCP finishes (dhcpcd probes the address first).
	pub.waitForDefaultRoute()
	if rc, out := pub.run("ping -c 3 -W 3 8.8.8.8", 30*time.Second); rc != 0 {
		t.Errorf("public VM cannot reach the internet: %s", out)
	}
	if rc, out := pub.run("nslookup -type=a cirros-cloud.net", 30*time.Second); rc != 0 {
		t.Errorf("public VM cannot resolve names: %s", out)
	}

	priv := e.openConsole(t, project, privVM)
	priv.login()
	priv.waitForDefaultRoute()
	// The private VM has a working network inside the VPC ...
	if rc, out := priv.run(fmt.Sprintf("ping -c 2 -W 2 10.%d.3.1", octet), 30*time.Second); rc != 0 {
		t.Errorf("private VM cannot reach its subnet gateway: %s", out)
	}
	// ... but no way out of it.
	if rc, out := priv.run("ping -c 2 -W 2 8.8.8.8", 30*time.Second); rc == 0 {
		t.Errorf("private VM must not reach the internet: %s", out)
	}
}

// deleteSubnets deletes every subnet of a VPC. The public subnet hosting the NAT gateway can only go last,
// and which one that is depends on creation order, so it retries until none are left.
func (e *env) deleteSubnets(t *testing.T, subnetPath string) {
	t.Helper()
	for pass := 0; pass < 4; pass++ {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		e.call(t, http.MethodGet, subnetPath, nil, &resp)
		if len(resp.Data) == 0 {
			return
		}
		for _, s := range resp.Data {
			e.call(t, http.MethodDelete, subnetPath+"/"+s.ID, nil, nil)
		}
	}
	t.Errorf("subnets under %s were not all deleted", subnetPath)
}

func conditionTrue(obj *unstructured.Unstructured, conditionType string) bool {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		m, ok := c.(map[string]any)
		if ok && m["type"] == conditionType && m["status"] == "True" {
			return true
		}
	}
	return false
}
