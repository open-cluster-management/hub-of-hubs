// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controller

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/config"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/apps"
	agentstatusconfig "github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/config"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/event"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/generic"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/hubcluster"
	localpolicies "github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/local_policies"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/localplacement"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/managedclusters"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/placement"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/policies"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	transportproducer "github.com/stolostron/multicluster-global-hub/pkg/transport/producer"
)

// AddControllers adds all the controllers to the Manager.
func AddControllers(ctx context.Context, mgr ctrl.Manager, agentConfig *config.AgentConfig) error {
	if err := agentstatusconfig.AddConfigController(mgr, agentConfig); err != nil {
		return fmt.Errorf("failed to add ConfigMap controller: %w", err)
	}

	// only use the cloudevents
	producer, err := transportproducer.NewGenericProducer(agentConfig.TransportConfig)
	if err != nil {
		return fmt.Errorf("failed to init status transport producer: %w", err)
	}

	_, err = policies.AddPolicyStatusSyncer(mgr, producer)
	if err != nil {
		return fmt.Errorf("failed to add PoliciesStatusController controller: %w", err)
	}
	// // support delta bundle sync mode
	// if isAsync {
	// 	kafkaProducer, ok := producer.(*transportproducer.KafkaProducer)
	// 	if !ok {
	// 		return fmt.Errorf("failed to set the kafka message producer callback() which is to switch the sync mode")
	// 	}
	// 	hybirdSyncManger.SetHybridModeCallBack(agentConfig.StatusDeltaCountSwitchFactor, kafkaProducer)
	// }

	addControllerFunctions := []func(ctrl.Manager, transport.Producer) error{
		managedclusters.AddMangedClusterSyncer,
		// apps.AddSubscriptionStatusesController,
		localpolicies.AddLocalRootPolicySyncer,
		// localpolicies.AddLocalReplicatedPolicySyncer,
		hubcluster.AddHubClusterInfoSyncer,
		hubcluster.AddHeartbeatStatusSyncer,
	}

	if agentConfig.EnableGlobalResource {
		addControllerFunctions = append(addControllerFunctions,
			placement.AddPlacementRulesController,
			placement.AddPlacementSyncer,
			placement.AddPlacementDecisionsController,
			apps.AddSubscriptionReportsSyncer,
			localplacement.AddLocalPlacementRulesController,
		)
	}

	for _, addControllerFunction := range addControllerFunctions {
		if err := addControllerFunction(mgr, producer); err != nil {
			return fmt.Errorf("failed to add controller: %w", err)
		}
	}

	// event syncer
	err = generic.LaunchGenericObjectSyncer(mgr, event.NewEventSyncer(), producer,
		[]generic.EventEmitter{
			event.NewLocalRootPolicyEmitter(ctx, mgr.GetClient()),
			// event.NewLocalReplicatedPolicyEmitter(ctx, mgr.GetClient()),
		})
	if err != nil {
		return fmt.Errorf("failed to launch event syncer: %w", err)
	}

	// local policy syncer
	err = generic.LaunchGenericObjectSyncer(mgr, localpolicies.NewLocalPolicySyncer(), producer,
		[]generic.EventEmitter{
			localpolicies.StatusEventEmitter(ctx, mgr.GetClient()),
		})
	if err != nil {
		return fmt.Errorf("failed to launch local policy syncer: %w", err)
	}
	return nil
}
