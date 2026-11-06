//go:build e2e

package e2e

import (
	"fmt"
	"math/rand"
	"net/http"
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
	var wg sync.WaitGroup
	for _, s := range []struct{ name, cidr string }{{"pub-a", "1"}, {"pub-b", "2"}} {
		wg.Add(1)
		go func(sname, third string) {
			defer wg.Done()
			e.mustCall(t, http.MethodPost, subnetPath, map[string]string{
				"name": sname, "cidr": fmt.Sprintf("10.%d.%s.0/24", octet, third), "type": "public",
			}, http.StatusCreated, nil)
		}(s.name, s.cidr)
	}
	wg.Wait()
	e.mustCall(t, http.MethodPost, subnetPath, map[string]string{
		"name": "priv", "cidr": fmt.Sprintf("10.%d.3.0/24", octet), "type": "private",
	}, http.StatusCreated, nil)
	// Subnets go before the VPC; registered after it, these cleanups run first.
	for _, s := range []string{"priv", "pub-b", "pub-a"} {
		id := networkID + "-" + s
		t.Cleanup(func() { e.call(t, http.MethodDelete, subnetPath+"/"+id, nil, nil) })
	}

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

	for _, id := range []string{pubVM, privVM} {
		dv, err := e.get(t, gvrDV, vmNamespace, id+"-rootdisk")
		if err != nil {
			t.Fatalf("datavolume %s: %v", id, err)
		}
		modes, _, _ := unstructured.NestedStringSlice(dv.Object, "spec", "pvc", "accessModes")
		if len(modes) != 1 || modes[0] != "ReadWriteMany" {
			t.Errorf("%s root disk access modes = %v, want [ReadWriteMany]", id, modes)
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
	if rc, out := pub.run("ip route | grep default", 10*time.Second); rc != 0 {
		t.Errorf("public VM has no default route: %s", out)
	}
	if rc, out := pub.run("ping -c 3 -W 3 8.8.8.8", 30*time.Second); rc != 0 {
		t.Errorf("public VM cannot reach the internet: %s", out)
	}
	if rc, out := pub.run("nslookup -type=a cirros-cloud.net", 30*time.Second); rc != 0 {
		t.Errorf("public VM cannot resolve names: %s", out)
	}

	priv := e.openConsole(t, project, privVM)
	priv.login()
	if rc, out := priv.run("ping -c 2 -W 2 8.8.8.8", 30*time.Second); rc == 0 {
		t.Errorf("private VM must not reach the internet: %s", out)
	}
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
