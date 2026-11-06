package kubevirt

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type DynamicApplier struct {
	client dynamic.Interface
}

func NewDynamicApplier(kubeconfigPath string) (*DynamicApplier, error) {
	cfg, err := buildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &DynamicApplier{client: client}, nil
}

func (a *DynamicApplier) Apply(ctx context.Context, resource map[string]any) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("dynamic client is not configured")
	}
	gvr, namespaced, err := resourceMapping(resource)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: resource}
	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("resource %s requires metadata.name", gvr.Resource)
	}
	var resourceClient dynamic.ResourceInterface = a.client.Resource(gvr)
	if namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			return fmt.Errorf("resource %s requires metadata.namespace", gvr.Resource)
		}
		resourceClient = a.client.Resource(gvr).Namespace(namespace)
	}
	created, err := resourceClient.Create(ctx, obj, v1.CreateOptions{})
	if err == nil {
		obj.SetResourceVersion(created.GetResourceVersion())
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	current, err := resourceClient.Get(ctx, name, v1.GetOptions{})
	if err != nil {
		return err
	}
	obj.SetResourceVersion(current.GetResourceVersion())
	_, err = resourceClient.Update(ctx, obj, v1.UpdateOptions{})
	return err
}

func (a *DynamicApplier) Delete(ctx context.Context, resource map[string]any) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("dynamic client is not configured")
	}
	gvr, namespaced, err := resourceMapping(resource)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: resource}
	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("resource %s requires metadata.name", gvr.Resource)
	}
	var resourceClient dynamic.ResourceInterface = a.client.Resource(gvr)
	if namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			return fmt.Errorf("resource %s requires metadata.namespace", gvr.Resource)
		}
		resourceClient = a.client.Resource(gvr).Namespace(namespace)
	}
	err = resourceClient.Delete(ctx, name, v1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (a *DynamicApplier) Patch(ctx context.Context, resource map[string]any) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("dynamic client is not configured")
	}
	gvr, namespaced, err := resourceMapping(resource)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{Object: resource}
	name := obj.GetName()
	if name == "" {
		return fmt.Errorf("resource %s requires metadata.name", gvr.Resource)
	}
	var resourceClient dynamic.ResourceInterface = a.client.Resource(gvr)
	if namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			return fmt.Errorf("resource %s requires metadata.namespace", gvr.Resource)
		}
		resourceClient = a.client.Resource(gvr).Namespace(namespace)
	}
	patch, err := json.Marshal(resource)
	if err != nil {
		return err
	}
	_, err = resourceClient.Patch(ctx, name, types.MergePatchType, patch, v1.PatchOptions{})
	return err
}

func (a *DynamicApplier) SetVirtualMachineRunStrategy(ctx context.Context, namespace string, name string, runStrategy string) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("dynamic client is not configured")
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"runStrategy":%q}}`, runStrategy))
	_, err := a.client.Resource(schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}).
		Namespace(namespace).
		Patch(ctx, name, types.MergePatchType, patch, v1.PatchOptions{})
	return err
}

func buildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	return clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
}

func resourceMapping(resource map[string]any) (schema.GroupVersionResource, bool, error) {
	apiVersion, _ := resource["apiVersion"].(string)
	kind, _ := resource["kind"].(string)
	switch {
	case apiVersion == "cdi.kubevirt.io/v1beta1" && kind == "DataVolume":
		return schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"}, true, nil
	case apiVersion == "kubevirt.io/v1" && kind == "VirtualMachine":
		return schema.GroupVersionResource{Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines"}, true, nil
	case apiVersion == "kubeovn.io/v1" && kind == "Vpc":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpcs"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "Subnet":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "subnets"}, false, nil
	case apiVersion == "k8s.cni.cncf.io/v1" && kind == "NetworkAttachmentDefinition":
		return schema.GroupVersionResource{Group: "k8s.cni.cncf.io", Version: "v1", Resource: "network-attachment-definitions"}, true, nil
	case apiVersion == "kubeovn.io/v1" && kind == "VpcNatGateway":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-nat-gateways"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "IptablesEIP":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-eips"}, false, nil
	case apiVersion == "kubeovn.io/v1" && kind == "IptablesSnatRule":
		return schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "iptables-snat-rules"}, false, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("unsupported resource %s %s", apiVersion, kind)
	}
}
