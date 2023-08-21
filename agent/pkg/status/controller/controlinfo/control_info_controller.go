package controlinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/bundle"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/bundle/controlinfo"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/config"
	agentstatusconfig "github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/config"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

const (
	controlInfoLogName = "control-info"
)

// LeafHubControlInfoController manages control info bundle traffic.
type LeafHubControlInfoController struct {
	log                     logr.Logger
	bundle                  bundle.Bundle
	transportBundleKey      string
	transport               transport.Producer
	resolveSyncIntervalFunc config.ResolveSyncIntervalFunc
}

// AddControlInfoController creates a new instance of control info controller and adds it to the manager.
func AddControlInfoController(mgr ctrl.Manager, producer transport.Producer) error {
	leafHubName := agentstatusconfig.GetLeafHubName()
	transportBundleKey := fmt.Sprintf("%s.%s", leafHubName, constants.ControlInfoMsgKey)

	controlInfoCtrl := &LeafHubControlInfoController{
		log:                     ctrl.Log.WithName(controlInfoLogName),
		bundle:                  controlinfo.NewControlInfoBundle(leafHubName),
		transportBundleKey:      transportBundleKey,
		transport:               producer,
		resolveSyncIntervalFunc: agentstatusconfig.GetControlInfoDuration,
	}

	if err := mgr.Add(controlInfoCtrl); err != nil {
		return fmt.Errorf("failed to add controller to the manager - %w", err)
	}

	return nil
}

// Start function starts control info controller.
func (c *LeafHubControlInfoController) Start(ctx context.Context) error {
	c.log.Info("Starting Controller")

	go c.periodicSync(ctx)

	<-ctx.Done() // blocking wait for stop event
	c.log.Info("Stopping Controller")

	return nil
}

func (c *LeafHubControlInfoController) periodicSync(ctx context.Context) {
	currentSyncInterval := c.resolveSyncIntervalFunc()
	ticker := time.NewTicker(currentSyncInterval)
	c.syncBundle()

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return

		case <-ticker.C: // wait for next time interval
			c.syncBundle()

			resolvedInterval := c.resolveSyncIntervalFunc()

			// reset ticker if sync interval has changed
			if resolvedInterval != currentSyncInterval {
				currentSyncInterval = resolvedInterval
				ticker.Reset(currentSyncInterval)
				c.log.Info(fmt.Sprintf("sync interval has been reset to %s", currentSyncInterval.String()))
			}
		}
	}
}

func (c *LeafHubControlInfoController) syncBundle() {
	c.bundle.UpdateObject(nil) // increase bundle value version

	payloadBytes, err := json.Marshal(c.bundle)
	if err != nil {
		c.log.Error(err, "marshal controlInfo bundle error", "transportBundleKey", c.transportBundleKey)
	}

	transportMessageKey := c.transportBundleKey
	if deltaStateBundle, ok := c.bundle.(bundle.DeltaStateBundle); ok {
		transportMessageKey = fmt.Sprintf("%s@%d", c.transportBundleKey, deltaStateBundle.GetTransportationID())
	}

	if err := c.transport.Send(context.TODO(), &transport.Message{
		Key:     transportMessageKey,
		ID:      c.transportBundleKey,
		MsgType: constants.StatusBundle,
		Version: c.bundle.GetBundleVersion().String(),
		Payload: payloadBytes,
	}); err != nil {
		c.log.Error(err, "send control info error", "messageId", c.transportBundleKey)
	}
	c.bundle.GetBundleVersion().Next() // increase bundle generation version
}
