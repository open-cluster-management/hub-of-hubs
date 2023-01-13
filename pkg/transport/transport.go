// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package transport

import (
	"context"
	"time"

	"github.com/stolostron/multicluster-global-hub/pkg/transport/protocol"
)

type TransportType string

const (
	Kafka TransportType = "kafka"
	Chan  TransportType = "chan" // only for test
)

type Consumer interface {
	// It will start the underlying receiver protocol as it has been configured. This call is blocking
	Start(ctx context.Context) error
}

type Producer interface {
	Send(ctx context.Context, message *Message) error
	// TODO: Do we need the stop method to shut down the protocol(kafka)
}

// Message abstracts a message object to be used by different transport components.
type Message struct {
	Destination string `json:"destination"`
	Key         string `json:"key"`
	ID          string `json:"id"`
	MsgType     string `json:"msgType"`
	Version     string `json:"version"`
	Payload     []byte `json:"payload"`
}

type TransportConfig struct {
	TransportType          string
	MessageCompressionType string
	CommitterInterval      time.Duration
	KafkaConfig            *protocol.KafkaConfig
	Extends                map[string]interface{}
}

const (
	// DestinationHub is the key used for destination-hub name header.
	DestinationHub = "destination-hub"
	// CompressionType is the key used for compression type header.
	CompressionType = "content-encoding"
	// Size is the key used for total bundle size header.
	Size = "size"
	// Offset is the key used for message fragment offset header.
	Offset = "offset"
	// FragmentationTimestamp is the key used for bundle fragmentation time header.
	FragmentationTimestamp = "fragmentation-timestamp"
	// Broadcast can be used as destination when a bundle should be broadcasted.
	Broadcast = ""
)

const (
	metricsHost                = "0.0.0.0"
	metricsPort          int32 = 8384
	webhookPort                = 9443
	webhookCertDir             = "/webhook-certs"
	kafkaTransportType         = "kafka"
	leaderElectionLockID       = "multicluster-global-hub-lock"
)
