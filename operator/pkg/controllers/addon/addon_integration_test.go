package addon_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stolostron/cluster-lifecycle-api/imageregistry/v1alpha1"
	agentv1 "github.com/stolostron/klusterlet-addon-controller/pkg/apis/agent/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	Expect(k8sClient.Create(ctx, cluster)).Should(Succeed())
	if len(conditions) != 0 || len(claims) != 0 {
		cluster.Status.Conditions = conditions
		cluster.Status.ClusterClaims = claims
		Expect(k8sClient.Status().Update(ctx, cluster)).Should(Succeed())
	}

	Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})).Should(Succeed())

	klusterletAddonConfig := &agentv1.KlusterletAddonConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
		Spec: agentv1.KlusterletAddonConfigSpec{
			SearchCollectorConfig: agentv1.KlusterletAddonAgentConfigSpec{
				Enabled: true,
			},
			PolicyController: agentv1.KlusterletAddonAgentConfigSpec{
				Enabled: true,
			},
			ApplicationManagerConfig: agentv1.KlusterletAddonAgentConfigSpec{
				Enabled: true,
			},
			CertPolicyControllerConfig: agentv1.KlusterletAddonAgentConfigSpec{
				Enabled: true,
			},
			IAMPolicyControllerConfig: agentv1.KlusterletAddonAgentConfigSpec{
				Enabled: true,
			},
		},
	}

	Expect(k8sClient.Create(ctx, klusterletAddonConfig)).Should(Succeed())
}

