// Copyright (c) 2023 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package consumer

import (
	"context"
	"fmt"

	"github.com/Shopify/sarama"
	"github.com/cloudevents/sdk-go/protocol/kafka_sarama/v2"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	ceprotocol "github.com/cloudevents/sdk-go/v2/protocol"
	"github.com/cloudevents/sdk-go/v2/protocol/gochan"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/config"
)

type GenericConsumer struct {
	log         logr.Logger
	client      cloudevents.Client
	assembler   *messageAssembler
	messageChan chan *transport.Message
}

func NewGenericConsumer(transportConfig *transport.TransportConfig) (*GenericConsumer, error) {
	log := ctrl.Log.WithName(fmt.Sprintf("%s-consumer", transportConfig.TransportFormat))
	var receiver interface{}
	switch transportConfig.TransportType {
	case string(transport.Kafka):
		log.Info("transport consumer with cloudevents-kafka receiver")
		saramaConfig, err := config.GetSaramaConfig(transportConfig.KafkaConfig)
		if err != nil {
			return nil, err
		}
		// if set this to false, it will consume message from beginning when restart the client
		saramaConfig.Consumer.Offsets.AutoCommit.Enable = true
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
		// set the consumer groupId = clientId
		receiver, err = kafka_sarama.NewConsumer([]string{transportConfig.KafkaConfig.BootstrapServer}, saramaConfig,
			transportConfig.KafkaConfig.ConsumerConfig.ConsumerID,
			transportConfig.KafkaConfig.ConsumerConfig.ConsumerTopic)
		if err != nil {
			return nil, err
		}
	case string(transport.Chan):
		log.Info("transport consumer with go chan receiver")
		if transportConfig.Extends == nil {
			transportConfig.Extends = make(map[string]interface{})
		}
		if _, found := transportConfig.Extends[string(transport.Chan)]; !found {
			transportConfig.Extends[string(transport.Chan)] = gochan.New()
		}
		receiver = transportConfig.Extends[string(transport.Chan)]
	default:
		return nil, fmt.Errorf("transport-type - %s is not a valid option", transportConfig.TransportType)
	}

	client, err := cloudevents.NewClient(receiver)
	if err != nil {
		return nil, err
	}

	return &GenericConsumer{
		log:         log,
		client:      client,
		messageChan: make(chan *transport.Message),
		assembler:   newMessageAssembler(),
	}, nil
}

func (c *GenericConsumer) Start(ctx context.Context) error {
	err := c.client.StartReceiver(ctx, func(ctx context.Context, event cloudevents.Event) ceprotocol.Result {
		c.log.V(2).Info("received message and forward to bundle channel", "event.ID", event.ID())

		chunk, isChunk := c.assembler.messageChunk(event)
		if !isChunk {
			transportMessage := &transport.Message{}
			if err := event.DataAs(transportMessage); err != nil {
				c.log.Error(err, "get transport message error", "event.ID", event.ID())
				return ceprotocol.ResultNACK
			}
			c.messageChan <- transportMessage
			return ceprotocol.ResultACK
		}

		if transportMessage := c.assembler.assemble(chunk); transportMessage != nil {
			c.messageChan <- transportMessage
			return ceprotocol.ResultACK
		}
		return ceprotocol.ResultNACK
	})
	if err != nil {
		return fmt.Errorf("failed to start Receiver: %w", err)
	}
	c.log.Info("receiver stopped\n")
	return nil
}

func (c *GenericConsumer) MessageChan() chan *transport.Message {
	return c.messageChan
}
