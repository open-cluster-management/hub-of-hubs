package dbsyncer_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle/status"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clustersv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

var _ = Describe("PlacementDbSyncer", Ordered, func() {
	const (
		leafHubName = "hub1"
		testSchema  = database.StatusSchema
		testTable   = database.PlacementsTableName
		messageKey  = constants.PlacementMsgKey
	)

	BeforeAll(func() {
		By("Create placements table in database")
		_, err := transportPostgreSQL.GetConn().Exec(ctx, `
			CREATE SCHEMA IF NOT EXISTS status;
			CREATE TABLE IF NOT EXISTS  status.placements (
				id uuid NOT NULL,
				leaf_hub_name character varying(63) NOT NULL,
				payload jsonb NOT NULL
			);
		`)
		Expect(err).ToNot(HaveOccurred())

		By("Check whether the tables are created")
		Eventually(func() error {
			rows, err := transportPostgreSQL.GetConn().Query(ctx, "SELECT * FROM pg_tables")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				columnValues, _ := rows.Values()
				schema := columnValues[0]
				table := columnValues[1]
				if schema == testSchema && table == testTable {
					return nil
				}
			}
			return fmt.Errorf("failed to create table %s.%s", testSchema, testTable)
		}, 10*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
	})

	It("sync the Placements bundle", func() {
		By("Create Placements bundle")
		obj := &clustersv1beta1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testPlacements",
				Namespace: "default",
				Annotations: map[string]string{
					constants.OriginOwnerReferenceAnnotation: "2aa5547c-c172-47ed-b70b-db468c84d327",
				},
			},
		}
		statusBundle := &GenericStatusBundle{
			Objects:           make([]Object, 0),
			LeafHubName:       leafHubName,
			BundleVersion:     status.NewBundleVersion(0, 0),
			manipulateObjFunc: nil,
			lock:              sync.Mutex{},
		}
		statusBundle.Objects = append(statusBundle.Objects, obj)

		By("Create transport message")
		// increment the version
		statusBundle.BundleVersion.Generation++
		payloadBytes, err := json.Marshal(statusBundle)
		Expect(err).ShouldNot(HaveOccurred())

		transportMessageKey := fmt.Sprintf("%s.%s", leafHubName, messageKey)
		transportMessage := &transport.Message{
			Key:     transportMessageKey,
			ID:      transportMessageKey,
			MsgType: constants.StatusBundle,
			Version: statusBundle.BundleVersion.String(),
			Payload: payloadBytes,
		}

		By("Sync message with transport")
		kafkaProducer.SendAsync(transportMessage)

		By("Check the managed cluster table")
		Eventually(func() error {
			querySql := fmt.Sprintf("SELECT leaf_hub_name,payload FROM %s.%s", testSchema, testTable)
			rows, err := transportPostgreSQL.GetConn().Query(ctx, querySql)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var hubName string
				placement := clustersv1beta1.Placement{}
				if err := rows.Scan(&hubName, &placement); err != nil {
					return err
				}
				if hubName == statusBundle.LeafHubName &&
					placement.Name == statusBundle.Objects[0].GetName() {
					return nil
				}
			}
			return fmt.Errorf("failed to sync content of table %s.%s", testSchema, testTable)
		}, 30*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
	})
})
