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

type policyComplianceHandler struct {
	log           logr.Logger
	eventType     string
	eventSyncMode metadata.EventSyncMode
	eventPriority conflator.ConflationPriority
}

func NewPolicyComplianceHandler() Handler {
	eventType := string(enum.ComplianceType)
	logName := strings.Replace(eventType, enum.EventTypePrefix, "", -1)
	return &policyComplianceHandler{
		log:           ctrl.Log.WithName(logName),
		eventType:     eventType,
		eventSyncMode: metadata.CompleteStateMode,
		eventPriority: conflator.CompliancePriority,
	}
}

func (h *policyComplianceHandler) RegisterHandler(conflationManager *conflator.ConflationManager) {
	conflationManager.Register(conflator.NewConflationRegistration(
		h.eventPriority,
		h.eventSyncMode,
		h.eventType,
		h.handleEvent,
	))
}

// if we got inside the handler function, then the bundle version is newer than what was already handled.
// handling clusters per policy bundle inserts or deletes rows from/to the compliance table.
// in case the row already exists (leafHubName, policyId, clusterName) it updates the compliance status accordingly.
// this bundle is triggered only when policy was added/removed or when placement rule has changed which caused list of
// clusters (of at least one policy) to change.
// in other cases where only compliance status change, only compliance bundle is received.
func (h *policyComplianceHandler) handleEvent(ctx context.Context, evt *cloudevents.Event) error {
	version := evt.Extensions()[metadata.ExtVersion]
	leafHubName := evt.Source()
	h.log.V(2).Info(startMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)

	data := grc.ComplianceData{}
	if err := evt.DataAs(&data); err != nil {
		return err
	}

	var compliancesFromDB []models.StatusCompliance
	db := database.GetGorm()
	err := db.Where(&models.StatusCompliance{LeafHubName: leafHubName}).Find(&compliancesFromDB).Error
	if err != nil {
		return err
	}

	// policyID: { compliance: (cluster1, cluster2), nonCompliance: (cluster3, cluster4), unknowns: (cluster5) }
	allComplianceClustersFromDB, err := getComplianceClusterSets(db, "leaf_hub_name = ?", leafHubName)

	for _, eventCompliance := range data { // every object is clusters list per policy with full state

		policyID := eventCompliance.PolicyID
		complianceClustersFromDB, policyExistsInDB := allComplianceClustersFromDB[policyID]
		if !policyExistsInDB {
			complianceClustersFromDB = NewPolicyClusterSets()
		}

		// handle compliant clusters of the policy
		compliantCompliances := newCompliances(leafHubName, policyID, database.Compliant,
			eventCompliance.CompliantClusters, complianceClustersFromDB.GetClusters(database.Compliant))

		// handle non compliant clusters of the policy
		nonCompliantCompliances := newCompliances(leafHubName, policyID, database.NonCompliant,
			eventCompliance.NonCompliantClusters, complianceClustersFromDB.GetClusters(database.NonCompliant))

		// handle unknown compliance clusters of the policy
		unknownCompliances := newCompliances(leafHubName, policyID, database.Unknown,
			eventCompliance.UnknownComplianceClusters, complianceClustersFromDB.GetClusters(database.Unknown))

		batchCompliances := []models.StatusCompliance{}
		batchCompliances = append(batchCompliances, compliantCompliances...)
		batchCompliances = append(batchCompliances, nonCompliantCompliances...)
		batchCompliances = append(batchCompliances, unknownCompliances...)

		// batch upsert
		err = db.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).CreateInBatches(batchCompliances, 100).Error
		if err != nil {
			return err
		}

		// delete compliance status rows in the db that were not sent in the bundle (leaf hub sends only living resources)
		allClustersOnDB := complianceClustersFromDB.GetAllClusters()
		for _, compliance := range batchCompliances {
			allClustersOnDB.Remove(compliance.ClusterName)
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			for _, name := range allClustersOnDB.ToSlice() {
				clusterName, ok := name.(string)
				if !ok {
					continue
				}
				err := tx.Where(&models.StatusCompliance{
					LeafHubName: leafHubName,
					PolicyID:    policyID,
					ClusterName: clusterName,
				}).Delete(&models.StatusCompliance{}).Error
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
			err := tx.Where(&models.StatusCompliance{
				PolicyID: policyID,
			}).Delete(&models.StatusCompliance{}).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to handle compliance event - %w", err)
	}

	h.log.V(2).Info(finishMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)
	return nil
}

func newCompliances(leafHub, policyID string, compliance database.ComplianceStatus,
	eventComplianceClusters []string, complianceClustersOnDB set.Set,
) []models.StatusCompliance {
	clusters := newComplianceClusters(eventComplianceClusters, complianceClustersOnDB)

	compliances := make([]models.StatusCompliance, 0)
	for _, cluster := range clusters {
		compliances = append(compliances, models.StatusCompliance{
			LeafHubName: leafHub,
			PolicyID:    policyID,
			ClusterName: cluster,
			Error:       database.ErrorNone,
			Compliance:  compliance,
		})
	}
	return compliances
}

func getComplianceClusterSets(db *gorm.DB, query interface{}, args ...interface{}) (
	map[string]*PolicyClustersSets, error,
) {
	var compliancesFromDB []models.StatusCompliance
	err := db.Where(query, args...).Find(&compliancesFromDB).Error
	if err != nil {
		return nil, err
	}

	// policyID: { compliance: (cluster1, cluster2), nonCompliance: (cluster3, cluster4), unknowns: (cluster5) }
	policyComplianceRowsFromDB := make(map[string]*PolicyClustersSets)
	for _, compliance := range compliancesFromDB {
		if _, ok := policyComplianceRowsFromDB[compliance.PolicyID]; !ok {
			policyComplianceRowsFromDB[compliance.PolicyID] = NewPolicyClusterSets()
		}
		policyComplianceRowsFromDB[compliance.PolicyID].AddCluster(compliance.ClusterName, compliance.Compliance)
	}
	return policyComplianceRowsFromDB, nil
}
