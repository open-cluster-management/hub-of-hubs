package dbsyncer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/go-logr/logr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/conflator"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/generic"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/database/models"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
)

type managedClusterHandler struct {
	log           logr.Logger
	eventType     string
	eventSyncMode metadata.EventSyncMode
	eventPriority conflator.ConflationPriority
}

func NewManagedClusterHandler() Handler {
	eventType := string(enum.ManagedClusterType)
	logName := strings.Replace(eventType, enum.EventTypePrefix, "", -1)
	return &managedClusterHandler{
		log:           ctrl.Log.WithName(logName),
		eventType:     eventType,
		eventSyncMode: metadata.CompleteStateMode,
		eventPriority: conflator.ManagedClustersPriority,
	}
}

func (h *managedClusterHandler) RegisterHandler(conflationManager *conflator.ConflationManager) {
	conflationManager.Register(conflator.NewConflationRegistration(
		h.eventPriority,
		h.eventSyncMode,
		h.eventType,
		h.handleEvent,
	))
}

func (h *managedClusterHandler) handleEvent(ctx context.Context, evt *cloudevents.Event) error {
	version := evt.Extensions()[metadata.ExtVersion]
	leafHubName := evt.Source()
	h.log.V(2).Info(startMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)

	data := generic.GenericObjectData{}
	if err := evt.DataAs(data); err != nil {
		return err
	}

	db := database.GetGorm()
	clusterIdToVersionMapFromDB, err := getClusterIdToVersionMap(db, leafHubName)
	if err != nil {
		return fmt.Errorf("failed fetching leaf hub managed clusters from db - %w", err)
	}

	// batch update/insert managed clusters
	batchManagedClusters := []models.ManagedCluster{}
	for _, object := range data {
		cluster, ok := object.(*clusterv1.ManagedCluster)
		if !ok {
			continue
		}

		// Initially, if the clusterID is not exist we will skip it until we get it from ClusterClaim
		clusterId := ""
		for _, claim := range cluster.Status.ClusterClaims {
			if claim.Name == "id.k8s.io" {
				clusterId = claim.Value
				break
			}
		}
		if clusterId == "" {
			continue
		}

		payload, err := json.Marshal(cluster)
		if err != nil {
			return err
		}

		clusterVersionFromDB, exist := clusterIdToVersionMapFromDB[clusterId]
		if !exist {
			batchManagedClusters = append(batchManagedClusters, models.ManagedCluster{
				ClusterID:   clusterId,
				LeafHubName: leafHubName,
				Payload:     payload,
				Error:       database.ErrorNone,
			})
			continue
		}

		// remove the handled object from the map
		delete(clusterIdToVersionMapFromDB, clusterId)

		if cluster.GetResourceVersion() == clusterVersionFromDB {
			continue // update cluster in db only if what we got is a different (newer) version of the resource
		}

		batchManagedClusters = append(batchManagedClusters, models.ManagedCluster{
			ClusterID:   clusterId,
			LeafHubName: leafHubName,
			Payload:     payload,
			Error:       database.ErrorNone,
		})
	}
	err = db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).CreateInBatches(batchManagedClusters, 100).Error
	if err != nil {
		return err
	}

	// delete objects that in the db but were not sent in the bundle (leaf hub sends only living resources).
	// https://gorm.io/docs/delete.html#Soft-Delete
	err = db.Transaction(func(tx *gorm.DB) error {
		for clusterId := range clusterIdToVersionMapFromDB {
			e := tx.Where(&models.ManagedCluster{
				LeafHubName: leafHubName,
				ClusterID:   clusterId,
			}).Delete(&models.ManagedCluster{}).Error
			if e != nil {
				return e
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed deleting managed clusters - %w", err)
	}

	h.log.V(2).Info(finishMessage, "type", evt.Type(), "LH", evt.Source(), "version", version)
	return nil
}

func getClusterIdToVersionMap(db *gorm.DB, leafHubName string) (map[string]string, error) {
	var resourceVersions []models.ResourceVersion

	err := db.Select("cluster_id AS key, payload->'metadata'->>'resourceVersion' AS resource_version").
		Where(&models.ManagedCluster{
			LeafHubName: leafHubName,
		}).Find(&models.ManagedCluster{}).Scan(&resourceVersions).Error
	if err != nil {
		return nil, err
	}
	nameToVersionMap := make(map[string]string)
	for _, resource := range resourceVersions {
		nameToVersionMap[resource.Key] = resource.ResourceVersion
	}
	return nameToVersionMap, nil
}
