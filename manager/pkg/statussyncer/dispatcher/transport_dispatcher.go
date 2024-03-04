package dispatcher

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/config"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/conflator"
	"github.com/stolostron/multicluster-global-hub/pkg/statistics"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	genericconsumer "github.com/stolostron/multicluster-global-hub/pkg/transport/consumer"
)

// Get message from transport, convert it to bundle and forward it to conflation manager.
type TransportDispatcher struct {
	log               logr.Logger
	consumer          transport.Consumer
	conflationManager *conflator.ConflationManager
	statistic         *statistics.Statistics
}

func AddTransportDispatcher(mgr ctrl.Manager, conflationManager *conflator.ConflationManager,
	managerConfig *config.ManagerConfig, stats *statistics.Statistics,
) error {
	// start a consumer
	topics := managerConfig.TransportConfig.KafkaConfig.Topics
	consumer, err := genericconsumer.NewGenericConsumer(managerConfig.TransportConfig,
		[]string{topics.EventTopic, topics.StatusTopic},
		genericconsumer.EnableDatabaseOffset(true),
		genericconsumer.EnableEventChan(managerConfig.TransportConfig.KafkaConfig.ConsumerConfig.EnableEventChan))
	if err != nil {
		return fmt.Errorf("failed to initialize transport consumer: %w", err)
	}
	if err := mgr.Add(consumer); err != nil {
		return fmt.Errorf("failed to add transport consumer to manager: %w", err)
	}

	transportDispatcher := &TransportDispatcher{
		log:               ctrl.Log.WithName("conflation-dispatcher"),
		consumer:          consumer,
		conflationManager: conflationManager,
		statistic:         stats,
	}
	if err := mgr.Add(transportDispatcher); err != nil {
		return fmt.Errorf("failed to add transport dispatcher to runtime manager: %w", err)
	}
	return nil
}

// Start function starts bundles status syncer.
func (d *TransportDispatcher) Start(ctx context.Context) error {
	d.log.Info("transport dispatcher starts dispatching received bundles...")

	go d.dispatch(ctx)

	<-ctx.Done() // blocking wait for stop event
	d.log.Info("stopped dispatching bundles")

	return nil
}

func (d *TransportDispatcher) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-d.consumer.EventChan():
			d.statistic.ReceivedEvent(evt)
			d.log.V(2).Info("forward received event to conflation", "event type", evt.Type())
			d.conflationManager.Insert(evt)
		}
	}
}
