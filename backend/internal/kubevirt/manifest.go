package kubevirt

import (
	"fmt"

	"github.com/kloudia/kloudia/backend/internal/domain"
)

type Resources struct {
	DataVolume     map[string]any
	VirtualMachine map[string]any
}

func BuildResources(namespace string, storageClassName string, plan domain.VirtualMachineProvisioningPlan) Resources {
	vmName := plan.VirtualMachine.ID
	dataVolumeName := fmt.Sprintf("%s-rootdisk", vmName)

	dataVolume := map[string]any{
		"apiVersion": "cdi.kubevirt.io/v1beta1",
		"kind":       "DataVolume",
		"metadata": map[string]any{
			"name":      dataVolumeName,
			"namespace": namespace,
			"labels":    labels(plan),
		},
		"spec": map[string]any{
			"source": map[string]any{
				"http": map[string]any{
					"url": plan.Image.SourceURL,
				},
			},
			"pvc": map[string]any{
				"accessModes": []any{"ReadWriteOnce"},
				"resources": map[string]any{
					"requests": map[string]any{
						"storage": formatBytes(plan.Flavor.RootDiskBytes),
					},
				},
				"storageClassName": storageClassName,
			},
		},
	}

	vm := map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachine",
		"metadata": map[string]any{
			"name":      vmName,
			"namespace": namespace,
			"labels":    labels(plan),
		},
		"spec": map[string]any{
			"runStrategy": "Always",
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels(plan),
				},
				"spec": map[string]any{
					"domain": map[string]any{
						"cpu": map[string]any{
							"cores": int64(plan.Flavor.CPU),
						},
						"devices": map[string]any{
							"disks": []any{
								map[string]any{
									"name": "rootdisk",
									"disk": map[string]any{"bus": "virtio"},
								},
							},
						},
						"resources": map[string]any{
							"requests": map[string]any{
								"memory": formatBytes(plan.Flavor.MemoryBytes),
							},
						},
					},
					"volumes": []any{
						map[string]any{
							"name":       "rootdisk",
							"dataVolume": map[string]any{"name": dataVolumeName},
						},
					},
				},
			},
		},
	}

	if plan.Subnet != nil {
		applyMultusNetwork(vm, namespace, *plan.Subnet)
	}

	return Resources{
		DataVolume:     dataVolume,
		VirtualMachine: vm,
	}
}

func applyMultusNetwork(vm map[string]any, namespace string, subnet domain.Subnet) {
	templateSpec := vm["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	templateSpec["networks"] = []any{
		map[string]any{
			"name": "net1",
			"multus": map[string]any{
				"networkName": namespace + "/" + subnet.NADName,
			},
		},
	}
	devices := templateSpec["domain"].(map[string]any)["devices"].(map[string]any)
	devices["interfaces"] = []any{
		map[string]any{
			"name":   "net1",
			"bridge": map[string]any{},
		},
	}
}

func BuildDeleteResources(namespace string, vm domain.VirtualMachine) Resources {
	return Resources{
		VirtualMachine: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachine",
			"metadata": map[string]any{
				"name":      vm.ID,
				"namespace": namespace,
			},
		},
		DataVolume: map[string]any{
			"apiVersion": "cdi.kubevirt.io/v1beta1",
			"kind":       "DataVolume",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("%s-rootdisk", vm.ID),
				"namespace": namespace,
			},
		},
	}
}

func labels(plan domain.VirtualMachineProvisioningPlan) map[string]any {
	return map[string]any{
		"app.kubernetes.io/name":       "kloudia",
		"app.kubernetes.io/managed-by": "kloudia",
		"kloudia.io/project":           plan.VirtualMachine.ProjectID,
		"kloudia.io/vm-id":             plan.VirtualMachine.ID,
	}
}

func formatBytes(bytes int64) string {
	if bytes < 1024*1024*1024 {
		mib := bytes / 1024 / 1024
		if mib < 1 {
			mib = 1
		}
		return fmt.Sprintf("%dMi", mib)
	}
	gib := bytes / 1024 / 1024 / 1024
	return fmt.Sprintf("%dGi", gib)
}
