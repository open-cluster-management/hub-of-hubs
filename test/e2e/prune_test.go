package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	chnv1 "open-cluster-management.io/multicloud-operators-channel/pkg/apis/apps/v1"
	placementrulesv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/placementrule/v1"
	appsubv1 "open-cluster-management.io/multicloud-operators-subscription/pkg/apis/apps/v1"
	applicationv1beta1 "sigs.k8s.io/application/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

const (
	TIMEOUT  = 5 * time.Minute
	INTERVAL = 1 * time.Second
)

var _ = Describe("Delete the multiclusterglobalhub and prune resources", Label("e2e-test-prune"), Ordered, func() {
	var managedClusterName1 string
	var managedClusterName2 string

	BeforeAll(func() {
		managedClusterName1 = managedClusters[0].Name
		managedClusterName2 = managedClusters[1].Name
	})

	It("create application", Label("e2e-test-global-resource"), func() {
		By("Add app label to the managedcluster1")
		assertAddLabel(managedClusters[0], APP_LABEL_KEY, APP_LABEL_VALUE)

		By("Apply the appsub to labeled cluster")
		Eventually(func() error {
			_, err := testClients.Kubectl(testOptions.GlobalHub.Name, "apply", "-f", APP_SUB_YAML)
			if err != nil {
				return err
			}
			return nil
		}, 1*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		By("Check the appsub is applied to the cluster")
		Eventually(func() error {
			return checkAppsubreport(httpClient, APP_SUB_NAME, APP_SUB_NAMESPACE, 1,
				[]string{managedClusterName1})
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})

	It("create policy", Label("e2e-test-global-resource"), func() {
		By("Add policy label to the managedcluster2")
		assertAddLabel(managedClusters[1], POLICY_LABEL_KEY, POLICY_LABEL_VALUE)

		By("Apply the policy to labeled cluster")
		Eventually(func() error {
			_, err := testClients.Kubectl(testOptions.GlobalHub.Name, "apply", "-f", INFORM_POLICY_YAML)
			if err != nil {
				return err
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		Eventually(func() error {
			status, err := getStatusFromGolbalHub(globalHubClient, httpClient, POLICY_NAME, POLICY_NAMESPACE)
			if err != nil {
				return err
			}
			for _, policyInfo := range status.Status {
				if policyInfo.ClusterName == managedClusterName2 {
					// if policyInfo.ComplianceState == policiesv1.NonCompliant {
					return nil
					// }
				}
			}
			return fmt.Errorf("the policy have not applied to the managed cluster %s", managedClusterName2)
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})

	It("delete multiclusterglobalhub", func() {
		By("Check whether multiclusterglobalhub is exists")
		mgh := &globalhubv1alpha4.MulticlusterGlobalHub{}
		err := globalHubClient.Get(ctx, types.NamespacedName{
			Namespace: testOptions.GlobalHub.Namespace,
			Name:      "multiclusterglobalhub",
		}, mgh)
		Expect(err).To(Succeed())

		By("Delete multiclusterglobalhub")
		err = globalHubClient.Delete(ctx, mgh, &client.DeleteOptions{})
		Expect(err).To(Succeed())

		By("Check whether multiclusterglobalhub is deleted")
		Eventually(func() error {
			err = globalHubClient.Get(ctx, types.NamespacedName{
				Namespace: testOptions.GlobalHub.Namespace,
				Name:      "multiclusterglobalhub",
			}, mgh)
			if errors.IsNotFound(err) {
				return nil
			}
			if err == nil {
				return fmt.Errorf("multiclusterglobalhub should be deleted")
			}
			return err
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})

	It("prune the global resources", func() {
		listOpts := []client.ListOption{
			client.MatchingLabels(map[string]string{
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			}),
		}

		By("Delete clusterrole")
		Eventually(func() error {
			clusterRoleList := &rbacv1.ClusterRoleList{}
			if err := globalHubClient.List(ctx, clusterRoleList, listOpts...); err != nil {
				return err
			}
			if len(clusterRoleList.Items) > 0 {
				return fmt.Errorf("clusterroles has not been deleted")
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete clusterrolebinding")
		Eventually(func() error {
			clusterRoleBindingList := &rbacv1.ClusterRoleBindingList{}
			if err := globalHubClient.List(ctx, clusterRoleBindingList, listOpts...); err != nil {
				return err
			}
			if len(clusterRoleBindingList.Items) > 0 {
				return fmt.Errorf("clusterrolebindings has not been deleted")
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete placement finalizer")
		Eventually(func() error {
			placements := &clusterv1beta1.PlacementList{}
			if err := globalHubClient.List(ctx, placements, &client.ListOptions{}); err != nil &&
				!errors.IsNotFound(err) {
				return err
			}
			for idx := range placements.Items {
				if controllerutil.ContainsFinalizer(&placements.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from obj: %s", placements.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete managedclusterset finalizer")
		Eventually(func() error {
			managedclustersets := &clusterv1beta2.ManagedClusterSetList{}
			if err := globalHubClient.List(ctx, managedclustersets, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range managedclustersets.Items {
				if controllerutil.ContainsFinalizer(&managedclustersets.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from obj: %s",
						managedclustersets.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete managedclustersetbinding finalizer")
		Eventually(func() error {
			managedclustersetbindings := &clusterv1beta2.ManagedClusterSetBindingList{}
			if err := globalHubClient.List(ctx, managedclustersetbindings, &client.ListOptions{}); err != nil &&
				!errors.IsNotFound(err) {
				return err
			}
			for idx := range managedclustersetbindings.Items {
				if controllerutil.ContainsFinalizer(&managedclustersetbindings.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from obj: %s",
						managedclustersetbindings.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})

	It("prune application", Label("e2e-test-global-resource"), func() {
		By("Delete the application finalizer")
		Eventually(func() error {
			applications := &applicationv1beta1.ApplicationList{}
			if err := globalHubClient.List(ctx, applications, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range applications.Items {
				if controllerutil.ContainsFinalizer(&applications.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from app: %s", applications.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete the appsub finalizer")
		Eventually(func() error {
			appsubs := &appsubv1.SubscriptionList{}
			if err := globalHubClient.List(ctx, appsubs, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range appsubs.Items {
				if controllerutil.ContainsFinalizer(&appsubs.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from appsub: %s", appsubs.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete the channel finalizer")
		Eventually(func() error {
			channels := &chnv1.ChannelList{}
			if err := globalHubClient.List(ctx, channels, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range channels.Items {
				if controllerutil.ContainsFinalizer(&channels.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from channels obj: %s", channels.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete the palcementrules(policy and app) finalizer")
		Eventually(func() error {
			palcementrules := &placementrulesv1.PlacementRuleList{}
			if err := globalHubClient.List(ctx, palcementrules, &client.ListOptions{}); err != nil && errors.IsNotFound(err) {
				return err
			}
			for idx := range palcementrules.Items {
				if controllerutil.ContainsFinalizer(&palcementrules.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from palcementrules obj: %s",
						palcementrules.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})

	It("prune policy", Label("e2e-test-global-resource"), func() {
		By("Delete the policies finalizer")
		Eventually(func() error {
			policies := &policiesv1.PolicyList{}
			if err := globalHubClient.List(ctx, policies, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range policies.Items {
				if controllerutil.ContainsFinalizer(&policies.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from policies obj: %s", policies.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())

		By("Delete the placementbindings finalizer")
		Eventually(func() error {
			placementbindings := &policiesv1.PlacementBindingList{}
			if err := globalHubClient.List(ctx, placementbindings, &client.ListOptions{}); err != nil {
				return err
			}
			for idx := range placementbindings.Items {
				if controllerutil.ContainsFinalizer(&placementbindings.Items[idx],
					constants.GlobalHubCleanupFinalizer) {
					return fmt.Errorf("finalizer hasn't be deleted from placementbindings obj: %s",
						placementbindings.Items[idx].GetName())
				}
			}
			return nil
		}, TIMEOUT, INTERVAL).ShouldNot(HaveOccurred())
	})
})
