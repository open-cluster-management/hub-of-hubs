package dbsyncer

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	ctrl "sigs.k8s.io/controller-runtime"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/go-logr/logr"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/conflator"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/conflator/dependency"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/grc"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/database/models"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
)

type policyDeltaComplianceHandler struct {
	log            logr.Logger
	eventType      string
	dependencyType string
	eventSyncMode  metadata.EventSyncMode
	eventPriority  conflator.ConflationPriority
}

func NewPolicyDeltaComplianceHandler() Handler {
	eventType := string(enum.DeltaComplianceType)
	logName := strings.Replace(eventType, enum.EventTypePrefix, "", -1)
	return &policyDeltaComplianceHandler{
		log:            ctrl.Log.WithName(logName),
		eventType:      eventType,
		dependencyType: string(enum.CompleteComplianceType),
		eventSyncMode:  metadata.DeltaStateMode,
		eventPriority:  conflator.DeltaCompliancePriority,
	}
}

func (h *policyDeltaComplianceHandler) RegisterHandler(conflationManager *conflator.ConflationManager) {
	registration := conflator.NewConflationRegistration(
		h.eventPriority,
		h.eventSyncMode,
		h.eventType,
		h.handleEvent,
	)
	registration.WithDependency(dependency.NewDependency(h.dependencyType, dependency.ExactMatch))
	conflationManager.Register(registration)
}

// if we got to the handler function, then the bundle pre-conditions were satisfied.
func (h *policyDeltaComplianceHandler) handleEvent(ctx context.Context, evt *cloudevents.Event) error {
	version := evt.Extensions()[metadata.ExtVersion]
	leafHub := evt.Source()
	h.log.V(2).Info(startMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)

	data := grc.ComplianceData{}
	if err := evt.DataAs(&data); err != nil {
		return err
	}

	db := database.GetGorm()
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, eventCompliance := range data { // every object in bundle is policy generic compliance status

			for _, cluster := range eventCompliance.CompliantClusters {
				err := updateCompliance(tx, eventCompliance.PolicyID, leafHub, cluster, database.Compliant)
				if err != nil {
					return err
				}
			}

			for _, cluster := range eventCompliance.NonCompliantClusters {
				err := updateCompliance(tx, eventCompliance.PolicyID, leafHub, cluster, database.NonCompliant)
				if err != nil {
					return err
				}
			}

			for _, cluster := range eventCompliance.UnknownComplianceClusters {
				err := updateCompliance(tx, eventCompliance.PolicyID, leafHub, cluster, database.Unknown)
				if err != nil {
					return err
				}
			}
		}

		// return nil will commit the whole transaction
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to handle delta compliance bundle - %w", err)
	}

	h.log.V(2).Info(finishMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)
	return nil
}

func updateCompliance(tx *gorm.DB, policyID, leafHub, cluster string, compliance database.ComplianceStatus) error {
	return tx.Model(&models.StatusCompliance{}).Where(&models.StatusCompliance{
		PolicyID:    policyID,
		ClusterName: cluster,
		LeafHubName: leafHub,
	}).Updates(&models.StatusCompliance{
		Compliance: compliance,
	}).Error
}
