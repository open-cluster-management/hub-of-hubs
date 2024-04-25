package addon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/api/addon/v1alpha1"
	v1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/condition"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	hubofhubsaddon "github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/addon"
	transportprotocol "github.com/stolostron/multicluster-global-hub/operator/pkg/transporter"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

var kubeCfg *rest.Config

func TestMain(m *testing.M) {
	// start testEnv
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "pkg", "testdata", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	kubeCfg, err = testEnv.Start()
	if err != nil {
		panic(err)
	}

	// run testings
	code := m.Run()

	// stop testEnv
	err = testEnv.Stop()
	if err != nil {
		panic(err)
	}

	os.Exit(code)
}

func fakeCluster(name, hostingCluster, addonDeployMode string) *v1.ManagedCluster {
	cluster := &v1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ManagedClusterSpec{},
	}
	labels := map[string]string{
		operatorconstants.GHAgentDeployModeLabelKey: addonDeployMode,
	}
	cluster.SetLabels(labels)

	if hostingCluster != "" {
		annotations := map[string]string{
			operatorconstants.AnnotationClusterDeployMode:         operatorconstants.ClusterDeployModeHosted,
			operatorconstants.AnnotationClusterHostingClusterName: hostingCluster,
		}
		cluster.SetAnnotations(annotations)
	}

	return cluster
}

func fakeHoHManagementAddon() *v1alpha1.ClusterManagementAddOn {
	return &v1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorconstants.GHClusterManagementAddonName,
			Labels: map[string]string{
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			},
		},
	}
}

