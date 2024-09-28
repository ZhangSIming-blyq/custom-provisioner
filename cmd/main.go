package main

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"os"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v7/controller"
)

type customProvisioner struct {
	// Define any dependencies that your provisioner might need here, here I use the kubernetes client
	client kubernetes.Interface
}

// NewCustomProvisioner creates a new instance of the custom provisioner
func NewCustomProvisioner(client kubernetes.Interface) controller.Provisioner {
	// customProvisioner needs to implement "Provision" and "Delete" methods in order to satisfy the Provisioner interface
	return &customProvisioner{
		client: client,
	}
}

func (p *customProvisioner) Provision(ctx context.Context, options controller.ProvisionOptions) (*corev1.PersistentVolume, controller.ProvisioningState, error) {
	// Validate the PVC spec, 0 storage size is not allowed
	requestedStorage := options.PVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if requestedStorage.IsZero() {
		return nil, controller.ProvisioningFinished, fmt.Errorf("requested storage size is zero")
	}

	// If no access mode is specified, return an error
	if len(options.PVC.Spec.AccessModes) == 0 {
		return nil, controller.ProvisioningFinished, fmt.Errorf("access mode is not specified")
	}

	// Generate a unique name for the volume using the PVC namespace and name
	volumeName := fmt.Sprintf("pv-%s-%s", options.PVC.Namespace, options.PVC.Name)

	// Check if the volume already exists
	volumePath := "/tmp/dynamic-volumes/" + volumeName
	if _, err := os.Stat(volumePath); !os.IsNotExist(err) {
		return nil, controller.ProvisioningFinished, fmt.Errorf("volume %s already exists at %s", volumeName, volumePath)
	}

	// Create the volume directory
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return nil, controller.ProvisioningFinished, fmt.Errorf("failed to create volume directory: %v", err)
	}

	// Based on the above checks, we can now create the PV, HostPath is used as the volume source
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: volumeName,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: options.PVC.Spec.Resources.Requests[corev1.ResourceStorage],
			},
			AccessModes:                   options.PVC.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: volumePath,
				},
			},
		},
	}

	// Return the PV, ProvisioningFinished and nil error to indicate success
	klog.Infof("Successfully provisioned volume %s for PVC %s/%s", volumeName, options.PVC.Namespace, options.PVC.Name)
	return pv, controller.ProvisioningFinished, nil
}

func (p *customProvisioner) Delete(ctx context.Context, volume *corev1.PersistentVolume) error {
	// Validate whether the volume is a HostPath volume
	if volume.Spec.HostPath == nil {
		klog.Infof("Volume %s is not a HostPath volume, skipping deletion.", volume.Name)
		return nil
	}

	// Get the volume path
	volumePath := volume.Spec.HostPath.Path

	// Check if the volume path exists
	if _, err := os.Stat(volumePath); os.IsNotExist(err) {
		klog.Infof("Volume path %s does not exist, nothing to delete.", volumePath)
		return nil
	}

	// Delete the volume directory, using os.RemoveAll to delete the directory and its contents
	klog.Infof("Deleting volume %s at path %s", volume.Name, volumePath)
	if err := os.RemoveAll(volumePath); err != nil {
		klog.Errorf("Failed to delete volume %s at path %s: %v", volume.Name, volumePath, err)
		return err
	}

	klog.Infof("Successfully deleted volume %s at path %s", volume.Name, volumePath)
	return nil
}

func main() {
	// Use "InClusterConfig" to create a new clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create clientset: %v", err)
	}

	provisioner := NewCustomProvisioner(clientset)

	// Important!! Create a new ProvisionController instance and run it
	pc := controller.NewProvisionController(clientset, "custom-provisioner", provisioner, controller.LeaderElection(false))
	klog.Infof("Starting custom provisioner...")
	pc.Run(context.Background())
}
