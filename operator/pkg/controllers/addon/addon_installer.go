package addon

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	imageregistryv1alpha1 "github.com/stolostron/cluster-lifecycle-api/imageregistry/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	transportprotocol "github.com/stolostron/multicluster-global-hub/operator/pkg/transporter"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

type HoHAddonInstaller struct {
	client.Client
	Log logr.Logger
}

func (r *HoHAddonInstaller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mgh, err := utils.WaitGlobalHubReady(ctx, r, 5*time.Second)
	if err != nil {
		return ctrl.Result{}, err
	}
	if config.IsPaused(mgh) {
		r.Log.Info("multiclusterglobalhub addon installer is paused, nothing more to do")
		return ctrl.Result{}, nil
	}

	// wait the transport to be ready
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			if config.GetTransporter() == nil {
				r.Log.Info("waiting transport ready...")
				return false, nil
			}
			return true, nil
		})
	if err != nil {
		return ctrl.Result{}, err
	}
	transporter := config.GetTransporter()

	clusterManagementAddOn := &v1alpha1.ClusterManagementAddOn{}
	err = r.Get(ctx, types.NamespacedName{
		Name: operatorconstants.GHClusterManagementAddonName,
	}, clusterManagementAddOn)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("waiting until clustermanagementaddon is created", "namespacedname", req.NamespacedName)
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	if !clusterManagementAddOn.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.NamespacedName.Name,
		},
	}

	err = r.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	userName := transporter.GenerateUserName(cluster.Name)
	clusterTopic := transporter.GenerateClusterTopic(cluster.Name)

	if !cluster.DeletionTimestamp.IsZero() {
		r.Log.Info("cluster is deleting, delete the kafkaUser, skip addon deployment", "cluster", cluster.Name)
		config.DeleteManagedCluster(cluster.Name)
		return ctrl.Result{}, transporter.DeleteUser(userName)
	}
	config.AppendManagedCluster(cluster.Name)

	// create transport user
	err = transporter.CreateUser(userName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// create transport topic
	err = transporter.CreateTopic(clusterTopic)
	if err != nil {
		return ctrl.Result{}, err
	}

	addon := &v1alpha1.ManagedClusterAddOn{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: cluster.Name,
		Name:      operatorconstants.GHManagedClusterAddonName,
	}, addon)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if err == nil && !addon.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, transporter.DeleteUser(userName)
	}

	// create/update the addon
	expectedAddon := &v1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorconstants.GHManagedClusterAddonName,
			Namespace: cluster.Name,
			Labels: map[string]string{
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			},
		},
		Spec: v1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: constants.GHAgentNamespace,
		},
	}
	expectedAddonAnnotations := map[string]string{}

	deployMode := cluster.GetLabels()[operatorconstants.GHAgentDeployModeLabelKey]
	if deployMode == operatorconstants.GHAgentDeployModeHosted {
		annotations := cluster.GetAnnotations()
		if hostingCluster := annotations[operatorconstants.AnnotationClusterHostingClusterName]; hostingCluster != "" {
			expectedAddonAnnotations[operatorconstants.AnnotationAddonHostingClusterName] = hostingCluster
			expectedAddon.Spec.InstallNamespace = fmt.Sprintf("klusterlet-%s", cluster.Name)
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get hosting cluster name "+
				"when addon in %s is installed in hosted mode", cluster.Name)
		}
	}

	if val, ok := cluster.Annotations[imageregistryv1alpha1.ClusterImageRegistriesAnnotation]; ok {
		expectedAddonAnnotations[imageregistryv1alpha1.ClusterImageRegistriesAnnotation] = val
	}
	if len(expectedAddonAnnotations) > 0 {
		expectedAddon.SetAnnotations(expectedAddonAnnotations)
	}

	existingAddon := &v1alpha1.ManagedClusterAddOn{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: cluster.Name,
		Name:      operatorconstants.GHManagedClusterAddonName,
	}, existingAddon)
	if err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if deployMode == operatorconstants.GHAgentDeployModeNone {
			return ctrl.Result{}, nil
		}

		r.Log.Info("creating addon for cluster", "cluster", cluster.Name, "addon", expectedAddon.Name)
		return ctrl.Result{}, r.Create(ctx, expectedAddon)
	}

	if deployMode == operatorconstants.GHAgentDeployModeNone {
		return ctrl.Result{}, r.Delete(ctx, expectedAddon)
	}

	if !reflect.DeepEqual(expectedAddon.Annotations, existingAddon.Annotations) ||
		existingAddon.Spec.InstallNamespace != expectedAddon.Spec.InstallNamespace {
		existingAddon.SetAnnotations(expectedAddon.Annotations)
		existingAddon.Spec.InstallNamespace = expectedAddon.Spec.InstallNamespace
		r.Log.Info("updating addon for cluster", "cluster", cluster.Name, "addon", expectedAddon.Name)
		return ctrl.Result{}, r.Update(ctx, existingAddon)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HoHAddonInstaller) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	clusterPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return !filterManagedCluster(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if filterManagedCluster(e.ObjectNew) {
				return false
			}
			if e.ObjectNew.GetResourceVersion() == e.ObjectOld.GetResourceVersion() {
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return !filterManagedCluster(e.Object)
		},
	}

	addonPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectNew.GetName() != operatorconstants.GHManagedClusterAddonName {
				return false
			}
			if e.ObjectNew.GetGeneration() == e.ObjectOld.GetGeneration() {
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == operatorconstants.GHManagedClusterAddonName
		},
	}

	clusterManagementAddonPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == operatorconstants.GHManagedClusterAddonName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectNew.GetName() != operatorconstants.GHManagedClusterAddonName {
				return false
			}
			if e.ObjectNew.GetGeneration() == e.ObjectOld.GetGeneration() {
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == operatorconstants.GHManagedClusterAddonName
		},
	}

	secretCond := func(obj client.Object) bool {
		if obj.GetName() == config.GetImagePullSecretName() ||
			obj.GetName() == constants.GHTransportSecretName ||
			obj.GetLabels() != nil && obj.GetLabels()["strimzi.io/cluster"] == transportprotocol.KafkaClusterName &&
				obj.GetLabels()["strimzi.io/kind"] == "KafkaUser" {
			return true
		}
		return false
	}
	secretPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return secretCond(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return secretCond(e.ObjectNew) && e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		// primary watch for managedcluster
		For(&clusterv1.ManagedCluster{}, builder.WithPredicates(clusterPred)).
		// secondary watch for managedclusteraddon
		Watches(&v1alpha1.ManagedClusterAddOn{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{
					// only trigger the addon reconcile when addon is updated/deleted
					{NamespacedName: types.NamespacedName{
						Name: obj.GetNamespace(),
					}},
				}
			}), builder.WithPredicates(addonPred)).
		// secondary watch for managedclusteraddon
		Watches(&v1alpha1.ClusterManagementAddOn{},
			handler.EnqueueRequestsFromMapFunc(r.renderAllManifestsHandler),
			builder.WithPredicates(clusterManagementAddonPred)).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.renderAllManifestsHandler),
			builder.WithPredicates(secretPred)).
		Complete(r)
}

func (r *HoHAddonInstaller) renderAllManifestsHandler(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	requests := []reconcile.Request{}
	// list all the managedCluster
	managedClusterList := &clusterv1.ManagedClusterList{}
	err := r.List(ctx, managedClusterList)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("no managed cluster found to trigger addoninstall reconciler")
			return requests
		}
		r.Log.Error(err, "failed to list managed clusters to trigger addoninstall reconciler")
		return requests
	}

	for i := range managedClusterList.Items {
		managedCluster := managedClusterList.Items[i]
		if filterManagedCluster(&managedCluster) {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: managedCluster.GetName(),
			},
		})
	}
	r.Log.Info("triggers addoninstall reconciler for all managed clusters", "requests", len(requests))
	return requests
}

func filterManagedCluster(obj client.Object) bool {
	return obj.GetLabels()["vendor"] != "OpenShift" ||
		obj.GetLabels()["openshiftVersion"] == "3" ||
		obj.GetName() == operatorconstants.LocalClusterName
}
