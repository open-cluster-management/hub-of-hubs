package localpolicies

import (
	"fmt"

	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/helper"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/bundle"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/bundle/grc"
	agentstatusconfig "github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/config"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/generic"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

// AddLocalPoliciesController this function adds a new local policies sync controller.
func AddLocalClusterPolicyEventsController(mgr ctrl.Manager, producer transport.Producer) error {
	createObjFunc := func() bundle.Object { return &policiesv1.Policy{} }

	leafHubName := agentstatusconfig.GetLeafHubName()
	agentConfig := agentstatusconfig.GetAgentConfigMap()

	localClusterPolicyHistoryEventTransportKey := fmt.Sprintf("%s.%s", leafHubName,
		constants.LocalClusterPolicyStatusEventMsgKey)
	clusterPolicyHistoryEventBundle := grc.NewClusterPolicyHistoryEventBundle(leafHubName, mgr.GetClient())

	localClusterPolicyBundleEntryCollection := []*generic.BundleCollectionEntry{
		generic.NewBundleCollectionEntry(localClusterPolicyHistoryEventTransportKey, clusterPolicyHistoryEventBundle,
			func() bool { return agentConfig.Data["enableLocalPolicies"] == "true" }),
	}

	localClusterPolicyPredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return !helper.HasAnnotation(object, constants.OriginOwnerReferenceAnnotation) &&
			helper.HasLabel(object, rootPolicyLabel)
	})
	if err := generic.NewGenericStatusSyncController(mgr, localPoliciesStatusSyncLog, producer,
		localClusterPolicyBundleEntryCollection, createObjFunc, localClusterPolicyPredicate,
		agentstatusconfig.GetPolicyDuration); err != nil {
		return fmt.Errorf("failed to add local cluster policies controller to the manager - %w", err)
	}

	return nil
}
