//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestVMOperations covers what an operator does to a running VM: create it from an admin-defined flavor,
// read its boot log, take over its console from another session, and restart it.
func TestVMOperations(t *testing.T) {
	e := newEnv(t)
	const project = "platform"
	suffix := fmt.Sprintf("%d", time.Now().Unix()%100000)

	// A flavor made for this test. Its cleanup is registered before the VM's, so it runs after the VM is gone.
	var flavor struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	e.mustCall(t, http.MethodPost, "/admin/flavors", map[string]any{
		"name": "e2e-" + suffix, "cpu": 1, "memoryBytes": 512 << 20, "rootDiskBytes": 2 << 30,
	}, http.StatusCreated, &flavor)
	flavorID := flavor.Data.ID
	t.Cleanup(func() {
		if code := e.call(t, http.MethodDelete, "/admin/flavors/"+flavorID, nil, nil); code != http.StatusNoContent {
			t.Errorf("delete flavor %s: %d", flavorID, code)
		}
	})
	e.mustCall(t, http.MethodPatch, "/admin/flavors/"+flavorID, map[string]any{"name": "E2E " + suffix}, http.StatusOK, nil)

	var vm struct {
		Data vmView `json:"data"`
	}
	e.mustCall(t, http.MethodPost, "/projects/"+project+"/vms", map[string]any{
		"name": "ops-" + suffix, "imageId": "platform-cirros", "flavorId": flavorID,
		"cloudInit": map[string]string{"username": "cirros", "password": "gocubsgo"},
	}, http.StatusCreated, &vm)
	vmID := vm.Data.ID
	t.Cleanup(func() { e.deleteVM(t, project, vmID) })
	vmPath := "/projects/" + project + "/vms/" + vmID

	if code := e.call(t, http.MethodPatch, "/admin/flavors/"+flavorID, map[string]any{"cpu": 2}, nil); code != http.StatusConflict {
		t.Errorf("resizing a flavor in use: expected 409, got %d", code)
	}
	if code := e.call(t, http.MethodPost, vmPath+"/restart", nil, nil); code != http.StatusConflict {
		t.Errorf("restarting a VM that is not running yet: expected 409, got %d", code)
	}

	e.waitRunning(t, project, vmID)

	// The boot log fills in without anyone logging in.
	eventually(t, 4*time.Minute, "the boot log to reach the login prompt", func() (bool, string) {
		var resp struct {
			Data struct {
				Log string `json:"log"`
			} `json:"data"`
		}
		code := e.call(t, http.MethodGet, vmPath+"/console/log", nil, &resp)
		return code == http.StatusOK && strings.Contains(resp.Data.Log, "login as 'cirros'"), fmt.Sprintf("%d, %d bytes", code, len(resp.Data.Log))
	})

	consoleInUse := func() bool {
		var resp struct {
			Data struct {
				InUse bool `json:"inUse"`
			} `json:"data"`
		}
		e.mustCall(t, http.MethodGet, vmPath+"/console", nil, http.StatusOK, &resp)
		return resp.Data.InUse
	}
	if consoleInUse() {
		t.Fatal("nobody is connected to the console yet")
	}
	first := e.openConsole(t, project, vmID)
	first.login()
	if !consoleInUse() {
		t.Fatal("the console should be in use")
	}
	// A second session takes over; the first one is told why it was dropped.
	second := e.openConsole(t, project, vmID)
	if code := closeCodeOf(first, 30*time.Second); code != 4001 {
		t.Errorf("the replaced session closed with %d, want 4001", code)
	}
	second.login()
	if rc, _ := second.run("true", 10*time.Second); rc != 0 {
		t.Error("the new session should work")
	}
	_ = second.conn.Close()
	eventually(t, 30*time.Second, "the console to be free", func() (bool, string) { return !consoleInUse(), "in use" })

	// Restart replaces the VM instance.
	before, err := e.get(t, gvrVMI, vmNamespace, vmID)
	if err != nil {
		t.Fatalf("vmi: %v", err)
	}
	e.mustCall(t, http.MethodPost, vmPath+"/restart", nil, http.StatusAccepted, nil)
	eventually(t, 5*time.Minute, "a new VM instance after restart", func() (bool, string) {
		after, err := e.get(t, gvrVMI, vmNamespace, vmID)
		if err != nil {
			return false, err.Error()
		}
		phase, _ := after.Object["status"].(map[string]any)["phase"].(string)
		return after.GetUID() != before.GetUID() && phase == "Running", phase
	})

	if code := e.call(t, http.MethodDelete, "/admin/flavors/"+flavorID, nil, nil); code != http.StatusConflict {
		t.Errorf("deleting a flavor in use: expected 409, got %d", code)
	}
}

// closeCodeOf waits for the server to close the console and returns the WebSocket close code.
func closeCodeOf(c *console, timeout time.Duration) int {
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-c.out:
			if !ok {
				return c.closeCode
			}
		case <-deadline:
			return 0
		}
	}
}
