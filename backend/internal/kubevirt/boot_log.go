package kubevirt

import (
	"context"
	"errors"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ErrBootLogUnavailable is returned when the VM has no launcher pod, so it is not running and has no log.
var ErrBootLogUnavailable = errors.New("the virtual machine has no running instance")

const (
	// guestConsoleLogContainer is the virt-launcher sidecar that records the guest's serial console output.
	guestConsoleLogContainer = "guest-console-log"
	bootLogTailLines         = int64(1000)
	bootLogLimitBytes        = int64(256 << 10)
)

// BootLogReader reads a VM's serial console output since it last started, without logging in.
type BootLogReader struct {
	namespace string
	client    kubernetes.Interface
}

func NewBootLogReader(namespace string, client kubernetes.Interface) *BootLogReader {
	return &BootLogReader{namespace: namespace, client: client}
}

// NewKubernetesBootLogReader builds a BootLogReader from a kubeconfig, or in-cluster config when empty.
func NewKubernetesBootLogReader(namespace string, kubeconfigPath string) (*BootLogReader, error) {
	cfg, err := buildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewBootLogReader(namespace, client), nil
}

// BootLog returns the last lines of the guest console log.
func (r *BootLogReader) BootLog(ctx context.Context, vmID string) (string, error) {
	pod, err := r.launcherPod(ctx, vmID)
	if err != nil {
		return "", err
	}
	tail, limit := bootLogTailLines, bootLogLimitBytes
	stream, err := r.client.CoreV1().Pods(r.namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: guestConsoleLogContainer, TailLines: &tail, LimitBytes: &limit,
	}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	raw, err := io.ReadAll(stream)
	return string(raw), err
}

// launcherPod finds the VM's newest virt-launcher pod; a restarted VM briefly has an old one terminating.
func (r *BootLogReader) launcherPod(ctx context.Context, vmID string) (string, error) {
	pods, err := r.client.CoreV1().Pods(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "kubevirt.io=virt-launcher,vm.kubevirt.io/name=" + vmID,
	})
	if err != nil {
		return "", err
	}
	var newest *corev1.Pod
	for i := range pods.Items {
		if newest == nil || pods.Items[i].CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = &pods.Items[i]
		}
	}
	if newest == nil {
		return "", ErrBootLogUnavailable
	}
	return newest.Name, nil
}
