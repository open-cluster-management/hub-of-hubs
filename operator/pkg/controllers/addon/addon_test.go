package addon_test

import (
	"context"
	"embed"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha2 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha2"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/controllers/addon"
	globalconstants "github.com/stolostron/multicluster-global-hub/pkg/constants"
	commonobjects "github.com/stolostron/multicluster-global-hub/pkg/objects"
	mchv1 "github.com/stolostron/multiclusterhub-operator/api/v1"
)

//go:embed manifests/templates
//go:embed manifests/templates/agent
//go:embed manifests/templates/hostedagent
//go:embed manifests/templates/hubcluster
var FS embed.FS

func fakeMulticlusterGlobalHub() *v1alpha2.MulticlusterGlobalHub {
	return &v1alpha2.MulticlusterGlobalHub{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multiclusterglobalhub",
			Namespace: config.GetDefaultNamespace(),
		},
		Spec: v1alpha2.MulticlusterGlobalHubSpec{
			DataLayer: &v1alpha2.DataLayerConfig{
				Type: v1alpha2.LargeScale,
				LargeScale: &v1alpha2.LargeScaleConfig{
					Kafka: corev1.LocalObjectReference{
						Name: "transport-secret",
					},
					Postgres: corev1.LocalObjectReference{
						Name: "storage-secret",
					},
				},
			},
		},
	}
}

func fakeKafkaSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "transport-secret",
			Namespace: config.GetDefaultNamespace(),
		},
		Data: map[string][]byte{
			"CA":               []byte("dGVzdAo="),
			"bootstrap_server": []byte("dGVzdAo="),
		},
		Type: corev1.SecretTypeOpaque,
	}
}

func fakePullSecret() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.DefaultImagePullSecretName,
			Namespace: config.GetDefaultNamespace(),
		},
		Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte("dGVzdAo=")},
		Type: corev1.SecretTypeDockerConfigJson,
	}
}

func fakeVersionClaim(version string) clusterv1.ManagedClusterClaim {
	return clusterv1.ManagedClusterClaim{
		Name:  globalconstants.VersionClusterClaimName,
		Value: version,
	}
}

func fakeManagedCluster(name string, claim clusterv1.ManagedClusterClaim) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: clusterv1.ManagedClusterSpec{},
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{claim},
		},
	}
}

func fakeManagedClusterAddon(clusterName, installNamespace string, installMode string) *v1alpha1.ManagedClusterAddOn {
	addon := &v1alpha1.ManagedClusterAddOn{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.HoHManagedClusterAddonName,
			Namespace: clusterName,
		},
		Spec: v1alpha1.ManagedClusterAddOnSpec{},
	}
	if installNamespace != "" {
		addon.Spec.InstallNamespace = installNamespace
	}
	switch installMode {
	case constants.ClusterDeployModeHosted:
		addon.SetAnnotations(map[string]string{"addon.open-cluster-management.io/hosting-cluster-name": "hostingCluster"})
	}

	return addon
}

func fakeLeaderElectionConfig() *commonobjects.LeaderElectionConfig {
	return &commonobjects.LeaderElectionConfig{
		LeaseDuration: 137,
		RenewDeadline: 107,
		RetryPeriod:   26,
	}
}

func fakeAgentAddon(t *testing.T, objects ...runtime.Object) agent.AgentAddon {
	hohAgentAddon := addon.NewHohAgentAddon(context.TODO(),
		fake.NewClientBuilder().WithScheme(testScheme).WithObjects(fakeMulticlusterGlobalHub()).Build(),
		kubefake.NewSimpleClientset(objects...),
		fakeLeaderElectionConfig(),
	)
	agentAddon, err := addonfactory.NewAgentAddonFactory(constants.HoHManagedClusterAddonName, FS, "manifests/templates").
		WithGetValuesFuncs(hohAgentAddon.GetValues).WithScheme(testScheme).BuildTemplateAgentAddon()
	if err != nil {
		t.Fatalf("failed to create agent addon. err:%v", err)
	}
	return agentAddon
}

var testScheme = scheme.Scheme

