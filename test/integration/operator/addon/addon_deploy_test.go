package addon

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

type Object interface {
	metav1.Object
	runtime.Object
}

var clusterAvailableCondition = metav1.Condition{
	Type:               "ManagedClusterConditionAvailable",
	Reason:             "ManagedClusterAvailable",
	Message:            "Managed cluster is available",
	Status:             "True",
	LastTransitionTime: metav1.Time{Time: time.Now()},
}

func prepareCluster(name string, labels, annotations map[string]string,
	claims []clusterv1.ManagedClusterClaim, conditions ...metav1.Condition,
) {
	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	Expect(runtimeClient.Create(ctx, cluster)).Should(Succeed())
	if len(conditions) != 0 || len(claims) != 0 {
		cluster.Status.Conditions = conditions
		cluster.Status.ClusterClaims = claims
		Expect(runtimeClient.Status().Update(ctx, cluster)).Should(Succeed())
	}

	err := runtimeClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	if !errors.IsAlreadyExists(err) {
		Expect(err).Should(Succeed())
	}

	Expect(runtimeClient.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-kafka-user", name),
			Namespace: config.GetMGHNamespacedName().Namespace,
		},
	})).Should(Succeed())
}

// go test ./test/integration/operator/addon -ginkgo.focus "addon deploy" -v
var _ = Describe("addon deploy", func() {
	It("Should create HoH agent when an OCP without deployMode label is imported", func() {
		clusterName := fmt.Sprintf("hub-%s", rand.String(6))
		workName := fmt.Sprintf("addon-%s-deploy-0",
			operatorconstants.GHManagedClusterAddonName)

		By("By preparing an OCP Managed Clusters")
		prepareCluster(clusterName,
			map[string]string{"vendor": "OpenShift"},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)

		By("By checking the addon CR is is created in the cluster ns")
		addon := &addonv1alpha1.ManagedClusterAddOn{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      operatorconstants.GHManagedClusterAddonName,
				Namespace: clusterName,
			}, addon)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(addon.GetAnnotations())).Should(Equal(0))

		By("By checking the agent manifestworks are created for the newly created managed cluster")
		work := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      workName,
				Namespace: clusterName,
			}, work)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(work.Spec.Workload.Manifests)).Should(Equal(9))
	})

	It("Should create HoH agent and ACM when an OCP is imported", func() {
		clusterName := fmt.Sprintf("hub-%s", rand.String(6))
		workName := fmt.Sprintf("addon-%s-deploy-0",
			operatorconstants.GHManagedClusterAddonName)

		By("By preparing an OCP Managed Clusters")
		prepareCluster(clusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentACMHubInstallLabelKey: "",
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{
				{
					Name:  constants.HubClusterClaimName,
					Value: constants.HubNotInstalled,
				},
			},
			clusterAvailableCondition)

		By("By checking the addon CR is is created in the cluster ns")
		addon := &addonv1alpha1.ManagedClusterAddOn{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      operatorconstants.GHManagedClusterAddonName,
				Namespace: clusterName,
			}, addon)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(addon.GetAnnotations())).Should(Equal(0))

		By("By checking the agent manifestworks are created for the newly created managed cluster")
		work := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      workName,
				Namespace: clusterName,
			}, work)
		}, timeout, interval).ShouldNot(HaveOccurred())

		// contains both the ACM and the Global Hub manifests
		Expect(len(work.Spec.Workload.Manifests)).Should(Equal(18))
	})

	It("Should create HoH addon when an OCP with deploy mode = default is imported in hosted mode", func() {
		clusterName := fmt.Sprintf("hub-%s", rand.String(6))
		hostingClusterName := fmt.Sprintf("hub-hosting-%s", rand.String(6))
		workName := fmt.Sprintf("addon-%s-deploy-0",
			operatorconstants.GHManagedClusterAddonName)

		By("By preparing clusters")
		prepareCluster(clusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeDefault,
			},
			map[string]string{
				operatorconstants.AnnotationClusterHostingClusterName: hostingClusterName,
			},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		prepareCluster(hostingClusterName,
			map[string]string{"vendor": "OpenShift"},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By checking the addon CR is is created in the cluster ns")
		addon := &addonv1alpha1.ManagedClusterAddOn{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      operatorconstants.GHManagedClusterAddonName,
				Namespace: clusterName,
			}, addon)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(addon.GetAnnotations())).Should(Equal(0))

		By("By checking the agent manifestworks are created for the newly created managed cluster")
		work := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      workName,
				Namespace: clusterName,
			}, work)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(work.Spec.Workload.Manifests)).Should(Equal(9))
	})

	It("Should create HoH addon when an OCP with deploy mode = Hosted is imported in hosted mode", func() {
		clusterName := fmt.Sprintf("hub-%s", rand.String(6))
		hostingClusterName := fmt.Sprintf("hub-hosting-%s", rand.String(6))
		workName := fmt.Sprintf("addon-%s-deploy-0",
			operatorconstants.GHManagedClusterAddonName)
		hostingWorkName := fmt.Sprintf("addon-%s-deploy-hosting-%s-0",
			operatorconstants.GHManagedClusterAddonName, clusterName)
		By("By preparing clusters")
		prepareCluster(clusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeHosted,
			},
			map[string]string{
				operatorconstants.AnnotationClusterDeployMode:                operatorconstants.ClusterDeployModeHosted,
				operatorconstants.AnnotationClusterKlusterletDeployNamespace: "open-cluster-management-hub1",
				operatorconstants.AnnotationClusterHostingClusterName:        hostingClusterName,
			},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		prepareCluster(hostingClusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeNone,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By checking the addon CR is is created in the cluster ns")
		addon := &addonv1alpha1.ManagedClusterAddOn{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      operatorconstants.GHManagedClusterAddonName,
				Namespace: clusterName,
			}, addon)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(addon.GetAnnotations()[operatorconstants.AnnotationAddonHostingClusterName]).Should(Equal(hostingClusterName))

		By("By checking the agent manifestworks are created for the newly created managed cluster")
		work := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      workName,
				Namespace: clusterName,
			}, work)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(work.Spec.Workload.Manifests)).Should(Equal(2))
		hostingWork := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      hostingWorkName,
				Namespace: hostingClusterName,
			}, hostingWork)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(hostingWork.Spec.Workload.Manifests)).Should(Equal(6))
	})

	It("Should create HoH agent and ACM when an OCP with deploy mode = Hosted is imported in hosted mode", func() {
		clusterName := fmt.Sprintf("hub-%s", rand.String(6))
		hostingClusterName := fmt.Sprintf("hub-hosting-%s", rand.String(6))
		workName := fmt.Sprintf("addon-%s-deploy-0",
			operatorconstants.GHManagedClusterAddonName)
		hostingWorkName := fmt.Sprintf("addon-%s-deploy-hosting-%s-0",
			operatorconstants.GHManagedClusterAddonName, clusterName)
		By("By preparing clusters")
		prepareCluster(clusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey:    operatorconstants.GHAgentDeployModeHosted,
				operatorconstants.GHAgentACMHubInstallLabelKey: "",
			},
			map[string]string{
				operatorconstants.AnnotationClusterDeployMode:                operatorconstants.ClusterDeployModeHosted,
				operatorconstants.AnnotationClusterKlusterletDeployNamespace: "open-cluster-management-hub1",
				operatorconstants.AnnotationClusterHostingClusterName:        hostingClusterName,
			},
			[]clusterv1.ManagedClusterClaim{
				{
					Name:  constants.HubClusterClaimName,
					Value: constants.HubNotInstalled,
				},
			},
			clusterAvailableCondition)
		prepareCluster(hostingClusterName,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeNone,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By checking the addon CR is is created in the cluster ns")
		addon := &addonv1alpha1.ManagedClusterAddOn{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      operatorconstants.GHManagedClusterAddonName,
				Namespace: clusterName,
			}, addon)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(addon.GetAnnotations()[operatorconstants.AnnotationAddonHostingClusterName]).Should(Equal(hostingClusterName))

		By("By checking the agent manifestworks are created for the newly created managed cluster")
		work := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      workName,
				Namespace: clusterName,
			}, work)
		}, timeout, interval).ShouldNot(HaveOccurred())

		Expect(len(work.Spec.Workload.Manifests)).Should(Equal(11))
		hostingWork := &workv1.ManifestWork{}
		Eventually(func() error {
			return runtimeClient.Get(ctx, types.NamespacedName{
				Name:      hostingWorkName,
				Namespace: hostingClusterName,
			}, hostingWork)
		}, timeout, interval).ShouldNot(HaveOccurred())
		Expect(len(hostingWork.Spec.Workload.Manifests)).Should(Equal(6))
	})
	It("Should not create HoH addon in these cases", func() {
		By("By preparing a non-OCP with deployMode label Managed Clusters")
		clusterName1 := fmt.Sprintf("hub-non-ocp-%s", rand.String(6))
		prepareCluster(clusterName1,
			map[string]string{
				"vendor": "GCP",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeDefault,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By preparing an OCP with deployMode = none label Managed Clusters")
		clusterName2 := fmt.Sprintf("hub-ocp-mode-none-%s", rand.String(6))
		prepareCluster(clusterName2,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeNone,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By preparing an OCP with no condition Managed Clusters")
		clusterName3 := fmt.Sprintf("hub-ocp-no-condtion-%s", rand.String(6))
		prepareCluster(clusterName3,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeDefault,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
		)
		By("By preparing a local cluster")
		clusterName4 := operatorconstants.LocalClusterName
		prepareCluster(clusterName4, map[string]string{
			"vendor": "OpenShift",
			operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeDefault,
		},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)
		By("By preparing an OCP with deploy mode = Hosted without hosting cluster")
		clusterName5 := fmt.Sprintf("hub-ocp-mode-none-%s", rand.String(6))
		prepareCluster(clusterName5,
			map[string]string{
				"vendor": "OpenShift",
				operatorconstants.GHAgentDeployModeLabelKey: operatorconstants.GHAgentDeployModeHosted,
			},
			map[string]string{},
			[]clusterv1.ManagedClusterClaim{},
			clusterAvailableCondition)

		By("By checking the addon CR is is created in the cluster ns")
		addonList := &addonv1alpha1.ManagedClusterAddOnList{}
		checkCount := 0
		Eventually(func() error {
			err := runtimeClient.List(ctx, addonList, client.InNamespace(clusterName1),
				client.InNamespace(clusterName2), client.InNamespace(clusterName3),
				client.InNamespace(clusterName4), client.InNamespace(clusterName5))
			if err != nil {
				return err
			}

			if len(addonList.Items) != 0 {
				return fmt.Errorf("expected there is no addon, but got %#v", addonList)
			}
			if checkCount == 5 {
				// cannot get addon in 5s, the addon is not created
				return nil
			}
			checkCount++
			time.Sleep(1 * time.Second)
			return fmt.Errorf("check again %v", checkCount)
		}, timeout, interval).ShouldNot(HaveOccurred())
	})
})
