/*
Copyright 2023

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	subv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/logger"
)

var log = logger.DefaultZapLogger()

// MergeObjects merge the desiredObj into the existingObj, then unmarshal to updatedObj
func MergeObjects(existingObj, desiredObj, updatedObj client.Object) error {
	existingJson, _ := json.Marshal(existingObj)
	desiredJson, _ := json.Marshal(desiredObj)

	// patch the desired json to the existing json
	patchedData, err := jsonpatch.MergePatch(existingJson, desiredJson)
	if err != nil {
		return err
	}
	err = json.Unmarshal(patchedData, updatedObj)
	if err != nil {
		return err
	}
	return nil
}

// Remove is used to remove string from a string array
func Remove(list []string, s string) []string {
	result := []string{}
	for _, v := range list {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

// Contains is used to check whether a list contains string s
func Contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// GetAnnotation returns the annotation value for a given key, or an empty string if not set
func GetAnnotation(annotations map[string]string, key string) string {
	if annotations == nil {
		return ""
	}
	return annotations[key]
}

func RemoveDuplicates(elements []string) []string {
	// Use map to record duplicates as we find them.
	encountered := map[string]struct{}{}
	result := []string{}

	for _, v := range elements {
		if _, found := encountered[v]; found {
			continue
		}
		encountered[v] = struct{}{}
		result = append(result, v)
	}
	// Return the new slice.
	return result
}

func UpdateObject(ctx context.Context, runtimeClient client.Client, obj client.Object) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return runtimeClient.Update(ctx, obj, &client.UpdateOptions{})
	})
}

// Finds subscription by name. Returns nil if none found.
func GetSubscriptionByName(ctx context.Context, k8sClient client.Client, namespace, name string) (
	*subv1alpha1.Subscription, error,
) {
	found := &subv1alpha1.Subscription{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return found, nil
}

// IsCommunityMode returns true if operator is running in community mode
func IsCommunityMode() bool {
	image := os.Getenv("RELATED_IMAGE_POSTGRESQL")
	if strings.Contains(image, "quay.io/stolostron") {
		// image has quay.io/stolostron treat as community version
		return true
	} else {
		return false
	}
}

func ApplyConfigMap(ctx context.Context, runtimeClient client.Client, required *corev1.ConfigMap) (bool, error) {
	curAlertConfigMap := &corev1.ConfigMap{}
	err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(required), curAlertConfigMap)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("creating configmap, namespace: %v, name: %v", required.Namespace, required.Name)
			err = runtimeClient.Create(ctx, required)
			if err != nil {
				return false, fmt.Errorf("failed to create alert configmap, namespace: %v, name: %v, error:%v",
					required.Namespace, required.Name, err)
			}
			return true, err
		}
		return false, nil
	}

	if reflect.DeepEqual(curAlertConfigMap.Data, required.Data) {
		return false, nil
	}

	log.Infof("Update alert configmap, namespace: %v, name: %v", required.Namespace, required.Name)
	curAlertConfigMap.Data = required.Data
	err = runtimeClient.Update(ctx, curAlertConfigMap)
	if err != nil {
		return false, fmt.Errorf("failed to update alert configmap, namespace: %v, name: %v, error:%v",
			required.Namespace, required.Name, err)
	}
	return true, nil
}

func ApplySecret(ctx context.Context, runtimeClient client.Client, requiredSecret *corev1.Secret) (bool, error) {
	currentSecret := &corev1.Secret{}
	err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(requiredSecret), currentSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("creating secret, namespace: %v, name: %v", requiredSecret.Namespace, requiredSecret.Name)
			err = runtimeClient.Create(ctx, requiredSecret)
			if err != nil {
				return false, fmt.Errorf("failed to create secret, namespace: %v, name: %v, error:%v",
					requiredSecret.Namespace, requiredSecret.Name, err)
			}
			return true, err
		}
		return false, nil
	}

	if reflect.DeepEqual(currentSecret.Data, requiredSecret.Data) {
		return false, nil
	}

	log.Infof("Update secret, namespace: %v, name: %v", requiredSecret.Namespace, requiredSecret.Name)
	currentSecret.Data = requiredSecret.Data
	err = runtimeClient.Update(ctx, currentSecret)
	if err != nil {
		return false, fmt.Errorf("failed to update secret, namespace: %v, name: %v, error:%v",
			requiredSecret.Namespace, requiredSecret.Name, err)
	}
	return true, nil
}

// getAlertGPCcount count the groupCount, policyCount, contactCount for the alert
func GetAlertGPCcount(a []byte) (int, int, int, error) {
	var o1 map[string]interface{}
	var groupCount, policyCount, contactCount int
	if len(a) == 0 {
		return groupCount, policyCount, contactCount, nil
	}
	if err := yaml.Unmarshal(a, &o1); err != nil {
		return groupCount, policyCount, contactCount, err
	}
	for k, v := range o1 {
		if !(k == "groups" || k == "policies" || k == "contactPoints") {
			continue
		}
		vArray, _ := v.([]interface{})
		if k == "groups" {
			groupCount = len(vArray)
		}
		if k == "policies" {
			policyCount = len(vArray)
		}
		if k == "contactPoints" {
			contactCount = len(vArray)
		}
	}
	return groupCount, policyCount, contactCount, nil
}

func IsAlertGPCcountEqual(a, b []byte) (bool, error) {
	ag, ap, ac, err := GetAlertGPCcount(a)
	if err != nil {
		return false, err
	}
	bg, bp, bc, err := GetAlertGPCcount(b)
	if err != nil {
		return false, err
	}
	if ag == bg && ap == bp && ac == bc {
		return true, nil
	}
	return false, nil
}

func CopyMap(newMap, originalMap map[string]string) {
	for key, value := range originalMap {
		newMap[key] = value
	}
}

func GetReadyMulticlusterGlobalHub(ctx context.Context, client client.Client) (*v1alpha4.MulticlusterGlobalHub, bool) {
	mgh := &v1alpha4.MulticlusterGlobalHub{}
	err := client.Get(ctx, config.GetMGHNamespacedName(), mgh)
	if errors.IsNotFound(err) {
		log.Debug("the mutliclusterglobalhub instance is not found")
		return nil, false
	} else if err != nil {
		log.Warnw("failed to get mutliclusterglobalhub instance", "error", err)
		return nil, false
	}

	if meta.IsStatusConditionTrue(mgh.Status.Conditions, config.CONDITION_TYPE_GLOBALHUB_READY) {
		return mgh, true
	}
	log.Debug("the mutliclusterglobalhub instance is not ready")
	return mgh, false
}

func GetResources(component string, advanced *v1alpha4.AdvancedSpec) *corev1.ResourceRequirements {
	resourceReq := corev1.ResourceRequirements{}
	requests := corev1.ResourceList{}

	switch component {
	case operatorconstants.Grafana:
		requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(operatorconstants.GrafanaMemoryRequest)
		requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(operatorconstants.GrafanaCPURequest)
		if advanced != nil && advanced.Grafana != nil {
			setResourcesFromCR(advanced.Grafana.Resources, requests)
		}

	case operatorconstants.Postgres:
		requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(operatorconstants.PostgresMemoryRequest)
		requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(operatorconstants.PostgresCPURequest)
		if advanced != nil && advanced.Postgres != nil {
			setResourcesFromCR(advanced.Postgres.Resources, requests)
		}

	case operatorconstants.Manager:
		requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(operatorconstants.ManagerMemoryRequest)
		requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(operatorconstants.ManagerCPURequest)
		if advanced != nil && advanced.Manager != nil {
			setResourcesFromCR(advanced.Manager.Resources, requests)
		}
	case operatorconstants.Agent:
		requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(operatorconstants.AgentMemoryRequest)
		requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(operatorconstants.AgentCPURequest)
		if advanced != nil && advanced.Agent != nil {
			setResourcesFromCR(advanced.Agent.Resources, requests)
		}
	case operatorconstants.Kafka:
		requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(operatorconstants.KafkaMemoryRequest)
		requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(operatorconstants.KafkaCPURequest)
		if advanced != nil && advanced.Kafka != nil {
			setResourcesFromCR(advanced.Kafka.Resources, requests)
		}
	}

	resourceReq.Requests = requests

	return &resourceReq
}

func setResourcesFromCR(res *v1alpha4.ResourceRequirements, requests corev1.ResourceList) {
	if res != nil {
		if res.Requests.Memory().String() != "0" {
			requests[corev1.ResourceName(corev1.ResourceMemory)] = resource.MustParse(res.Requests.Memory().String())
		}
		if res.Requests.Cpu().String() != "0" {
			requests[corev1.ResourceName(corev1.ResourceCPU)] = resource.MustParse(res.Requests.Cpu().String())
		}
	}
}

func RemoveManagedHubClusterFinalizer(ctx context.Context, c client.Client) error {
	clusters := &clusterv1.ManagedClusterList{}
	if err := c.List(ctx, clusters, &client.ListOptions{}); err != nil {
		return err
	}

	for idx := range clusters.Items {
		managedHub := &clusters.Items[idx]
		if managedHub.Name == constants.LocalClusterName {
			continue
		}

		if ok := controllerutil.RemoveFinalizer(managedHub, constants.GlobalHubCleanupFinalizer); ok {
			if err := c.Update(ctx, managedHub, &client.UpdateOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

// add addon.open-cluster-management.io/on-multicluster-hub annotation to the managed hub
// clusters indicate the addons are running on a hub cluster
func AnnotateManagedHubCluster(ctx context.Context, c client.Client) error {
	clusters := &clusterv1.ManagedClusterList{}
	if err := c.List(ctx, clusters, &client.ListOptions{}); err != nil {
		return err
	}

	globalHubAddons := &addonapiv1alpha1.ManagedClusterAddOnList{}
	if err := c.List(ctx, globalHubAddons, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(
			labels.Set{
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			}),
	}); err != nil {
		return err
	}

	for idx, managedHub := range clusters.Items {
		hasGlobalHubAgentInstalled := false
		for _, addon := range globalHubAddons.Items {
			if addon.GetNamespace() == managedHub.Name {
				hasGlobalHubAgentInstalled = true
				break
			}
		}
		if !hasGlobalHubAgentInstalled {
			continue
		}

		if managedHub.Name == constants.LocalClusterName {
			continue
		}
		orgAnnotations := managedHub.GetAnnotations()
		if orgAnnotations == nil {
			orgAnnotations = make(map[string]string)
		}
		annotations := make(map[string]string, len(orgAnnotations))
		CopyMap(annotations, managedHub.GetAnnotations())

		// set the annotations for the managed hub
		orgAnnotations[operatorconstants.AnnotationONMulticlusterHub] = "true"
		orgAnnotations[operatorconstants.AnnotationPolicyONMulticlusterHub] = "true"
		if !equality.Semantic.DeepEqual(annotations, orgAnnotations) {
			clusters.Items[idx].SetAnnotations(orgAnnotations)
			if err := c.Update(ctx, &clusters.Items[idx], &client.UpdateOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func TriggerManagedHubAddons(ctx context.Context, c client.Client, addonManager addonmanager.AddonManager) error {
	clusters := &clusterv1.ManagedClusterList{}
	if err := c.List(ctx, clusters, &client.ListOptions{}); err != nil {
		return err
	}

	for i := range clusters.Items {
		cluster := clusters.Items[i]
		if !FilterManagedCluster(&cluster) {
			addonManager.Trigger(cluster.Name, operatorconstants.GHClusterManagementAddonName)
		}
	}
	return nil
}

func FilterManagedCluster(obj client.Object) bool {
	return obj.GetLabels()["vendor"] != "OpenShift" ||
		obj.GetLabels()["openshiftVersion"] == "3" ||
		obj.GetName() == constants.LocalClusterName
}

// ManipulateGlobalHubObjects will attach the owner reference, add specific labels to these objects
func ManipulateGlobalHubObjects(objects []*unstructured.Unstructured,
	mgh *v1alpha4.MulticlusterGlobalHub, hohDeployer deployer.Deployer,
	mapper *restmapper.DeferredDiscoveryRESTMapper, scheme *runtime.Scheme,
) error {
	// manipulate the object
	for _, obj := range objects {
		mapping, err := mapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
		if err != nil {
			return err
		}

		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			// for namespaced resource, set ownerreference of controller
			if err := controllerutil.SetControllerReference(mgh, obj, scheme); err != nil {
				return err
			}
		}

		// set owner labels
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[constants.GlobalHubOwnerLabelKey] = constants.GHOperatorOwnerLabelVal
		obj.SetLabels(labels)

		if err := hohDeployer.Deploy(obj); err != nil {
			return err
		}
	}

	return nil
}

func PruneMetricsResources(ctx context.Context, c client.Client, metricsLabel map[string]string) error {
	listOpts := []client.ListOption{
		client.MatchingLabels(metricsLabel),
	}
	configmapList := &corev1.ConfigMapList{}
	if err := c.List(ctx, configmapList, listOpts...); err != nil {
		return err
	}
	for idx := range configmapList.Items {
		if err := c.Delete(ctx, &configmapList.Items[idx]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	serviceMonitorList := &promv1.ServiceMonitorList{}
	if err := c.List(ctx, serviceMonitorList, listOpts...); err != nil {
		// Handle the env do not have this kind of resource
		return nil
	}
	for idx := range serviceMonitorList.Items {
		if err := c.Delete(ctx, serviceMonitorList.Items[idx]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	podMonitorList := &promv1.PodMonitorList{}
	if err := c.List(ctx, podMonitorList, listOpts...); err != nil {
		// Handle the env do not have this kind of resource
		return nil
	}
	for idx := range podMonitorList.Items {
		if err := c.Delete(ctx, podMonitorList.Items[idx]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	prometheusRuleList := &promv1.PrometheusRuleList{}
	if err := c.List(ctx, prometheusRuleList, listOpts...); err != nil {
		// Handle the env do not have this kind of resource
		return nil
	}
	for idx := range prometheusRuleList.Items {
		if err := c.Delete(ctx, prometheusRuleList.Items[idx]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
