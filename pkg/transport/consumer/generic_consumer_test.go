package consumer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm/clause"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	"github.com/stolostron/multicluster-global-hub/pkg/database/models"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/test/pkg/testpostgres"
)

func TestGenerateConsumer(t *testing.T) {
	mockKafkaCluster, err := kafka.NewMockCluster(1)
	if err != nil {
		t.Errorf("failed to init mock kafka cluster - %v", err)
	}
	transportConfig := &transport.TransportConfig{
		TransportType: "kafka",
		KafkaConfig: &transport.KafkaConfig{
			BootstrapServer: mockKafkaCluster.BootstrapServers(),
			EnableTLS:       false,
			ConsumerConfig: &transport.KafkaConsumerConfig{
				ConsumerID:  "test-consumer",
				StatusTopic: "test-topic",
			},
		},
	}
	_, err = NewGenericConsumer(transportConfig, []string{"test-topic"})
	if err != nil && !strings.Contains(err.Error(), "client has run out of available brokers") {
		t.Errorf("failed to generate consumer - %v", err)
	}
}

func TestGetInitOffset(t *testing.T) {
	testPostgres, err := testpostgres.NewTestPostgres()
	assert.Nil(t, err)
	err = testpostgres.InitDatabase(testPostgres.URI)
	assert.Nil(t, err)

	databaseTransports := []models.Transport{}

	databaseTransports = append(databaseTransports, generateTransport("status.hub1", 12))
	databaseTransports = append(databaseTransports, generateTransport("status.hub2", 11))
	databaseTransports = append(databaseTransports, generateTransport("status", 9))
	databaseTransports = append(databaseTransports, generateTransport("spec", 9))

	db := database.GetGorm()
	err = db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).CreateInBatches(databaseTransports, 100).Error
	assert.Nil(t, err)
	offsets, err := getInitOffset()
	assert.Nil(t, err)

	count := 0
	for _, offset := range offsets {
		if *offset.Topic == "spec" {
			t.Fatalf("the topic %s shouldn't be selected", "spec")
		}
		count++
	}
	assert.Equal(t, 3, count)
}

func generateTransport(topic string, offset int64) models.Transport {
	payload, _ := json.Marshal(metadata.TransportPosition{
		Topic:     topic,
		Partition: 0,
		Offset:    int64(offset),
	})
	return models.Transport{
		Name:    topic,
		Payload: payload,
	}
}
