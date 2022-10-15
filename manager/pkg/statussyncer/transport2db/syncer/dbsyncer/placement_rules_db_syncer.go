package dbsyncer

import (
	"github.com/go-logr/logr"

	statusbundle "github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/transport2db/bundle"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/transport2db/db"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/conflator"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

// NewPlacementRulesDBSyncer creates a new instance of genericDBSyncer to sync placement-rules.
func NewPlacementRulesDBSyncer(log logr.Logger) DBSyncer {
	dbSyncer := &genericDBSyncer{
		log:              log,
		transportMsgKey:  constants.PlacementRuleMsgKey,
		dbSchema:         db.StatusSchema,
		dbTableName:      db.PlacementRulesTableName,
		createBundleFunc: statusbundle.NewPlacementRulesBundle,
		bundlePriority:   conflator.PlacementRulePriority,
		bundleSyncMode:   bundle.CompleteStateMode,
	}

	log.Info("initialized placement-rules db syncer")

	return dbSyncer
}