func init() {
	utilruntime.Must(mchv1.AddToScheme(testScheme))
	utilruntime.Must(v1alpha2.AddToScheme(testScheme))
	utilruntime.Must(operatorsv1.AddToScheme(testScheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(testScheme))
}

func TestManifest(t *testing.T) {
	tests := []struct {
		name                     string
		existingObjects          []runtime.Object
		cluster                  *clusterv1.ManagedCluster
		addon                    *v1alpha1.ManagedClusterAddOn
		expectedCount            int
		expectedInstallNamespace string
	}{
		{
			name:                     "install agent in default mode",
			existingObjects:          []runtime.Object{fakeKafkaSecret()},
			cluster:                  fakeManagedCluster("cluster1", clusterv1.ManagedClusterClaim{}),
			addon:                    fakeManagedClusterAddon("cluster1", "", constants.ClusterDeployModeDefault),
			expectedCount:            6,
			expectedInstallNamespace: constants.HoHAgentInstallNamespace,
		},
		{
			name:                     "install agent in hosted mode",
			existingObjects:          []runtime.Object{fakeKafkaSecret()},
			cluster:                  fakeManagedCluster("cluster1", clusterv1.ManagedClusterClaim{}),
			addon:                    fakeManagedClusterAddon("cluster1", "hoh-agent-addon", constants.ClusterDeployModeHosted),
			expectedCount:            10,
			expectedInstallNamespace: "hoh-agent-addon",
		},
		{
			name:            "install agent and acm in default mode without pullsecret",
			existingObjects: []runtime.Object{fakeKafkaSecret()},
			// only install acm when the version claim has empty value
			cluster:                  fakeManagedCluster("cluster1", fakeVersionClaim("")),
			addon:                    fakeManagedClusterAddon("cluster1", "addon-test", constants.ClusterDeployModeDefault),
			expectedCount:            12,
			expectedInstallNamespace: "addon-test",
		},
		{
			name:            "install agent and acm in default mode with pullsecret",
			existingObjects: []runtime.Object{fakeKafkaSecret(), fakePullSecret()},
			// only install acm when the version claim has empty value
			cluster:                  fakeManagedCluster("cluster1", fakeVersionClaim("")),
			addon:                    fakeManagedClusterAddon("cluster1", "", constants.ClusterDeployModeDefault),
			expectedCount:            13,
			expectedInstallNamespace: constants.HoHAgentInstallNamespace,
		},
		{
			name:            "install agent in hosted mode and acm in default mode with pullsecret",
			existingObjects: []runtime.Object{fakeKafkaSecret(), fakePullSecret()},
			// only install acm when the version claim has empty value
			cluster:                  fakeManagedCluster("cluster1", fakeVersionClaim("")),
			addon:                    fakeManagedClusterAddon("cluster1", "hoh-agent-addon", constants.ClusterDeployModeHosted),
			expectedCount:            17,
			expectedInstallNamespace: "hoh-agent-addon",
		},
	}

	addon.SetPackageManifestConfig("release-2.6", "advanced-cluster-management.v2.6.0",
		"stable-2.0", "multicluster-engine.v2.0.1",
		map[string]string{"multiclusterhub-operator": "example.com/registration-operator:test"},
		map[string]string{"registration-operator": "example.com/registration-operator:test"})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agentAddon := fakeAgentAddon(t, test.existingObjects...)
			objects, err := agentAddon.Manifests(test.cluster, test.addon)
			if err != nil {
				t.Fatalf("failed to get manifests. err:%v", err)
			}
			if len(objects) != test.expectedCount {
				t.Errorf("expected test.expectedCount objects, but got %v", len(objects))
			}
			for _, o := range objects {
				switch object := o.(type) {
				case *appsv1.Deployment:
					if object.GetNamespace() != test.expectedInstallNamespace {
						t.Errorf("expected namespace constants.HOHAgentInstallNamespace, but got %v", object.GetNamespace())
					}
					image := object.Spec.Template.Spec.Containers[0].Image
					if image == "" {
						t.Errorf("expected image, but got %v", image)
					}
				}
			}
			// output is for debug
			output(t, test.name, objects...)
		})
	}
}

func output(t *testing.T, name string, objects ...runtime.Object) {
	_, err := os.Stat("./.tmp")
	if os.IsNotExist(err) {
		err := os.Mkdir("./.tmp", 0o777)
		if err != nil {
			t.Fatalf("failed to create tmp")
		}
	}
	tmpDir, err := os.MkdirTemp("./.tmp/", name)
	if err != nil {
		t.Fatalf("failed to create temp files %v", err)
	}

	for i, o := range objects {
		data, err := yaml.Marshal(o)
		if err != nil {
			t.Fatalf("failed yaml marshal %v", err)
		}

		err = ioutil.WriteFile(fmt.Sprintf("%v/%v-%v.yaml", tmpDir, i,
			o.GetObjectKind().GroupVersionKind().Kind), data, 0o644)
		if err != nil {
			t.Fatalf("failed to Marshal object.%v", err)
		}
	}
}
