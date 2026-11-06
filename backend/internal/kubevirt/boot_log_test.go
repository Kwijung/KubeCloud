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
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "kloudia-vms", CreationTimestamp: metav1.NewTime(created),
			Labels: map[string]string{"vm.kubevirt.io/name": vmID, "kubevirt.io": "virt-launcher"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "guest-console-log", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
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

func TestBootLogSkipsAPodThatIsStillStarting(t *testing.T) {
	now := time.Now()
	starting := launcherPod("virt-launcher-web-new", "platform-web", now)
	starting.Status.Phase = corev1.PodPending
	starting.Status.InitContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}

	reader := NewBootLogReader("kloudia-vms", fake.NewSimpleClientset(starting))
	if _, err := reader.BootLog(context.Background(), "platform-web"); !errors.Is(err, ErrBootLogUnavailable) {
		t.Fatalf("a starting pod has no log yet; expected ErrBootLogUnavailable, got %v", err)
	}

	// While a restarted VM's new pod starts, the old one is still the one to read.
	old := launcherPod("virt-launcher-web-old", "platform-web", now.Add(-time.Hour))
	reader = NewBootLogReader("kloudia-vms", fake.NewSimpleClientset(starting, old))
	if pod, err := reader.launcherPod(context.Background(), "platform-web"); err != nil || pod != "virt-launcher-web-old" {
		t.Fatalf("expected the running old pod, got %q (%v)", pod, err)
	}
}
