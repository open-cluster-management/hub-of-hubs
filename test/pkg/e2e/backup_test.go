package tests

import (
	"context"
	"time"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mchv1 "github.com/stolostron/multiclusterhub-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var (
	runtimeClient client.Client
	scheme        = runtime.NewScheme()
	ctx           = context.Background()
)

var mchObj = &mchv1.MultiClusterHub{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mch",
		Namespace: "open-cluster-management",
	},
	Spec: mchv1.MultiClusterHubSpec{
		Overrides: &mchv1.Overrides{
			Components: []mchv1.ComponentConfig{
				{
					Name:    "cluster-backup",
					Enabled: true,
				},
			},
		},
	},
}

var _ = Describe("The resources should have backup label", Ordered, Label("e2e-tests-backup"), func() {
	BeforeAll(func() {
		By("Get the runtimeClient client")
		globalhubv1alpha4.AddToScheme(scheme)
		kafkav1beta2.AddToScheme(scheme)
		apiextensionsv1.AddToScheme(scheme)
		corev1.AddToScheme(scheme)
		mchv1.AddToScheme(scheme)
		var err error
		runtimeClient, err = testClients.ControllerRuntimeClient(testOptions.GlobalHub.Name, scheme)
		Expect(err).ShouldNot(HaveOccurred())
		err = runtimeClient.Create(ctx, mchObj)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("The pvc should have backup label", func() {
		Eventually(func() bool {
			pvcList := &corev1.PersistentVolumeClaimList{}
			Expect(runtimeClient.List(ctx, pvcList)).Should(Succeed())
			for _, v := range pvcList.Items {
				if v.Namespace != Namespace {
					continue
				}
				klog.Errorf("pvc:%v, label:%v", v.Name, v.Labels)
				if !utils.HasItem(v.Labels, constants.BackupExcludeKey, "true") {
					return false
				}
			}
			return true
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
	})

	It("The mgh should have backup label", func() {
		Eventually(func() bool {
			mgh := &globalhubv1alpha4.MulticlusterGlobalHub{}
			Expect(runtimeClient.Get(ctx, types.NamespacedName{
				Namespace: Namespace,
				Name:      "multiclusterglobalhub",
			}, mgh)).Should(Succeed())
			return utils.HasItem(mgh.Labels, constants.BackupKey, constants.BackupActivationValue)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
	})

	It("The secret should have backup label", func() {
		customSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Namespace,
				Name:      constants.CustomGrafanaIniName,
			},
			Data: map[string][]byte{
				grafanaIniKey: []byte(`
	[smtp]
	user = true
	pass = true
	[auth]
	enabled = false
	[dataproxy]
	timeout = 300
	dial_timeout = 30
	keep_alive_seconds = 300
	`),
			},
		}
		_, err := testClients.KubeClient().CoreV1().Secrets(Namespace).Create(ctx, customSecret, metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() bool {
			cusSecret, err := testClients.KubeClient().CoreV1().Secrets(Namespace).Get(ctx, customSecret.Name, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			return utils.HasItem(cusSecret.Labels, constants.BackupKey, constants.BackupGlobalHubValue)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
		err = testClients.KubeClient().CoreV1().Secrets(Namespace).Delete(ctx, customSecret.Name, metav1.DeleteOptions{})
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("The configmap should have backup label", func() {
		customConfig := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: Namespace,
				Name:      constants.CustomAlertName,
			},
			Data: map[string]string{
				alertConfigMapKey: `
- name: alerts-cu-webhook
	orgId: 1
  receivers:
  - disableResolveMessage: false
	type: email
	uid: 4e3bfe25-00cf-4173-b02b-16f077e539da`,
			},
		}

		_, err := testClients.KubeClient().CoreV1().ConfigMaps(Namespace).Create(ctx, customConfig, metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())
		Eventually(func() bool {
			cusConfigmap, err := testClients.KubeClient().CoreV1().ConfigMaps(Namespace).Get(ctx, customConfig.Name, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			return utils.HasItem(cusConfigmap.Labels, constants.BackupKey, constants.BackupGlobalHubValue)
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
		err = testClients.KubeClient().CoreV1().ConfigMaps(Namespace).Delete(ctx, customConfig.Name, metav1.DeleteOptions{})
		Expect(err).ShouldNot(HaveOccurred())
	})
})
