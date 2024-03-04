package transport_test

import (
	"context"
	"fmt"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/consumer"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/producer"
)

func TestAssembler(t *testing.T) {
	topic := "test"
	transportConfig := &transport.TransportConfig{
		TransportType: string(transport.Chan),
	}

	genericProducer, err := producer.NewGenericProducer(transportConfig, topic)
	assert.Nil(t, err)
	genericProducer.SetDataLimit(5)

	genericConsumer, err := consumer.NewGenericConsumer(transportConfig, []string{topic})
	assert.Nil(t, err)
	go func() {
		err = genericConsumer.Start(context.TODO())
		assert.Nil(t, err)
	}()

	e := cloudevents.NewEvent()
	e.SetID(uuid.New().String())
	e.SetType("com.cloudevents.sample.sent")
	e.SetSource("https://github.com/cloudevents/sdk-go/samples/kafka/sender")
	_ = e.SetData(cloudevents.ApplicationJSON, map[string]interface{}{
		"id":      0,
		"message": "Hello, World!",
	})

	err = genericProducer.SendEvent(context.TODO(), e)
	assert.Nil(t, err)

	evt := <-genericConsumer.EventChan()
	fmt.Println("whole", evt)
}
