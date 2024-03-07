package dbsyncer

import (
	"context"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	set "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/conflator"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/grc"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/database/models"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
)

type localPolicyComplianceHandler struct {
	log           logr.Logger
	eventType     string
	eventSyncMode metadata.EventSyncMode
	eventPriority conflator.ConflationPriority
}

func NewLocalPolicyComplianceHandler() Handler {
	eventType := string(enum.LocalComplianceType)
	logName := strings.Replace(eventType, enum.EventTypePrefix, "", -1)
	return &localPolicyComplianceHandler{
		log:           ctrl.Log.WithName(logName),
		eventType:     eventType,
		eventSyncMode: metadata.CompleteStateMode,
		eventPriority: conflator.LocalCompliancePriority,
	}
}

func (h *localPolicyComplianceHandler) RegisterHandler(conflationManager *conflator.ConflationManager) {
	conflationManager.Register(conflator.NewConflationRegistration(
		h.eventPriority,
		h.eventSyncMode,
		h.eventType,
		h.handleEventWrapper,
	))
}

func (h *localPolicyComplianceHandler) handleEventWrapper(ctx context.Context, evt *cloudevents.Event) error {
	return handleCompliance(h.log, ctx, evt)
}

func handleCompliance(log logr.Logger, ctx context.Context, evt *cloudevents.Event) error {
	version := evt.Extensions()[metadata.ExtVersion]
	leafHub := evt.Source()
	log.V(2).Info(startMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)

	data := grc.ComplianceData{}
	if err := evt.DataAs(&data); err != nil {
		return err
	}

	db := database.GetGorm()
	// policyID: { compliance: (cluster1, cluster2), nonCompliance: (cluster3, cluster4), unknowns: (cluster5) }
	allComplianceClustersFromDB, err := getLocalComplianceClusterSets(db, "leaf_hub_name = ?", leafHub)
	if err != nil {
		return err
	}

	for _, eventCompliance := range data { // every object is clusters list per policy with full state

		policyID := eventCompliance.PolicyID
		complianceClustersFromDB, policyExistsInDB := allComplianceClustersFromDB[policyID]
		if !policyExistsInDB {
			complianceClustersFromDB = NewPolicyClusterSets()
		}

		// handle compliant clusters of the policy
		compliantCompliances := newLocalCompliances(leafHub, policyID, database.Compliant,
			eventCompliance.CompliantClusters, complianceClustersFromDB.GetClusters(database.Compliant))

		// handle non compliant clusters of the policy
		nonCompliantCompliances := newLocalCompliances(leafHub, policyID, database.NonCompliant,
			eventCompliance.NonCompliantClusters, complianceClustersFromDB.GetClusters(database.NonCompliant))

		// handle unknown compliance clusters of the policy
		unknownCompliances := newLocalCompliances(leafHub, policyID, database.Unknown,
			eventCompliance.UnknownComplianceClusters, complianceClustersFromDB.GetClusters(database.Unknown))

		batchLocalCompliances := []models.LocalStatusCompliance{}
		batchLocalCompliances = append(batchLocalCompliances, compliantCompliances...)
		batchLocalCompliances = append(batchLocalCompliances, nonCompliantCompliances...)
		batchLocalCompliances = append(batchLocalCompliances, unknownCompliances...)

		// batch upsert
		err = db.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).CreateInBatches(batchLocalCompliances, 100).Error
		if err != nil {
			return err
		}

		// delete compliance status rows in the db that were not sent in the bundle (leaf hub sends only living resources)
		allClustersOnDB := complianceClustersFromDB.GetAllClusters()
		for _, compliance := range batchLocalCompliances {
			allClustersOnDB.Remove(compliance.ClusterName)
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			for _, name := range allClustersOnDB.ToSlice() {
				clusterName, ok := name.(string)
				if !ok {
					continue
				}
				err := tx.Where(&models.LocalStatusCompliance{
					LeafHubName: leafHub,
					PolicyID:    policyID,
					ClusterName: clusterName,
				}).Delete(&models.LocalStatusCompliance{}).Error
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to handle clusters per policy bundle - %w", err)
		}
		// keep this policy in db, should remove from db only policies that were not sent in the bundle
		delete(allComplianceClustersFromDB, policyID)
	}

	// delete the policy isn't contained on the bundle
	err = db.Transaction(func(tx *gorm.DB) error {
		for policyID := range allComplianceClustersFromDB {
			err := tx.Where(&models.LocalStatusCompliance{
				PolicyID: policyID,
			}).Delete(&models.LocalStatusCompliance{}).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to handle local compliance event - %w", err)
	}

	log.V(2).Info(finishMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)
	return nil
}

func newComplianceClusters(eventComplianceClusters []string, complianceClusters set.Set) []string {
	newComplianceClusters := make([]string, 0)
	for _, clusterName := range eventComplianceClusters {
		if !complianceClusters.Contains(clusterName) {
			newComplianceClusters = append(newComplianceClusters, clusterName)
		}
	}
	return newComplianceClusters
}

func newLocalCompliances(leafHub, policyID string, compliance database.ComplianceStatus,
	eventComplianceClusters []string, complianceClustersOnDB set.Set,
) []models.LocalStatusCompliance {
	clusters := newComplianceClusters(eventComplianceClusters, complianceClustersOnDB)

	compliances := make([]models.LocalStatusCompliance, 0)
	for _, cluster := range clusters {
		compliances = append(compliances, models.LocalStatusCompliance{
			LeafHubName: leafHub,
			PolicyID:    policyID,
			ClusterName: cluster,
			Error:       database.ErrorNone,
			Compliance:  compliance,
		})
	}
	return compliances
}

func getLocalComplianceClusterSets(db *gorm.DB, query interface{}, args ...interface{}) (
	map[string]*PolicyClustersSets, error,
) {
	var compliancesFromDB []models.LocalStatusCompliance
	err := db.Where(query, args...).Find(&compliancesFromDB).Error
	if err != nil {
		return nil, err
	}

	// policyID: { compliance: (cluster1, cluster2), nonCompliance: (cluster3, cluster4), unknowns: (cluster5) }
	allPolicyComplianceRowsFromDB := make(map[string]*PolicyClustersSets)
	for _, compliance := range compliancesFromDB {
		if _, ok := allPolicyComplianceRowsFromDB[compliance.PolicyID]; !ok {
			allPolicyComplianceRowsFromDB[compliance.PolicyID] = NewPolicyClusterSets()
		}
		allPolicyComplianceRowsFromDB[compliance.PolicyID].AddCluster(
			compliance.ClusterName, compliance.Compliance)
	}
	return allPolicyComplianceRowsFromDB, nil
}
