// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package utils

import (
	"context"
	"fmt"
	"strings"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog"
	"sigs.k8s.io/kustomize/api/filters/namespace"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"
)

// Options ...
type RenderOptions struct {
	KustomizationPath string
	OutputPath        string
	Namespace         string
}

// Render is used to render the kustomization
func Render(o RenderOptions) ([]byte, error) {
	fSys := filesys.MakeFsOnDisk()
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	m, err := k.Run(fSys, o.KustomizationPath)
	if err != nil {
		return nil, err
	}
	err = m.ApplyFilter(namespace.Filter{
		Namespace: o.Namespace,
	})
	if err != nil {
		return nil, err
	}
	return m.AsYaml()
}

// GetLabels return labels
func GetLabels(yamlB []byte) (interface{}, error) {
	data := map[string]interface{}{}
	err := yaml.Unmarshal(yamlB, &data)
	return data["metadata"].(map[string]interface{})["labels"], err
}

// Apply a multi resources file to the cluster described by the url, kubeconfig and ctx.
// url of the cluster
// kubeconfig which contains the ctx
// ctx, the ctx to use
// yamlB, a byte array containing the resources file
func Apply(testClients TestClient, testOptions Options, o RenderOptions) error {
	bytes, err := Render(o)
	if err != nil {
		return err
	}
	yamls := strings.Split(string(bytes), "---\n")
	// yamlFiles is an []string
	for _, tf := range yamls {
		if len(strings.TrimSpace(tf)) == 0 {
			continue
		}
		klog.Errorf("###########:%v", tf)

		targetNs := fmt.Sprintf("namespace: %s", testOptions.GlobalHub.Namespace)
		targetOperatorGroupNs := fmt.Sprintf("- %s\n", testOptions.GlobalHub.Namespace)
		of := strings.ReplaceAll(tf, "namespace: multicluster-global-hub", targetNs)
		f := strings.ReplaceAll(of, "- multicluster-global-hub\n", targetOperatorGroupNs)
		klog.Errorf("###########:%v", testOptions.GlobalHub.Namespace)
		klog.Errorf("###########:%v", f)

		obj := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(f), obj)
		if err != nil {
			klog.V(6).Infof("unmarshal %v is wrong", f)
			return err
		}
		var kind string
		if v, ok := obj.Object["kind"]; !ok {
			return fmt.Errorf("kind attribute not found in %s", f)
		} else {
			kind = v.(string)
		}

		klog.V(7).Infof("kind: %s\n", kind)

		var apiVersion string
		if v, ok := obj.Object["apiVersion"]; !ok {
			return fmt.Errorf("apiVersion attribute not found in %s", f)
		} else {
			apiVersion = v.(string)
		}
		klog.V(7).Infof("apiVersion: %s\n", apiVersion)

		clientKube := testClients.KubeClient()
		dynamicClient := testClients.KubeDynamicClient()
		clientAPIExtension := testClients.APIExtensionClient()
		// now use switch over the type of the object
		// and match each type-case
		switch kind {
		case "CustomResourceDefinition":
			klog.V(7).Infof("Install CRD: %s\n", f)
			obj := &apiextensionsv1.CustomResourceDefinition{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientAPIExtension.ApiextensionsV1().
				CustomResourceDefinitions().
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientAPIExtension.ApiextensionsV1().
					CustomResourceDefinitions().
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				existingObject.Spec = obj.Spec
				klog.Warningf("CRD %s already exists, updating!", existingObject.Name)
				_, err = clientAPIExtension.ApiextensionsV1().CustomResourceDefinitions().Update(context.TODO(), existingObject, metav1.UpdateOptions{})
			}
		case "Namespace":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.Namespace{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				Namespaces().
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().Namespaces().Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s already exists, updating!", obj.Kind, obj.Name)
				_, err = clientKube.CoreV1().Namespaces().Update(context.TODO(), existingObject, metav1.UpdateOptions{})
			}
		case "ServiceAccount":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.ServiceAccount{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				ServiceAccounts(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					ServiceAccounts(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().ServiceAccounts(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "ClusterRole":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &rbacv1.ClusterRole{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.RbacV1().
				ClusterRoles().
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.RbacV1().ClusterRoles().Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.RbacV1().ClusterRoles().Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "ClusterRoleBinding":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &rbacv1.ClusterRoleBinding{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.RbacV1().
				ClusterRoleBindings().
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.RbacV1().ClusterRoleBindings().Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.RbacV1().ClusterRoleBindings().Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "Role":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &rbacv1.Role{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.RbacV1().
				Roles(obj.GetNamespace()).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.RbacV1().Roles(obj.GetNamespace()).Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.RbacV1().Roles(obj.GetNamespace()).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "RoleBinding":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &rbacv1.RoleBinding{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.RbacV1().
				RoleBindings(obj.GetNamespace()).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.RbacV1().RoleBindings(obj.GetNamespace()).Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.RbacV1().RoleBindings(obj.GetNamespace()).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "Secret":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.Secret{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				Secrets(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().Secrets(obj.Namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().Secrets(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "ConfigMap":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.ConfigMap{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				ConfigMaps(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					ConfigMaps(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().ConfigMaps(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "Service":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.Service{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				Services(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					Services(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				obj.Spec.ClusterIP = existingObject.Spec.ClusterIP
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().Services(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "PersistentVolumeClaim":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.PersistentVolumeClaim{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				PersistentVolumeClaims(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					PersistentVolumeClaims(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				obj.Spec.VolumeName = existingObject.Spec.VolumeName
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().PersistentVolumeClaims(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "Deployment":
			obj := &appsv1.Deployment{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			// replace images
			obj.Spec.Template.Spec.Containers[0].Image = testOptions.GlobalHub.OperatorImageREF
			container := obj.Spec.Template.Spec.Containers[0]
			for i, env := range container.Env {
				if env.Name == "RELATED_IMAGE_MULTICLUSTER_GLOBAL_HUB_AGENT" {
					container.Env[i].Value = testOptions.GlobalHub.AgentImageREF
				}
				if env.Name == "RELATED_IMAGE_MULTICLUSTER_GLOBAL_HUB_MANAGER" {
					container.Env[i].Value = testOptions.GlobalHub.ManagerImageREF
				}
			}
			klog.V(7).Infof("Install %s: %v\n", kind, obj)

			existingObject, errGet := clientKube.AppsV1().
				Deployments(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.AppsV1().
					Deployments(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.AppsV1().Deployments(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "LimitRange":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.LimitRange{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				LimitRanges(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					LimitRanges(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().LimitRanges(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "ResourceQuota":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &corev1.ResourceQuota{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.CoreV1().
				ResourceQuotas(obj.Namespace).
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.CoreV1().
					ResourceQuotas(obj.Namespace).
					Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.CoreV1().ResourceQuotas(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "StorageClass":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &storagev1.StorageClass{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := clientKube.StorageV1().
				StorageClasses().
				Get(context.TODO(), obj.Name, metav1.GetOptions{})
			if errGet != nil {
				_, err = clientKube.StorageV1().StorageClasses().Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.ObjectMeta = existingObject.ObjectMeta
				klog.Warningf("%s %s/%s already exists, updating!", obj.Kind, obj.Namespace, obj.Name)
				_, err = clientKube.StorageV1().StorageClasses().Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		case "OperatorGroup":
			klog.V(7).Infof("Install %s: %s\n", kind, f)
			obj := &unstructured.Unstructured{}
			err = yaml.Unmarshal([]byte(f), obj)
			if err != nil {
				return err
			}
			existingObject, errGet := dynamicClient.Resource(operatorsv1.GroupVersion.WithResource("operatorgroups")).
				Namespace(obj.GetNamespace()).Get(context.TODO(), obj.GetName(), metav1.GetOptions{})
			if errGet != nil {
				_, err = dynamicClient.Resource(operatorsv1.GroupVersion.WithResource("operatorgroups")).
					Namespace(obj.GetNamespace()).Create(context.TODO(), obj, metav1.CreateOptions{})
			} else {
				obj.SetResourceVersion(existingObject.GetResourceVersion())
				klog.Warningf("%s %s/%s already exists, updating!", obj.GetKind(), obj.GetNamespace(), obj.GetName())
				_, err = dynamicClient.Resource(operatorsv1.GroupVersion.WithResource("operatorgroups")).
					Namespace(obj.GetNamespace()).Update(context.TODO(), obj, metav1.UpdateOptions{})
			}
		default:
			return fmt.Errorf("resource %s not supported", kind)
		}
		if err != nil {
			return err
		}

	}
	return nil
}
