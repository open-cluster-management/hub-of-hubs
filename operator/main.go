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

package main

import (
	"flag"
	"os"

	operatorsv1 "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apis/operators/v1"
	hypershiftdeploymentv1alpha1 "github.com/stolostron/hypershift-deployment-controller/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/stolostron/multicluster-globalhub/operator/apis/operator/v1alpha1"
	"github.com/stolostron/multicluster-globalhub/operator/pkg/constants"
	hubofhubscontrollers "github.com/stolostron/multicluster-globalhub/operator/pkg/controllers/hubofhubs"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorsv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	utilruntime.Must(workv1.AddToScheme(scheme))
	utilruntime.Must(addonv1alpha1.AddToScheme(scheme))
	utilruntime.Must(hypershiftdeploymentv1alpha1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// build filtered resource map
	newCacheFunc := cache.BuilderWithOptions(cache.Options{
		SelectorsByObject: cache.SelectorsByObject{
			&corev1.Secret{}: { // also cache transport-secret and storage-secret
				Field: fields.SelectorFromSet(fields.Set{"metadata.namespace": constants.HOHDefaultNamespace}),
			},
			&corev1.ConfigMap{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&corev1.ServiceAccount{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&corev1.Service{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&appsv1.Deployment{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&batchv1.Job{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&rbacv1.Role{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&rbacv1.RoleBinding{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&rbacv1.ClusterRole{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&rbacv1.ClusterRoleBinding{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&networkingv1.Ingress{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&networkingv1.NetworkPolicy{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&clusterv1beta1.ManagedClusterSetBinding{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&clusterv1beta1.Placement{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&clusterv1.ManagedCluster{}: {
				Label: labels.SelectorFromSet(labels.Set{"vendor": "OpenShift"}),
			},
			&workv1.ManifestWork{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&addonv1alpha1.ClusterManagementAddOn{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
			&addonv1alpha1.ManagedClusterAddOn{}: {
				Label: labels.SelectorFromSet(labels.Set{constants.HoHOperatorOwnerLabelKey: constants.HoHOperatorOwnerLabelVal}),
			},
		},
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "549a8919.open-cluster-management.io",
		NewCache:               newCacheFunc,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&hubofhubscontrollers.MultiClusterGlobalHubReconciler{
		Manager: mgr,
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MultiClusterGlobalHub")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
