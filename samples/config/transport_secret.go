package config

import (
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func GetTransportSecret() (*v1.Secret, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("failed to get kubeconfig")
	}
	namespace := os.Getenv("SECRET_NAMESPACE")
	if namespace == "" {
		namespace = "multicluster-global-hub"
	}
	name := os.Getenv("SECRET_NAME")
	if name == "" {
		name = "multicluster-global-hub-transport"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient %v", err)
	}
	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %v", err)
	}
	return secret, nil
}
