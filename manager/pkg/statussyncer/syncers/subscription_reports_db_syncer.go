package dbsyncer

import (
	"github.com/go-logr/logr"

	statusbundle "github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/conflator"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
)

// NewSubscriptionReportsDBSyncer creates a new instance of genericDBSyncer to sync subscription-reports.
func NewSubscriptionReportsDBSyncer(log logr.Logger) Syncer {
	dbSyncer := &genericStatusSyncer{
		log:              log,
		transportMsgKey:  constants.SubscriptionReportMsgKey,
		dbSchema:         database.StatusSchema,
		dbTableName:      database.SubscriptionReportsTableName,
		createBundleFunc: statusbundle.NewSubscriptionReportsBundle,
		bundlePriority:   conflator.SubscriptionReportPriority,
		bundleSyncMode:   bundle.CompleteStateMode,
	}

	log.Info("initialized subscription-reports db syncer")

	return dbSyncer
}
