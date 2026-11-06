//go:build e2e

// Package e2e runs Kloudia end to end against a real cluster: the deployed API, Kubernetes, and the VMs'
// serial consoles.
//
//	KLOUDIA_E2E_API=http://192.168.160.64:30080/api/v1 KUBECONFIG=... go test -tags e2e ./e2e/ -v -timeout 30m
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const vmNamespace = "kloudia-vms"

type env struct {
	api  string
	kube dynamic.Interface
}

func newEnv(t *testing.T) *env {
	t.Helper()
	api := os.Getenv("KLOUDIA_E2E_API")
	if api == "" {
		t.Skip("KLOUDIA_E2E_API is required for e2e tests")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatalf("kubeconfig: %v", err)
	}
	kube, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("kubernetes client: %v", err)
	}
	return &env{api: strings.TrimRight(api, "/"), kube: kube}
}

// call sends a JSON request and decodes a JSON response into out (when non-nil). It returns the status code.
func (e *env) call(t *testing.T, method, path string, body any, out any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.api+path, reader)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s %s (%d): %v: %s", method, path, resp.StatusCode, err, raw)
		}
	}
	return resp.StatusCode
}

// mustCall is call that fails the test unless the status is want.
func (e *env) mustCall(t *testing.T, method, path string, body any, want int, out any) {
	t.Helper()
	var raw json.RawMessage
	got := e.call(t, method, path, body, &raw)
	if got != want {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, want, got, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
}

func (e *env) get(t *testing.T, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	t.Helper()
	if namespace == "" {
		return e.kube.Resource(gvr).Get(context.Background(), name, metav1.GetOptions{})
	}
	return e.kube.Resource(gvr).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

// eventually polls check until it returns true or the timeout passes.
func eventually(t *testing.T, timeout time.Duration, what string, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		ok, state := check()
		if ok {
			return
		}
		last = state
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("timed out after %s waiting for %s (last state: %s)", timeout, what, last)
}

var (
	gvrVM  = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}
	gvrVMI = schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachineinstances"}
	gvrDV  = schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}
	gvrVPC = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"}
)

type vmView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// createVM creates a CirrOS VM (public image, cirros/gocubsgo) and registers its deletion as cleanup.
func (e *env) createVM(t *testing.T, project, name, subnetID string) string {
	t.Helper()
	body := map[string]any{
		"name": name, "imageId": "platform-cirros", "flavorId": "small",
		"cloudInit": map[string]string{"username": "cirros", "password": "gocubsgo"},
	}
	if subnetID != "" {
		body["subnetId"] = subnetID
	}
	var resp struct {
		Data vmView `json:"data"`
	}
	e.mustCall(t, http.MethodPost, "/projects/"+project+"/vms", body, http.StatusCreated, &resp)
	id := resp.Data.ID
	t.Cleanup(func() { e.deleteVM(t, project, id) })
	return id
}

func (e *env) deleteVM(t *testing.T, project, id string) {
	t.Helper()
	e.call(t, http.MethodDelete, "/projects/"+project+"/vms/"+id, nil, nil)
	eventually(t, 3*time.Minute, "VM "+id+" to be gone", func() (bool, string) {
		_, err := e.get(t, gvrVMI, vmNamespace, id)
		return err != nil, "vmi still present"
	})
}

func (e *env) waitRunning(t *testing.T, project, id string) {
	t.Helper()
	eventually(t, 6*time.Minute, "VM "+id+" to run", func() (bool, string) {
		var resp struct {
			Data struct {
				RuntimeStatus  string `json:"runtimeStatus"`
				FailureMessage string `json:"failureMessage"`
			} `json:"data"`
		}
		e.call(t, http.MethodGet, "/projects/"+project+"/vms/"+id, nil, &resp)
		if resp.Data.RuntimeStatus == "Failed" {
			t.Fatalf("VM %s failed: %s", id, resp.Data.FailureMessage)
		}
		return resp.Data.RuntimeStatus == "Running", resp.Data.RuntimeStatus
	})
}

// console logs into a CirrOS VM over the API's console WebSocket and runs commands.
type console struct {
	t    *testing.T
	conn *websocket.Conn
	out  chan string
	buf  strings.Builder
}

func (e *env) openConsole(t *testing.T, project, id string) *console {
	t.Helper()
	u, err := url.Parse(e.api + "/projects/" + project + "/vms/" + id + "/console/stream")
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("console %s: %v", id, err)
	}
	c := &console{t: t, conn: conn, out: make(chan string, 1024)}
	go func() {
		defer close(c.out)
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			c.out <- string(b)
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

func (c *console) send(s string) {
	if err := c.conn.WriteMessage(websocket.BinaryMessage, []byte(s)); err != nil {
		c.t.Fatalf("console write: %v", err)
	}
}

func (c *console) waitFor(timeout time.Duration, subs ...string) string {
	deadline := time.After(timeout)
	for {
		for _, s := range subs {
			if strings.Contains(c.buf.String(), s) {
				return s
			}
		}
		select {
		case m, ok := <-c.out:
			if !ok {
				return ""
			}
			c.buf.WriteString(m)
		case <-deadline:
			return ""
		}
	}
}

// login waits for the guest to boot and logs in as cirros.
func (c *console) login() {
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		c.buf.Reset()
		c.send("\n")
		switch c.waitFor(10*time.Second, "login:", "$ ") {
		case "$ ":
			return
		case "login:":
			time.Sleep(time.Second)
			c.buf.Reset()
			c.send("cirros\n")
			if c.waitFor(10*time.Second, "assword:") == "" {
				continue
			}
			c.send("gocubsgo\n")
			if c.waitFor(20*time.Second, "$ ") != "" {
				return
			}
		}
	}
	c.t.Fatalf("could not log in to the console: %q", c.buf.String())
}

// run executes cmd and returns its exit status and output.
func (c *console) run(cmd string, timeout time.Duration) (int, string) {
	c.buf.Reset()
	// The quotes split the marker in the echoed command line, so only the real output matches it.
	c.send(cmd + `; echo "__RC""=$?__"` + "\n")
	if c.waitFor(timeout, "__RC=") == "" {
		c.t.Fatalf("command %q did not finish: %q", cmd, c.buf.String())
	}
	c.waitFor(2*time.Second, "__\r", "__\n")
	text := strings.ReplaceAll(c.buf.String(), "\r", "")
	i := strings.Index(text, "__RC=")
	var rc int
	fmt.Sscanf(text[i:], "__RC=%d__", &rc)
	return rc, text[:i]
}
