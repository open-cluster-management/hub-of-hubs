package config

import (
	"context"
	"fmt"
	"log"
	"os"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	KAFKA_USER      = "global-hub-kafka-user"
	KAFKA_CLUSTER   = "kafka"
	KAFKA_NAMESPACE = "multicluster-global-hub"
)

func GetConfluentConfigMap(isProducer bool) (*kafka.ConfigMap, error) {
	secret, err := GetTransportSecret()
	if err != nil {
		log.Fatalf("failed to get transport secret: %v", err)
		return nil, err
	}
	bootStrapServer := string(secret.Data["bootstrap_server"])

	caCrtPath := "/tmp/ca.crt"
	err = os.WriteFile(caCrtPath, secret.Data["ca.crt"], 0o600)
	if err != nil {
		log.Fatalf("failed to write ca.crt: %v", err)
		return nil, err
	}

	clientCrtPath := "/tmp/client.crt"
	err = os.WriteFile(clientCrtPath, secret.Data["client.crt"], 0o600)
	if err != nil {
		log.Fatalf("failed to write client.crt: %v", err)
		return nil, err
	}

	clientKeyPath := "/tmp/client.key"
	err = os.WriteFile(clientKeyPath, secret.Data["client.key"], 0o600)
	if err != nil {
		log.Fatalf("failed to write client.key: %v", err)
		return nil, err
	}

	kafkaConfig := &transport.KafkaConfig{
		BootstrapServer: bootStrapServer,
		EnableTLS:       true,
		CaCertPath:      caCrtPath,
		ClientCertPath:  clientCrtPath,
		ClientKeyPath:   clientKeyPath,
	}
	configMap, err := config.GetConfluentConfigMap(kafkaConfig, isProducer)
	if err != nil {
		log.Fatalf("failed to get confluent config map: %v", err)
		return nil, err
	}
	return configMap, nil
}

func GetConfluentConfigMapByKafkaUser(isProducer bool) (*kafka.ConfigMap, error) {
	kubeconfig, err := loadDynamicKubeConfig(EnvKubconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig")
	}

	userName := KAFKA_USER
	name := os.Getenv("KAFKA_USER")
	if name != "" {
		userName = name
	}
	consumerGroupID := userName
	groupID := os.Getenv("CONSUMER_GROUP_ID")
	if groupID != "" {
		consumerGroupID = groupID
	}

	kafkav1beta2.AddToScheme(scheme.Scheme)
	c, err := client.New(kubeconfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime client")
	}

	kafkaCluster := &kafkav1beta2.Kafka{}
	err = c.Get(context.TODO(), types.NamespacedName{
		Name:      KAFKA_CLUSTER,
		Namespace: KAFKA_NAMESPACE,
	}, kafkaCluster)
	if err != nil {
		return nil, err
	}

	bootstrapServer := *kafkaCluster.Status.Listeners[1].BootstrapServers

	kafkaUserSecret := &corev1.Secret{}
	err = c.Get(context.TODO(), types.NamespacedName{
		Name:      userName,
		Namespace: KAFKA_NAMESPACE,
	}, kafkaUserSecret)
	if err != nil {
		return nil, err
	}

	caCrtPath := "/tmp/ca.crt"
	err = os.WriteFile(caCrtPath, []byte(kafkaCluster.Status.Listeners[1].Certificates[0]), 0o600)
	if err != nil {
		log.Fatalf("failed to write ca.crt: %v", err)
		return nil, err
	}

	clientCrtPath := "/tmp/client.crt"
	err = os.WriteFile(clientCrtPath, kafkaUserSecret.Data["user.crt"], 0o600)
	if err != nil {
		log.Fatalf("failed to write client.crt: %v", err)
		return nil, err
	}

	clientKeyPath := "/tmp/client.key"
	err = os.WriteFile(clientKeyPath, kafkaUserSecret.Data["user.key"], 0o600)
	if err != nil {
		log.Fatalf("failed to write client.key: %v", err)
		return nil, err
	}

	kafkaConfig := &transport.KafkaConfig{
		BootstrapServer: bootstrapServer,
		EnableTLS:       true,
		CaCertPath:      caCrtPath,
		ClientCertPath:  clientCrtPath,
		ClientKeyPath:   clientKeyPath,
		ConsumerConfig: &transport.KafkaConsumerConfig{
			ConsumerID: consumerGroupID,
		},
	}
	configMap, err := config.GetConfluentConfigMap(kafkaConfig, isProducer)
	if err != nil {
		log.Fatalf("failed to get confluent config map: %v", err)
		return nil, err
	}
	return configMap, nil
}
