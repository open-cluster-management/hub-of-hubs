package localpolicies

import (
	"fmt"

	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/generic"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/grc"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewCompleteComplianceEmitter(topic string, dependencyVersion *metadata.BundleVersion) generic.ObjectEmitter {
	eventData := make([]grc.CompleteCompliance, 0)
	return generic.NewGenericObjectEmitter(
		enum.LocalPolicyCompleteComplianceType,
		eventData,
		NewCompleteComplianceHandler(eventData),
		generic.WithTopic(topic),
		generic.WithDependencyVersion(dependencyVersion),
		generic.WithPredicate(complianceEmitterPredicate),
	)
}

type completeComplianceHandler struct {
	eventData grc.CompleteComplianceData
}

func NewCompleteComplianceHandler(evtData grc.CompleteComplianceData) generic.Handler {
	return &completeComplianceHandler{
		eventData: evtData,
	}
}

func (h *completeComplianceHandler) Update(obj client.Object) bool {
	policy, isPolicy := obj.(*policiesv1.Policy)
	if !isPolicy {
		return false // do not handle objects other than policy
	}

	originPolicyID := extractPolicyID(obj)
	newComplete := newCompleteCompliance(originPolicyID, policy)

	index := getPayloadIndexByUID(originPolicyID, h.eventData)
	if index == -1 { // object not found, need to add it to the bundle (only in case it contains non-compliant/unknown)
		// don't send in the bundle a policy where all clusters are compliant
		if len(newComplete.UnknownComplianceClusters) == 0 && len(newComplete.NonCompliantClusters) == 0 {
			return false
		}

		h.eventData = append(h.eventData, *newComplete)
		return true
	}

	// if we reached here, policy already exists in the bundle with at least one non compliant or unknown cluster.
	oldComplete := h.eventData[index]
	if utils.Equal(oldComplete.NonCompliantClusters, newComplete.NonCompliantClusters) &&
		utils.Equal(oldComplete.UnknownComplianceClusters, newComplete.UnknownComplianceClusters) {
		return false
	}

	// the payload is updated
	h.eventData[index].NonCompliantClusters = newComplete.NonCompliantClusters
	h.eventData[index].UnknownComplianceClusters = newComplete.UnknownComplianceClusters

	// don't send in the bundle a policy where all clusters are compliant
	if len(h.eventData[index].NonCompliantClusters) == 0 && len(h.eventData[index].UnknownComplianceClusters) == 0 {
		h.eventData = append(h.eventData[:index], h.eventData[index+1:]...) // remove from objects
	}
	return true
}

func (h *completeComplianceHandler) Delete(obj client.Object) bool {
	_, isPolicy := obj.(*policiesv1.Policy)
	if !isPolicy {
		return false // don't handle objects other than policy
	}

	index := getPayloadIndexByObj(obj, h.eventData)
	if index == -1 { // trying to delete object which doesn't exist
		return false
	}

	// don't increase version, no need to send bundle when policy is removed (Compliance bundle is sent).
	h.eventData = append((h.eventData)[:index], h.eventData[index+1:]...) // remove from objects
	return false
}

func newCompleteCompliance(originPolicyID string, policy *policiesv1.Policy) *grc.CompleteCompliance {
	nonCompliantClusters := make([]string, 0)
	unknownComplianceClusters := make([]string, 0)

	for _, clusterCompliance := range policy.Status.Status {
		if clusterCompliance.ComplianceState == policiesv1.Compliant {
			continue
		}
		if clusterCompliance.ComplianceState == policiesv1.NonCompliant {
			nonCompliantClusters = append(nonCompliantClusters, clusterCompliance.ClusterName)
		} else { // not compliant not non compliant -> means unknown
			unknownComplianceClusters = append(unknownComplianceClusters, clusterCompliance.ClusterName)
		}
	}

	return &grc.CompleteCompliance{
		PolicyID:                  originPolicyID,
		NamespacedName:            policy.Namespace + "/" + policy.Name,
		NonCompliantClusters:      nonCompliantClusters,
		UnknownComplianceClusters: unknownComplianceClusters,
	}
}

func getPayloadIndexByObj(obj client.Object, completes []grc.CompleteCompliance) int {
	uid := extractPolicyID(obj)
	if len(uid) > 0 {
		for i, complete := range completes {
			if uid == string(complete.PolicyID) {
				return i
			}
		}
	} else {
		for i, complete := range completes {
			if string(complete.NamespacedName) == fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName()) {
				return i
			}
		}
	}
	return -1
}

func getPayloadIndexByUID(uid string, completeCompliances []grc.CompleteCompliance) int {
	for i, object := range completeCompliances {
		if object.PolicyID == uid {
			return i
		}
	}
	return -1
}

// returns a list of non compliant clusters and a list of unknown compliance clusters.
func getNonCompliantAndUnknownClusters(policy *policiesv1.Policy) ([]string, []string) {
	nonCompliantClusters := make([]string, 0)
	unknownComplianceClusters := make([]string, 0)

	for _, clusterCompliance := range policy.Status.Status {
		if clusterCompliance.ComplianceState == policiesv1.Compliant {
			continue
		}

		if clusterCompliance.ComplianceState == policiesv1.NonCompliant {
			nonCompliantClusters = append(nonCompliantClusters, clusterCompliance.ClusterName)
		} else { // not compliant not non compliant -> means unknown
			unknownComplianceClusters = append(unknownComplianceClusters, clusterCompliance.ClusterName)
		}
	}

	return nonCompliantClusters, unknownComplianceClusters
}
