package kubevirt

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func launcherPod(name, vmID string, created time.Time) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "kloudia-vms", CreationTimestamp: metav1.NewTime(created),
		Labels: map[string]string{"vm.kubevirt.io/name": vmID, "kubevirt.io": "virt-launcher"},
	}}
}

func TestBootLogReadsTheNewestLauncherPod(t *testing.T) {
	now := time.Now()
	client := fake.NewSimpleClientset(
		launcherPod("virt-launcher-web-old", "platform-web", now.Add(-time.Hour)),
		launcherPod("virt-launcher-web-new", "platform-web", now),
		launcherPod("virt-launcher-other", "platform-other", now),
	)
	reader := NewBootLogReader("kloudia-vms", client)

	pod, err := reader.launcherPod(context.Background(), "platform-web")
	if err != nil {
		t.Fatalf("launcher pod: %v", err)
	}
	if pod != "virt-launcher-web-new" {
		t.Fatalf("expected the newest launcher pod, got %s", pod)
	}
	if _, err := reader.BootLog(context.Background(), "platform-web"); err != nil {
		t.Fatalf("boot log: %v", err)
	}
}

func TestBootLogWithoutLauncherPod(t *testing.T) {
	reader := NewBootLogReader("kloudia-vms", fake.NewSimpleClientset())
	if _, err := reader.BootLog(context.Background(), "platform-web"); !errors.Is(err, ErrBootLogUnavailable) {
		t.Fatalf("expected ErrBootLogUnavailable, got %v", err)
	}
}
