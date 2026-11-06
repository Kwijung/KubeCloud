package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kloudia/kloudia/backend/internal/domain"
	"github.com/kloudia/kloudia/backend/internal/kubevirt"
)

func (p *recordingProvisioner) RestartVirtualMachine(_ context.Context, vm domain.VirtualMachine) error {
	p.restartedVMs = append(p.restartedVMs, vm)
	return nil
}

type fakeBootLogReader struct {
	log string
	err error
}

func (r fakeBootLogReader) BootLog(context.Context, string) (string, error) {
	return r.log, r.err
}

func opsRouter(status string, deps Dependencies) (http.Handler, *recordingProvisioner) {
	vm := domain.VirtualMachine{ID: "platform-web-01", ProjectID: "platform", Name: "web-01", Status: status}
	provisioner := &recordingProvisioner{}
	deps.Version = "test"
	deps.Store = &fakeCatalogStore{vms: []domain.VirtualMachine{vm}}
	deps.Provisioner = provisioner
	deps.StatusReader = fakeStatusReader{statuses: map[string]string{vm.ID: status}}
	return NewRouterWithDependencies(deps), provisioner
}

func TestRestartVirtualMachine(t *testing.T) {
	router, provisioner := opsRouter("Running", Dependencies{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms/platform-web-01/restart", nil))
	if rec.Code != http.StatusAccepted || len(provisioner.restartedVMs) != 1 {
		t.Fatalf("expected 202 and one restart, got %d %#v", rec.Code, provisioner.restartedVMs)
	}

	router, provisioner = opsRouter("Stopped", Dependencies{})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/projects/platform/vms/platform-web-01/restart", nil))
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "VM_NOT_RUNNING" || len(provisioner.restartedVMs) != 0 {
		t.Fatalf("expected 409 VM_NOT_RUNNING without a restart, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBootLog(t *testing.T) {
	cases := []struct {
		name   string
		reader BootLogReader
		status int
		code   string
	}{
		{"not configured", nil, http.StatusNotImplemented, "BOOT_LOG_NOT_CONFIGURED"},
		{"no launcher pod", fakeBootLogReader{err: kubevirt.ErrBootLogUnavailable}, http.StatusConflict, "BOOT_LOG_UNAVAILABLE"},
		{"reads the log", fakeBootLogReader{log: "login as 'cirros' user."}, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, _ := opsRouter("Running", Dependencies{BootLogReader: tc.reader})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms/platform-web-01/console/log", nil))
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, rec.Code, rec.Body.String())
			}
			if tc.code != "" {
				if code := errorCode(t, rec); code != tc.code {
					t.Fatalf("expected %s, got %s", tc.code, code)
				}
				return
			}
			var body struct {
				Data struct {
					Log string `json:"log"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Data.Log == "" {
				t.Fatalf("expected the log, got %s (%v)", rec.Body.String(), err)
			}
		})
	}
}

func TestConsoleCapabilityReportsInUse(t *testing.T) {
	streamer := &recordingConsoleStreamer{inUse: true}
	router, _ := opsRouter("Running", Dependencies{ConsoleStreamer: streamer})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects/platform/vms/platform-web-01/console", nil))
	var body struct {
		Data struct {
			Available bool `json:"available"`
			InUse     bool `json:"inUse"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Data.InUse {
		t.Fatalf("expected inUse, got %s", rec.Body.String())
	}
}
