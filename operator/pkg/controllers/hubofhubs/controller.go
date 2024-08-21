/*
Copyright 2022.

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

package hubofhubs

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	imagev1 "github.com/openshift/api/image/v1"
	routev1 "github.com/openshift/api/route/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	subv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/grafana"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/manager"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/metrics"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/prune"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/status"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/storage"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/transporter"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/hubofhubs/transporter/protocol"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

// GlobalHubReconciler reconciles a MulticlusterGlobalHub object
type GlobalHubReconciler struct {
	config              *rest.Config
	client              client.Client
	recorder            record.EventRecorder
	scheme              *runtime.Scheme
	log                 logr.Logger
	upgraded            bool
	operatorConfig      *config.OperatorConfig
	pruneReconciler     *prune.PruneReconciler
	metricsReconciler   *metrics.MetricsReconciler
	storageReconciler   *storage.StorageReconciler
	transportReconciler *transporter.TransportReconciler
	statusReconciler    *status.StatusReconciler
	managerReconciler   *manager.ManagerReconciler
	grafanaReconciler   *grafana.GrafanaReconciler
	imageClient         *imagev1client.ImageV1Client
}

func NewGlobalHubReconciler(mgr ctrl.Manager, kubeClient kubernetes.Interface,
	operatorConfig *config.OperatorConfig, imageClient *imagev1client.ImageV1Client,
) *GlobalHubReconciler {
	return &GlobalHubReconciler{
		log:                 ctrl.Log.WithName(operatorconstants.GlobalHubControllerName),
		client:              mgr.GetClient(),
		config:              mgr.GetConfig(),
		scheme:              mgr.GetScheme(),
		recorder:            mgr.GetEventRecorderFor(operatorconstants.GlobalHubControllerName),
		operatorConfig:      operatorConfig,
		pruneReconciler:     prune.NewPruneReconciler(mgr.GetClient()),
		metricsReconciler:   metrics.NewMetricsReconciler(mgr.GetClient()),
		storageReconciler:   storage.NewStorageReconciler(mgr, operatorConfig.GlobalResourceEnabled),
		transportReconciler: transporter.NewTransportReconciler(mgr),
		statusReconciler:    status.NewStatusReconciler(mgr.GetClient()),
		managerReconciler:   manager.NewManagerReconciler(mgr, kubeClient, operatorConfig),
		grafanaReconciler:   grafana.NewGrafanaReconciler(mgr, kubeClient),
		imageClient:         imageClient,
	}
}

func NewGlobalHubController(mgr ctrl.Manager, kubeClient kubernetes.Interface,
	operatorConfig *config.OperatorConfig, imageClient *imagev1client.ImageV1Client,
) (controller.Controller, error) {
	globalHubController, err := controller.New(operatorconstants.GlobalHubControllerName, mgr, controller.Options{
		Reconciler: NewGlobalHubReconciler(mgr, kubeClient, operatorConfig, imageClient),
	})
	if err != nil {
		return nil, err
	}

	if err := addGlobalHubControllerWatches(mgr, globalHubController); err != nil {
		return nil, err
	}

	if err := addImageStreamWatch(mgr, globalHubController); err != nil {
		return nil, err
	}

	return globalHubController, nil
}

// addImageStreamWatch adds watch oauth-proxy imagestream
func addImageStreamWatch(mgr ctrl.Manager, globalHubController controller.Controller) error {
	if _, err := mgr.GetRESTMapper().KindFor(schema.GroupVersionResource{
		Group:    "image.openshift.io",
		Version:  "v1",
		Resource: "imagestreams",
	}); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}
	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &imagev1.ImageStream{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *imagev1.ImageStream,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchImageStreamPredict())); err != nil {
		return err
	}
	return nil
}

func watchImageStreamPredict() predicate.TypedPredicate[*imagev1.ImageStream] {
	return predicate.TypedFuncs[*imagev1.ImageStream]{
		CreateFunc: func(e event.TypedCreateEvent[*imagev1.ImageStream]) bool {
			return e.Object.GetName() == operatorconstants.OauthProxyImageStreamName
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*imagev1.ImageStream]) bool {
			if e.ObjectNew.GetName() != operatorconstants.OauthProxyImageStreamName {
				return false
			}
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*imagev1.ImageStream]) bool {
			return false
		},
	}
}

func addGlobalHubControllerWatches(mgr ctrl.Manager, globalHubController controller.Controller) error {
	schema := mgr.GetScheme()
	restMapper := mgr.GetClient().RESTMapper()

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &v1alpha4.MulticlusterGlobalHub{},
			&handler.TypedEnqueueRequestForObject[*v1alpha4.MulticlusterGlobalHub]{},
			watchMulticlusterGlobalHubPredict())); err != nil {
		return err
	}

	// Custom predicate to handle status changes
	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &appsv1.Deployment{},
			handler.TypedEnqueueRequestForOwner[*appsv1.Deployment](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*appsv1.Deployment]{
				predicate.TypedFuncs[*appsv1.Deployment]{
					CreateFunc: func(e event.TypedCreateEvent[*appsv1.Deployment]) bool {
						return true
					},
					// status changes
					UpdateFunc: func(e event.TypedUpdateEvent[*appsv1.Deployment]) bool {
						oldDeployment := e.ObjectOld
						newDeployment := e.ObjectNew
						return !equality.Semantic.DeepEqual(oldDeployment.Status, newDeployment.Status)
					},
					DeleteFunc: func(e event.TypedDeleteEvent[*appsv1.Deployment]) bool {
						return true
					},
				},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &appsv1.StatefulSet{},
			handler.TypedEnqueueRequestForOwner[*appsv1.StatefulSet](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*appsv1.StatefulSet]{
				predicate.TypedGenerationChangedPredicate[*appsv1.StatefulSet]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.Service{},
			handler.TypedEnqueueRequestForOwner[*corev1.Service](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*corev1.Service]{
				predicate.TypedGenerationChangedPredicate[*corev1.Service]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.ServiceAccount{},
			handler.TypedEnqueueRequestForOwner[*corev1.ServiceAccount](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*corev1.ServiceAccount]{
				predicate.TypedGenerationChangedPredicate[*corev1.ServiceAccount]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.Secret{},
			handler.TypedEnqueueRequestForOwner[*corev1.Secret](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*corev1.Secret]{
				predicate.TypedGenerationChangedPredicate[*corev1.Secret]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &rbacv1.Role{},
			handler.TypedEnqueueRequestForOwner[*rbacv1.Role](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*rbacv1.Role]{
				predicate.TypedGenerationChangedPredicate[*rbacv1.Role]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &rbacv1.RoleBinding{},
			handler.TypedEnqueueRequestForOwner[*rbacv1.RoleBinding](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*rbacv1.RoleBinding]{
				predicate.TypedGenerationChangedPredicate[*rbacv1.RoleBinding]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &routev1.Route{},
			handler.TypedEnqueueRequestForOwner[*routev1.Route](
				schema, restMapper, &v1alpha4.MulticlusterGlobalHub{}, handler.OnlyControllerOwner()),
			[]predicate.TypedPredicate[*routev1.Route]{
				predicate.TypedGenerationChangedPredicate[*routev1.Route]{},
			}...)); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.ConfigMap{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *corev1.ConfigMap,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchConfigMapPredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &admissionv1.MutatingWebhookConfiguration{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				a *admissionv1.MutatingWebhookConfiguration,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchMutatingWebhookConfigurationPredicate())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *corev1.Namespace,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchNamespacePredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &corev1.Secret{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *corev1.Secret,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchSecretPredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &rbacv1.ClusterRole{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *rbacv1.ClusterRole,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchClusterRolePredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &rbacv1.ClusterRoleBinding{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *rbacv1.ClusterRoleBinding,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchClusterRoleBindingPredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &promv1.ServiceMonitor{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *promv1.ServiceMonitor,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchServiceMonitorPredict())); err != nil {
		return err
	}

	if err := globalHubController.Watch(
		source.Kind(mgr.GetCache(), &subv1alpha1.Subscription{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context,
				c *subv1alpha1.Subscription,
			) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: config.GetMGHNamespacedName()}}
			}), watchSubscriptionPredict())); err != nil {
		return err
	}

	return nil
}

func watchServiceMonitorPredict() predicate.TypedPredicate[*promv1.ServiceMonitor] {
	return predicate.TypedFuncs[*promv1.ServiceMonitor]{
		CreateFunc: func(e event.TypedCreateEvent[*promv1.ServiceMonitor]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*promv1.ServiceMonitor]) bool {
			if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] !=
				constants.GHOperatorOwnerLabelVal {
				return false
			}
			// only requeue when spec change, if the resource do not have spec field, the generation is always 0
			if e.ObjectNew.GetGeneration() == 0 {
				return true
			}
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*promv1.ServiceMonitor]) bool {
			return e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal
		},
	}
}

func watchMulticlusterGlobalHubPredict() predicate.TypedPredicate[*v1alpha4.MulticlusterGlobalHub] {
	return predicate.TypedFuncs[*v1alpha4.MulticlusterGlobalHub]{
		CreateFunc: func(e event.TypedCreateEvent[*v1alpha4.MulticlusterGlobalHub]) bool {
			return true
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*v1alpha4.MulticlusterGlobalHub]) bool {
			return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*v1alpha4.MulticlusterGlobalHub]) bool {
			return !e.DeleteStateUnknown
		},
	}
}

func watchClusterRolePredict() predicate.TypedPredicate[*rbacv1.ClusterRole] {
	return predicate.TypedFuncs[*rbacv1.ClusterRole]{
		CreateFunc: func(e event.TypedCreateEvent[*rbacv1.ClusterRole]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*rbacv1.ClusterRole]) bool {
			if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] !=
				constants.GHOperatorOwnerLabelVal {
				return false
			}
			// only requeue when spec change, if the resource do not have spec field, the generation is always 0
			if e.ObjectNew.GetGeneration() == 0 {
				return true
			}
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*rbacv1.ClusterRole]) bool {
			return e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal
		},
	}
}

func watchClusterRoleBindingPredict() predicate.TypedPredicate[*rbacv1.ClusterRoleBinding] {
	return predicate.TypedFuncs[*rbacv1.ClusterRoleBinding]{
		CreateFunc: func(e event.TypedCreateEvent[*rbacv1.ClusterRoleBinding]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*rbacv1.ClusterRoleBinding]) bool {
			if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] !=
				constants.GHOperatorOwnerLabelVal {
				return false
			}
			// only requeue when spec change, if the resource do not have spec field, the generation is always 0
			if e.ObjectNew.GetGeneration() == 0 {
				return true
			}
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*rbacv1.ClusterRoleBinding]) bool {
			return e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal
		},
	}
}

func watchSubscriptionPredict() predicate.TypedPredicate[*subv1alpha1.Subscription] {
	return predicate.TypedFuncs[*subv1alpha1.Subscription]{
		CreateFunc: func(e event.TypedCreateEvent[*subv1alpha1.Subscription]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*subv1alpha1.Subscription]) bool {
			return false
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*subv1alpha1.Subscription]) bool {
			return true
		},
	}
}

func watchSecretPredict() predicate.TypedPredicate[*corev1.Secret] {
	secretCond := func(obj client.Object) bool {
		if WatchedSecret.Has(obj.GetName()) {
			return true
		}
		if obj.GetLabels()["strimzi.io/cluster"] == protocol.KafkaClusterName &&
			obj.GetLabels()["strimzi.io/kind"] == "KafkaUser" {
			return true
		}
		return false
	}
	return predicate.TypedFuncs[*corev1.Secret]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Secret]) bool {
			return secretCond(e.Object)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Secret]) bool {
			return secretCond(e.ObjectNew)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Secret]) bool {
			return secretCond(e.Object)
		},
	}
}

func watchConfigMapPredict() predicate.TypedPredicate[*corev1.ConfigMap] {
	return predicate.TypedFuncs[*corev1.ConfigMap]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.ConfigMap]) bool {
			return WatchedConfigMap.Has(e.Object.GetName())
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.ConfigMap]) bool {
			if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal {
				return true
			}
			return WatchedConfigMap.Has(e.ObjectNew.GetName())
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.ConfigMap]) bool {
			if e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal {
				return true
			}
			return WatchedConfigMap.Has(e.Object.GetName())
		},
	}
}

func watchNamespacePredict() predicate.TypedPredicate[*corev1.Namespace] {
	return predicate.TypedFuncs[*corev1.Namespace]{
		CreateFunc: func(e event.TypedCreateEvent[*corev1.Namespace]) bool {
			return e.Object.GetName() == config.GetMGHNamespacedName().Namespace
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Namespace]) bool {
			return e.ObjectNew.GetName() == config.GetMGHNamespacedName().Namespace &&
				e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*corev1.Namespace]) bool {
			return false
		},
	}
}

func watchMutatingWebhookConfigurationPredicate() predicate.TypedPredicate[*admissionv1.MutatingWebhookConfiguration] {
	return predicate.TypedFuncs[*admissionv1.MutatingWebhookConfiguration]{
		CreateFunc: func(e event.TypedCreateEvent[*admissionv1.MutatingWebhookConfiguration]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[*admissionv1.MutatingWebhookConfiguration]) bool {
			if e.ObjectNew.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal {
				if len(e.ObjectNew.Webhooks) != len(e.ObjectOld.Webhooks) ||
					e.ObjectNew.Webhooks[0].Name != e.ObjectOld.Webhooks[0].Name ||
					!reflect.DeepEqual(e.ObjectNew.Webhooks[0].AdmissionReviewVersions,
						e.ObjectOld.Webhooks[0].AdmissionReviewVersions) ||
					!reflect.DeepEqual(e.ObjectNew.Webhooks[0].Rules, e.ObjectOld.Webhooks[0].Rules) ||
					!reflect.DeepEqual(e.ObjectNew.Webhooks[0].ClientConfig.Service, e.ObjectOld.Webhooks[0].ClientConfig.Service) {
					return true
				}
				return false
			}
			return false
		},
		DeleteFunc: func(e event.TypedDeleteEvent[*admissionv1.MutatingWebhookConfiguration]) bool {
			return e.Object.GetLabels()[constants.GlobalHubOwnerLabelKey] ==
				constants.GHOperatorOwnerLabelVal
		},
	}
}

// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/join,verbs=create;delete
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets/bind,verbs=create;delete
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=subscriptions,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=placementrules,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=apps.open-cluster-management.io,resources=channels,verbs=get;list;update;patch
// +kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=placementbindings,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=app.k8s.io,resources=applications,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=placements,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclustersets,verbs=get;list;patch;update
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=create;delete;get;list;update;watch
// +kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterhubs,verbs=get;list;patch;update;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules;podmonitors,verbs=get;create;delete;update;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheuses/api,resourceNames=k8s,verbs=get;create;update
// +kubebuilder:rbac:groups=operators.coreos.com,resources=subscriptions,verbs=get;create;delete;update;list;watch
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=delete
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;create;list;watch
// +kubebuilder:rbac:groups=kafka.strimzi.io,resources=kafkas;kafkatopics;kafkausers,verbs=get;create;list;watch;update;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=image.openshift.io,resources=imagestreams,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Modify the Reconcile function to compare the state specified by
// the MulticlusterGlobalHub object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *GlobalHubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if len(req.Namespace) == 0 || len(req.Name) == 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	r.log.V(2).Info("reconciling mgh instance", "namespace", req.Namespace, "name", req.Name)
	mgh, err := config.GetMulticlusterGlobalHub(ctx, req, r.client, r.imageClient)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			r.log.Info("mgh instance not found", "namespace", req.Namespace, "name", req.Name)
			return ctrl.Result{}, nil
		}
		r.log.Error(err, "failed to get MulticlusterGlobalHub")
		return ctrl.Result{}, err
	}
	if config.IsPaused(mgh) {
		r.log.Info("mgh controller is paused, nothing more to do")
		return ctrl.Result{}, nil
	}

	// add finalizer to the mgh
	if controllerutil.AddFinalizer(mgh, constants.GlobalHubCleanupFinalizer) {
		if err = r.client.Update(ctx, mgh, &client.UpdateOptions{}); err != nil {
			if errors.IsConflict(err) {
				r.log.Info("conflict when adding finalizer to mgh instance", "error", err)
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	// update status condition
	defer func() {
		err := r.statusReconciler.Reconcile(ctx, mgh, err)
		if err != nil {
			r.log.Error(err, "failed to update the instance condition")
		}
	}()

	// prune resources if deleting mgh or metrics is disabled
	if err = r.pruneReconciler.Reconcile(ctx, mgh); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to prune Global Hub resources %v", err)
	}
	if mgh.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if config.IsACMResourceReady() {
		// update the managed hub clusters
		// only reconcile once: upgrade
		if !r.upgraded {
			if err = utils.RemoveManagedHubClusterFinalizer(ctx, r.client); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to upgrade from release-2.10: %v", err)
			}
			r.upgraded = true
		}
		if err := utils.AnnotateManagedHubCluster(ctx, r.client); err != nil {
			return ctrl.Result{}, err
		}
	}

	// storage and transporter
	if err = r.ReconcileMiddleware(ctx, mgh); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile metrics
	if err := r.metricsReconciler.Reconcile(ctx, mgh); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile manager
	if err := r.managerReconciler.Reconcile(ctx, mgh); err != nil {
		return ctrl.Result{}, err
	}

	if config.IsACMResourceReady() {
		// Grafana is required for ACM global hub
		// reconcile grafana
		if err := r.grafanaReconciler.Reconcile(ctx, mgh); err != nil {
			return ctrl.Result{}, err
		}
	}

	if config.IsACMResourceReady() && config.GetAddonManager() != nil {
		if err := utils.TriggerManagedHubAddons(ctx, r.client, config.GetAddonManager()); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ReconcileMiddleware creates the kafka and postgres if needed.
// 1. create the kafka and postgres subscription at the same time
// 2. then create the kafka and postgres resources at the same time
// 3. wait for kafka and postgres ready
func (r *GlobalHubReconciler) ReconcileMiddleware(ctx context.Context, mgh *v1alpha4.MulticlusterGlobalHub,
) error {
	if err := r.transportReconciler.Reconcile(ctx, mgh); err != nil {
		return err
	}

	if err := r.storageReconciler.Reconcile(ctx, mgh); err != nil {
		return err
	}
	return nil
}

var WatchedSecret = sets.NewString(
	constants.GHTransportSecretName,
	constants.GHStorageSecretName,
	constants.GHBuiltInStorageSecretName,
	config.PostgresCertName,
	constants.CustomGrafanaIniName,
	config.GetImagePullSecretName(),
)

var WatchedConfigMap = sets.NewString(
	constants.PostgresCAConfigMap,
	constants.CustomAlertName,
)
