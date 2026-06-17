package kubevirt

import "testing"

func TestConsoleWebSocketURLBuildsKubeVirtConsoleSubresourceURL(t *testing.T) {
	rawURL, err := consoleWebSocketURL("https://10.96.0.1:6443", "kloudia-vms", "platform-portal-02")
	if err != nil {
		t.Fatalf("build console websocket url: %v", err)
	}

	want := "wss://10.96.0.1:6443/apis/subresources.kubevirt.io/v1/namespaces/kloudia-vms/virtualmachineinstances/platform-portal-02/console"
	if rawURL != want {
		t.Fatalf("expected %q, got %q", want, rawURL)
	}
}

func TestConsoleWebSocketURLRejectsUnsupportedScheme(t *testing.T) {
	if _, err := consoleWebSocketURL("ftp://10.96.0.1:6443", "kloudia-vms", "platform-portal-02"); err == nil {
		t.Fatalf("expected unsupported scheme error")
	}
}