func fakeMGH(namespace, name string) *operatorv1alpha4.MulticlusterGlobalHub {
	return &operatorv1alpha4.MulticlusterGlobalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: operatorv1alpha4.MulticlusterGlobalHubStatus{
			Conditions: []metav1.Condition{
				{
					Type:   condition.CONDITION_TYPE_GLOBALHUB_READY,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

func fakeHoHAddon(cluster, installNamespace, addonDeployMode string) *v1alpha1.ManagedClusterAddOn {
	addon := &v1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorconstants.GHManagedClusterAddonName,
			Namespace: cluster,
		},
		Spec: v1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: installNamespace,
		},
	}

	if addonDeployMode == operatorconstants.ClusterDeployModeHosted {
		addon.SetAnnotations(map[string]string{operatorconstants.AnnotationAddonHostingClusterName: "hostingcluster"})
	}

	return addon
}

func TestHoHAddonReconciler(t *testing.T) {
	namespace := "default"
	name := "test"
	config.SetMGHNamespacedName(types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	})

	cases := []struct {
		name            string
		cluster         *v1.ManagedCluster
		managementAddon *v1alpha1.ClusterManagementAddOn
		mgh             *operatorv1alpha4.MulticlusterGlobalHub
		addon           *v1alpha1.ManagedClusterAddOn
		req             reconcile.Request
		validateFunc    func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error)
	}{
		{
			name:            "clustermanagementaddon not ready",
			mgh:             fakeMGH(namespace, name),
			cluster:         fakeCluster("cluster1", "", operatorconstants.GHAgentDeployModeDefault),
			managementAddon: nil,
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster1"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if !errors.IsNotFound(err) {
					t.Errorf("expected not found addon, but got err %v", err)
				}
				if addon != nil {
					t.Errorf("expected nil addon, but got %v", addon)
				}
			},
		},
		{
			name:            "req not found",
			mgh:             fakeMGH(namespace, name),
			cluster:         fakeCluster("cluster1", "", operatorconstants.GHAgentDeployModeDefault),
			managementAddon: fakeHoHManagementAddon(),
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster2"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if !errors.IsNotFound(err) {
					t.Errorf("expected not found addon, but got err %v", err)
				}
				if addon != nil {
					t.Errorf("expected nil addon, but got %v", addon)
				}
			},
		},
		{
			name:            "do not create addon",
			mgh:             fakeMGH(namespace, name),
			cluster:         fakeCluster("cluster1", "", operatorconstants.GHAgentDeployModeNone),
			managementAddon: fakeHoHManagementAddon(),
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster1"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if !errors.IsNotFound(err) {
					t.Errorf("expected not found addon, but got err %v", err)
				}
				if addon != nil {
					t.Errorf("expected nil addon, but got %v", addon)
				}
			},
		},
		{
			name:            "create addon in default mode",
			mgh:             fakeMGH(namespace, name),
			cluster:         fakeCluster("cluster1", "", operatorconstants.GHAgentDeployModeDefault),
			managementAddon: fakeHoHManagementAddon(),
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster1"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if err != nil {
					t.Errorf("failed to reconcile .%v", err)
				}
				if addon.Spec.InstallNamespace != constants.GHAgentNamespace {
					t.Errorf("expected install name %s, but got %s",
						operatorconstants.GHAgentInstallNamespace, addon.Spec.InstallNamespace)
				}
			},
		},
		{
			name: "create addon in hosted mode",
			cluster: fakeCluster("cluster1", "cluster2",
				operatorconstants.GHAgentDeployModeHosted),
			managementAddon: fakeHoHManagementAddon(),
			mgh:             fakeMGH(namespace, name),
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster1"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if err != nil {
					t.Errorf("failed to reconcile .%v", err)
				}
				if addon.Spec.InstallNamespace != "klusterlet-cluster1" {
					t.Errorf("expected installname klusterlet-cluster1, but got %s", addon.Spec.InstallNamespace)
				}
				if addon.Annotations[operatorconstants.AnnotationAddonHostingClusterName] != "cluster2" {
					t.Errorf("expected hosting cluster cluster2, but got %s",
						addon.Annotations[operatorconstants.AnnotationAddonHostingClusterName])
				}
			},
		},
		{
			name: "update addon in hosted mode",
			cluster: fakeCluster("cluster1", "cluster2",
				operatorconstants.GHAgentDeployModeHosted),
			managementAddon: fakeHoHManagementAddon(),
			mgh:             fakeMGH(namespace, name),
			addon:           fakeHoHAddon("cluster1", "test", ""),
			req:             reconcile.Request{NamespacedName: types.NamespacedName{Name: "cluster1"}},
			validateFunc: func(t *testing.T, addon *v1alpha1.ManagedClusterAddOn, err error) {
				if err != nil {
					t.Errorf("failed to reconcile: %v", err)
				}
				if addon.Spec.InstallNamespace != "klusterlet-cluster1" {
					t.Errorf("expected installname klusterlet-cluster1, but got %s", addon.Spec.InstallNamespace)
				}
				if addon.Annotations[operatorconstants.AnnotationAddonHostingClusterName] != "cluster2" {
					t.Errorf("expected hosting cluster cluster2, but got %s",
						addon.Annotations[operatorconstants.AnnotationAddonHostingClusterName])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objects := []client.Object{tc.cluster}
			if tc.managementAddon != nil {
				objects = append(objects, tc.managementAddon)
			}
			if tc.addon != nil {
				objects = append(objects, tc.addon)
			}
			if tc.mgh != nil {
				objects = append(objects, tc.mgh)
				config.SetMGHNamespacedName(types.NamespacedName{
					Namespace: tc.mgh.Namespace, Name: tc.mgh.Name,
				})
			} else {
				config.SetMGHNamespacedName(types.NamespacedName{Namespace: "", Name: ""})
			}
			mgr, err := ctrl.NewManager(kubeCfg, ctrl.Options{
				Scheme: config.GetRuntimeScheme(),
				Metrics: metricsserver.Options{
					BindAddress: "0", // disable the metrics serving
				},
				NewCache: config.InitCache,
			})
			if err != nil {
				t.Errorf("failed to create manager: %v", err)
			}
			if mgr == nil {
				t.Error("the mgr shouldn't be nil")
			}
			objects = append(objects, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-kafka-user", tc.cluster.Name),
					Namespace: tc.mgh.Namespace,
					Labels: map[string]string{
						constants.GlobalHubOwnerLabelKey: constants.GlobalHubAddonOwnerLabelVal,
					},
				},
			})

			transporter := transportprotocol.NewBYOTransporter(ctx, types.NamespacedName{
				Namespace: tc.mgh.Namespace,
				Name:      constants.GHTransportSecretName,
			}, k8sClient)
			config.SetTransporter(transporter)

			r := &hubofhubsaddon.AddonInstaller{
				Client: fake.NewClientBuilder().WithScheme(mgr.GetScheme()).WithObjects(objects...).Build(),
				Log:    ctrl.Log.WithName("test"),
			}
			err = r.SetupWithManager(ctx, mgr)
			if err != nil {
				t.Errorf("failed to setup addon install controller with manager: %v", err)
			}

			_, err = r.Reconcile(context.TODO(), tc.req)
			for err != nil && strings.Contains(err.Error(), "object was modified") {
				fmt.Println("error message:", err.Error())
				_, err = r.Reconcile(context.TODO(), tc.req)
			}

			if err != nil {
				tc.validateFunc(t, nil, err)
			} else {
				addon := &v1alpha1.ManagedClusterAddOn{}
				err = r.Get(context.TODO(), types.NamespacedName{
					Namespace: tc.cluster.Name, Name: operatorconstants.GHManagedClusterAddonName,
				}, addon)
				if err != nil {
					if errors.IsNotFound(err) {
						tc.validateFunc(t, nil, err)
					} else {
						t.Errorf("failed to get addon %s", tc.cluster.Name)
					}
				} else {
					tc.validateFunc(t, addon, nil)
				}
			}
		})
	}
}
