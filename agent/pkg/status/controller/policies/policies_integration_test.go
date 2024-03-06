package policies

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
)

var _ = Describe("test the policy emitters", Ordered, func() {
	It("should pass the replicated policy event", func() {
		By("Create a root policy")
		rootPolicy := &policyv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "policy1",
				Namespace:  "default",
				Finalizers: []string{constants.GlobalHubCleanupFinalizer},
			},
			Spec: policyv1.PolicySpec{
				Disabled:        true,
				PolicyTemplates: []*policyv1.PolicyTemplate{},
			},
			Status: policyv1.PolicyStatus{},
		}
		Expect(kubeClient.Create(ctx, rootPolicy)).NotTo(HaveOccurred())

		By("Check the local policy spec can be read from cloudevents consumer")
		Eventually(func() error {
			evt := mockTrans.GetEvent()
			fmt.Println(evt)
			if evt.Type() != string(enum.LocalPolicySpecType) {
				return fmt.Errorf("want %v, got %v", string(enum.LocalPolicySpecType), evt.Type())
			}
			return nil
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		rootPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.Compliant,
			Status: []*policyv1.CompliancePerClusterStatus{
				{
					ClusterName:      "cluster1",
					ClusterNamespace: "cluster1",
					ComplianceState:  policyv1.Compliant,
				},
			},
		}
		Expect(kubeClient.Status().Update(ctx, rootPolicy)).Should(Succeed())

		By("Check the local compliance can be read from cloudevents consumer")
		Eventually(func() error {
			evt := mockTrans.GetEvent()
			fmt.Println(evt)
			if evt.Type() != string(enum.LocalComplianceType) {
				return fmt.Errorf("want %v, got %v", string(enum.LocalComplianceType), evt.Type())
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())

		By("Create namespace and cluster for the replicated policy")
		err := kubeClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
		}, &client.CreateOptions{})
		Expect(err).Should(Succeed())

		By("Create the cluster")
		cluster := &clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
		}
		Expect(kubeClient.Create(ctx, cluster, &client.CreateOptions{})).Should(Succeed())
		cluster.Status = clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  "id.k8s.io",
					Value: "3f406177-34b2-4852-88dd-ff2809680336",
				},
			},
		}
		Expect(kubeClient.Status().Update(ctx, cluster)).Should(Succeed())

		By("Create the replicated policy")
		replicatedPolicy := &policyv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default.policy1",
				Namespace: "cluster1",
				Labels: map[string]string{
					constants.PolicyEventRootPolicyNameLabelKey: fmt.Sprintf("%s.%s", "default", "policy1"),
					constants.PolicyEventClusterNameLabelKey:    "cluster1",
				},
			},
			Spec: policyv1.PolicySpec{
				Disabled:        false,
				PolicyTemplates: []*policyv1.PolicyTemplate{},
			},
		}
		Expect(kubeClient.Create(ctx, replicatedPolicy)).ToNot(HaveOccurred())

		By("Create the replicated policy event")
		replicatedPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.NonCompliant,
			Details: []*policyv1.DetailsPerTemplate{
				{
					TemplateMeta: metav1.ObjectMeta{
						Name: "test-local-policy-template",
					},
					History: []policyv1.ComplianceHistory{
						{
							EventName:     "default.policy1.17b0db2427432200",
							LastTimestamp: metav1.Now(),
							Message: `NonCompliant; violation - limitranges [container-mem-limit-range] not found in namespace 
							default`,
						},
					},
				},
			},
		}
		Expect(kubeClient.Status().Update(ctx, replicatedPolicy)).Should(Succeed())

		By("Check the local replicated policy event can be read from cloudevents consumer")
		Eventually(func() error {
			evt := mockTrans.GetEvent()
			fmt.Println(evt)
			if evt.Type() != string(enum.LocalReplicatedPolicyEventType) {
				return fmt.Errorf("want %v, got %v", string(enum.LocalReplicatedPolicyEventType), evt.Type())
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})