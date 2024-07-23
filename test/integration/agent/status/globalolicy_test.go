package status

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	policyv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle/grc"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

// go test ./test/integration/agent/status -v -ginkgo.focus "GlobalPolicyEmitters"
var _ = Describe("GlobalPolicyEmitters", Ordered, func() {
	const testGlobalPolicyOriginUID = "test-globalpolicy-uid"
	var globalPolicy *policyv1.Policy
	var consumer transport.Consumer

	BeforeAll(func() {
		consumer = chanTransport.Consumer(PolicyTopic)
	})

	It("be able to sync policy spec", func() {
		By("Create a global policy")
		globalPolicy = &policyv1.Policy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-global-policy-1",
				Namespace: "default",
				Annotations: map[string]string{
					constants.OriginOwnerReferenceAnnotation: testGlobalPolicyOriginUID,
				},
			},
			Spec: policyv1.PolicySpec{
				Disabled:        false,
				PolicyTemplates: []*policyv1.PolicyTemplate{},
			},
			Status: policyv1.PolicyStatus{},
		}
		Expect(runtimeClient.Create(ctx, globalPolicy)).NotTo(HaveOccurred())

		By("Create compliance on the policy")
		globalPolicyCopy := globalPolicy.DeepCopy()
		globalPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.NonCompliant,
			Placement: []*policyv1.Placement{
				{
					PlacementBinding: "test-policy-placement",
					PlacementRule:    "test-policy-placement",
				},
			},
			Status: []*policyv1.CompliancePerClusterStatus{
				{
					ClusterName:      "hub1-mc1",
					ClusterNamespace: "hub1-mc1",
					ComplianceState:  policyv1.Compliant,
				},
				{
					ClusterName:      "hub1-mc2",
					ClusterNamespace: "hub1-mc2",
					ComplianceState:  policyv1.NonCompliant,
				},
				{
					ClusterName:      "hub1-mc3",
					ClusterNamespace: "hub1-mc3",
					ComplianceState:  policyv1.NonCompliant,
				},
			},
		}
		Expect(runtimeClient.Status().Patch(ctx, globalPolicy, client.MergeFrom(globalPolicyCopy))).ToNot(HaveOccurred())
		// Expect(runtimeClient.Status().Update(ctx, globalPolicy)).Should(Succeed())

		// policy := &policyv1.Policy{}
		// err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(globalPolicy), policy)
		// Expect(err).Should(Succeed())
		// utils.PrettyPrint(policy.Status)

		By("Check the compliance can be read from cloudevents consumer")
		Eventually(func() error {
			evt := <-consumer.EventChan()
			fmt.Println(evt)
			if evt.Type() != string(enum.ComplianceType) {
				return fmt.Errorf("want %v, got %v", string(enum.ComplianceType), evt.Type())
			}

			data := grc.ComplianceBundle{}
			if err := evt.DataAs(&data); err != nil {
				return err
			}
			compliance := data[0]
			expectedCompliant := []string{"hub1-mc1"}
			expectedNonCompliant := []string{"hub1-mc2", "hub1-mc3"}
			if !utils.Equal(compliance.CompliantClusters, expectedCompliant) {
				return fmt.Errorf("compliant: want %v, got %v", expectedCompliant, compliance.CompliantClusters)
			}
			if !utils.Equal(compliance.NonCompliantClusters, expectedNonCompliant) {
				return fmt.Errorf("nonCompliant: want %v, got %v", expectedNonCompliant, compliance.NonCompliantClusters)
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("be able to sync policy complete compliance", func() {
		By("Create the compliance on the root policy status")
		globalPolicyCopy := globalPolicy.DeepCopy()
		globalPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.NonCompliant,
			Placement: []*policyv1.Placement{
				{
					PlacementBinding: "test-policy-placement",
					PlacementRule:    "test-policy-placement",
				},
			},
			Status: []*policyv1.CompliancePerClusterStatus{
				{
					ClusterName:      "hub1-mc1",
					ClusterNamespace: "hub1-mc1",
					ComplianceState:  policyv1.Compliant,
				},
				{
					ClusterName:      "hub1-mc2",
					ClusterNamespace: "hub1-mc2",
					ComplianceState:  policyv1.Compliant,
				},
				{
					ClusterName:      "hub1-mc3",
					ClusterNamespace: "hub1-mc3",
					ComplianceState:  policyv1.NonCompliant,
				},
			},
		}
		Expect(runtimeClient.Status().Patch(ctx, globalPolicy, client.MergeFrom(globalPolicyCopy))).ToNot(HaveOccurred())

		// policy := &policyv1.Policy{}
		// err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(globalPolicy), policy)
		// Expect(err).Should(Succeed())
		// utils.PrettyPrint(policy.Status)

		By("Check the complete compliance can be read from cloudevents consumer")
		Eventually(func() error {
			evt := <-consumer.EventChan()
			fmt.Println(evt)
			if evt.Type() != string(enum.CompleteComplianceType) {
				return fmt.Errorf("want %v, got %v", string(enum.CompleteComplianceType), evt.Type())
			}
			data := grc.CompleteComplianceBundle{}
			if err := evt.DataAs(&data); err != nil {
				return err
			}

			complete := data[0]
			expectedNonCompliant := []string{"hub1-mc3"}
			if !utils.Equal(complete.NonCompliantClusters, expectedNonCompliant) {
				return fmt.Errorf("noCompliant: want %v, got %v", expectedNonCompliant, complete.NonCompliantClusters)
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("be able to sync policy compliance", func() {
		By("Create the compliance on the root policy status")
		globalPolicyCopy := globalPolicy.DeepCopy()
		globalPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.NonCompliant,
			Placement: []*policyv1.Placement{
				{
					PlacementBinding: "test-policy-placement",
					PlacementRule:    "test-policy-placement",
				},
			},
			Status: []*policyv1.CompliancePerClusterStatus{
				{
					ClusterName:      "hub1-mc1",
					ClusterNamespace: "hub1-mc1",
					ComplianceState:  policyv1.Compliant,
				},
				{
					ClusterName:      "hub1-mc3",
					ClusterNamespace: "hub1-mc3",
					ComplianceState:  policyv1.NonCompliant,
				},
			},
		}
		Expect(runtimeClient.Status().Patch(ctx, globalPolicy, client.MergeFrom(globalPolicyCopy))).ToNot(HaveOccurred())

		// policy := &policyv1.Policy{}
		// err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(globalPolicy), policy)
		// Expect(err).Should(Succeed())
		// utils.PrettyPrint(policy.Status)

		By("Check the compliance can be read from cloudevents consumer")
		Eventually(func() error {
			evt := <-consumer.EventChan()
			fmt.Println(evt)
			if evt.Type() != string(enum.ComplianceType) {
				return fmt.Errorf("want %v, got %v", string(enum.ComplianceType), evt.Type())
			}
			data := grc.ComplianceBundle{}
			if err := evt.DataAs(&data); err != nil {
				return err
			}

			compliance := data[0]
			expectedCompliant := []string{"hub1-mc1"}
			expectedNonCompliant := []string{"hub1-mc3"}
			if !utils.Equal(compliance.CompliantClusters, expectedCompliant) {
				return fmt.Errorf("compliant: want %v, got %v", expectedCompliant, compliance.CompliantClusters)
			}
			if !utils.Equal(compliance.NonCompliantClusters, expectedNonCompliant) {
				return fmt.Errorf("nonCompliant: want %v, got %v", expectedNonCompliant, compliance.NonCompliantClusters)
			}

			if len(compliance.UnknownComplianceClusters) > 0 {
				return fmt.Errorf("expect unknown compliance should be emtpy")
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("be able to delete policy compliance when status is empty", func() {
		By("Create the compliance on the root policy status")
		globalPolicyCopy := globalPolicy.DeepCopy()
		globalPolicy.Status = policyv1.PolicyStatus{
			ComplianceState: policyv1.NonCompliant,
			Placement: []*policyv1.Placement{
				{
					PlacementBinding: "test-policy-placement",
					PlacementRule:    "test-policy-placement",
				},
			},
		}
		Expect(runtimeClient.Status().Patch(ctx, globalPolicy, client.MergeFrom(globalPolicyCopy))).ToNot(HaveOccurred())

		// policy := &policyv1.Policy{}
		// err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(globalPolicy), policy)
		// Expect(err).Should(Succeed())
		// utils.PrettyPrint(policy.Status)

		By("Check the compliance can be read from cloudevents consumer")
		Eventually(func() error {
			evt := <-consumer.EventChan()
			fmt.Println(evt)
			if evt.Type() != string(enum.ComplianceType) {
				return fmt.Errorf("want %v, got %v", string(enum.ComplianceType), evt.Type())
			}
			data := grc.ComplianceBundle{}
			if err := evt.DataAs(&data); err != nil {
				return err
			}

			compliance := data[0]
			if len(compliance.UnknownComplianceClusters) > 0 {
				return fmt.Errorf("expect unknown compliance should be emtpy")
			}

			if len(compliance.CompliantClusters) > 0 {
				return fmt.Errorf("expect compliant compliance should be emtpy")
			}

			if len(compliance.NonCompliantClusters) > 0 {
				return fmt.Errorf("expect non-compliant compliance should be emtpy")
			}

			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