var _ = Describe("addon integration", Ordered, func() {
	BeforeAll(func() {
		By("Create clustermanagementaddon instance")
		clusterManagementAddon := &addonv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: operatorconstants.GHClusterManagementAddonName,
				Labels: map[string]string{
					constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
				},
			},
		}
		Expect(k8sClient.Create(ctx, clusterManagementAddon)).Should(Succeed())

		By("Create global hub configmap instance")
		expectedHoHConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: constants.GHSystemNamespace,
				Name:      constants.GHAgentConfigCMName,
				Labels: map[string]string{
					constants.GlobalHubOwnerLabelKey:       constants.GHOperatorOwnerLabelVal,
					constants.GlobalHubGlobalResourceLabel: "",
				},
			},
			Data: map[string]string{
				"aggregationLevel":    string(operatorconstants.FullAggregation),
				"enableLocalPolicies": strconv.FormatBool(true),
			},
		}
		expectedHoHConfigMap.SetUID("test-uid")
		config.SetGlobalHubAgentConfig(expectedHoHConfigMap)
	})

	Context("When configure the image registry and pull secret", func() {
		It("Should update the image pull secret from the mgh cr", func() {
			clusterName := fmt.Sprintf("hub-%s", rand.String(6))
			workName := fmt.Sprintf("addon-%s-deploy-0",
				operatorconstants.GHManagedClusterAddonName)

			By("By preparing an OCP Managed Clusters")
			prepareCluster(clusterName,
				map[string]string{"vendor": "OpenShift"},
				map[string]string{},
				[]clusterv1.ManagedClusterClaim{},
				clusterAvailableCondition)

			By("By checking the addon CR is created in the cluster ns")
			addon := &addonv1alpha1.ManagedClusterAddOn{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(addon.GetAnnotations())).Should(Equal(0))

			By("By checking the mgh image pull secret is created in the cluster's manifestworks")
			Eventually(func() error {
				work := &workv1.ManifestWork{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      workName,
					Namespace: clusterName,
				}, work)
				if err != nil {
					return err
				}
				fmt.Println("===", "manifests size", len(work.Spec.Workload.Manifests))
				for _, manifest := range work.Spec.Workload.Manifests {
					unstructuredObj := &unstructured.Unstructured{}
					err := json.Unmarshal(manifest.Raw, unstructuredObj)
					if err != nil {
						return err
					}
					fmt.Println("===", unstructuredObj.GetKind(),
						unstructuredObj.GetName(), unstructuredObj.GetNamespace())
					if unstructuredObj.GetKind() == "Secret" &&
						unstructuredObj.GetName() == mgh.Spec.ImagePullSecret {
						return nil
					}
				}
				return fmt.Errorf("image global hub pull secret is not created")
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Should update the image registry and pull secret from ManagedClusterImageRegistry", func() {
			clusterName := fmt.Sprintf("hub-%s", rand.String(6))
			workName := fmt.Sprintf("addon-%s-deploy-0",
				operatorconstants.GHManagedClusterAddonName)

			By("By preparing the image registry pull secret")
			imageRegistrySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-registry-pull-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					".dockerconfigjson": []byte(`{"test":"secret by the managedclusterimageregistries"}`),
				},
				Type: corev1.SecretTypeDockerConfigJson,
			}
			Expect(k8sClient.Create(ctx, imageRegistrySecret)).Should(Succeed())

			By("By preparing an OCP Managed Clusters")
			prepareCluster(clusterName,
				map[string]string{"vendor": "OpenShift"},
				map[string]string{
					v1alpha1.ClusterImageRegistriesAnnotation: `{"pullSecret":"default.image-registry-pull-secret","registries":[{"mirror":"quay.io/test","source":"quay.io/stolostron"}]}`,
				},
				[]clusterv1.ManagedClusterClaim{},
				clusterAvailableCondition)

			By("By checking the addon CR is is created in the cluster ns")
			addon := &addonv1alpha1.ManagedClusterAddOn{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			// Expect(len(addon.GetAnnotations())).Should(Equal(0))

			By("By checking the mgh image pull secret is created in the cluster's manifestworks")
			Eventually(func() error {
				work := &workv1.ManifestWork{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      workName,
					Namespace: clusterName,
				}, work)
				if err != nil {
					return err
				}
				fmt.Println("+++", "manifests size", len(work.Spec.Workload.Manifests))
				for _, manifest := range work.Spec.Workload.Manifests {
					unstructuredObj := &unstructured.Unstructured{}
					err := json.Unmarshal(manifest.Raw, unstructuredObj)
					if err != nil {
						return err
					}
					fmt.Println("+++", unstructuredObj.GetKind(),
						unstructuredObj.GetName(), unstructuredObj.GetNamespace())
					if unstructuredObj.GetKind() == "Secret" &&
						unstructuredObj.GetName() == imageRegistrySecret.GetName() {
						return nil
					}
				}
				return fmt.Errorf("image registry pull secret is not created")
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
	})

	Context("When a cluster is imported in default mode", func() {
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
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(addon.GetAnnotations())).Should(Equal(0))

			By("By checking the agent manifestworks are created for the newly created managed cluster")
			work := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
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
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(addon.GetAnnotations())).Should(Equal(0))

			By("By checking the agent manifestworks are created for the newly created managed cluster")
			work := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
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
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(addon.GetAnnotations())).Should(Equal(0))

			By("By checking the agent manifestworks are created for the newly created managed cluster")
			work := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
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
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(addon.GetAnnotations()[operatorconstants.AnnotationAddonHostingClusterName]).Should(Equal(hostingClusterName))

			By("By checking the agent manifestworks are created for the newly created managed cluster")
			work := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      workName,
					Namespace: clusterName,
				}, work)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(work.Spec.Workload.Manifests)).Should(Equal(2))
			hostingWork := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      hostingWorkName,
					Namespace: hostingClusterName,
				}, hostingWork)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(hostingWork.Spec.Workload.Manifests)).Should(Equal(7))
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
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      operatorconstants.GHManagedClusterAddonName,
					Namespace: clusterName,
				}, addon)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(addon.GetAnnotations()[operatorconstants.AnnotationAddonHostingClusterName]).Should(Equal(hostingClusterName))

			By("By checking the agent manifestworks are created for the newly created managed cluster")
			work := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      workName,
					Namespace: clusterName,
				}, work)
			}, timeout, interval).ShouldNot(HaveOccurred())

			Expect(len(work.Spec.Workload.Manifests)).Should(Equal(11))
			hostingWork := &workv1.ManifestWork{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      hostingWorkName,
					Namespace: hostingClusterName,
				}, hostingWork)
			}, timeout, interval).ShouldNot(HaveOccurred())
			Expect(len(hostingWork.Spec.Workload.Manifests)).Should(Equal(7))
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
				err := k8sClient.List(ctx, addonList, client.InNamespace(clusterName1),
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
})
