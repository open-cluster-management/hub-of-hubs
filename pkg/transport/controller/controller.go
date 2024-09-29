package controller

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/config"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/consumer"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/producer"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/requester"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

type TransportCallback func(transportClient transport.TransportClient) error

type TransportCtrl struct {
	runtimeClient    client.Client
	secretNamespace  string
	secretName       string
	extraSecretNames []string

	transportConfig *transport.TransportInternalConfig

	// the use the producer and consumer to activate the call back funciton, once it executed successful, then clear it.
	transportCallback TransportCallback
	transportClient   *TransportClient

	mutex sync.Mutex
}

type TransportClient struct {
	consumer  transport.Consumer
	producer  transport.Producer
	requester transport.Requester
}

func (c *TransportClient) GetProducer() transport.Producer {
	return c.producer
}

func (c *TransportClient) GetConsumer() transport.Consumer {
	return c.consumer
}

func (c *TransportClient) GetRequester() transport.Requester {
	return c.requester
}

func NewTransportCtrl(namespace, name string, callback TransportCallback,
	transportConfig *transport.TransportInternalConfig,
) *TransportCtrl {
	return &TransportCtrl{
		secretNamespace:   namespace,
		secretName:        name,
		transportCallback: callback,
		transportClient:   &TransportClient{},
		transportConfig:   transportConfig,
		extraSecretNames:  make([]string, 2),
	}
}

func (c *TransportCtrl) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: c.secretNamespace,
			Name:      c.secretName,
		},
	}
	if err := c.runtimeClient.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		return ctrl.Result{}, err
	}

	_, isKafka := secret.Data["kafka.yaml"]
	if isKafka {
		c.transportConfig.TransportType = string(transport.Kafka)
	}

	_, isRestful := secret.Data["rest.yaml"]
	if isRestful {
		c.transportConfig.TransportType = string(transport.Rest)
	}

	var updated bool
	var err error
	switch c.transportConfig.TransportType {
	case string(transport.Kafka):
		updated, err = c.ReconcileKafkaCredential(ctx, secret)
		if err != nil {
			return ctrl.Result{}, err
		}
		if updated {
			if err := c.ReconcileConsumer(ctx); err != nil {
				return ctrl.Result{}, err
			}
			if err := c.ReconcileProducer(); err != nil {
				return ctrl.Result{}, err
			}
		}
	case string(transport.Rest):
		updated, err = c.ReconcileRestfulCredential(ctx, secret)
		if err != nil {
			return ctrl.Result{}, err
		}
		if updated {
			if err := c.ReconcileRequester(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unsupported transport type: %s", c.transportConfig.TransportType)
	}

	if !updated {
		return ctrl.Result{}, nil
	}

	if c.transportCallback != nil {
		if err := c.transportCallback(c.transportClient); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to invoke the callback function: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// ReconcileProducer, transport config is changed, then create/update the producer
func (c *TransportCtrl) ReconcileProducer() error {
	if c.transportClient.producer == nil {
		sender, err := producer.NewGenericProducer(c.transportConfig)
		if err != nil {
			return fmt.Errorf("failed to create/update the producer: %w", err)
		}
		c.transportClient.producer = sender
	} else {
		if err := c.transportClient.producer.Reconnect(c.transportConfig); err != nil {
			return fmt.Errorf("failed to reconnect the producer: %w", err)
		}
	}
	return nil
}

// ReconcileConsumer, transport config is changed, then create/update the consumer
func (c *TransportCtrl) ReconcileConsumer(ctx context.Context) error {
	// if the consumer groupId is empty, then it's means the agent is in the standalone mode, don't create the consumer
	if c.transportConfig.ConsumerGroupId == "" {
		return nil
	}

	// create/update the consumer with the kafka transport
	if c.transportClient.consumer == nil {
		receiver, err := consumer.NewGenericConsumer(c.transportConfig)
		if err != nil {
			return fmt.Errorf("failed to create the consumer: %w", err)
		}
		go func() {
			if err = receiver.Start(ctx); err != nil {
				klog.Errorf("failed to start the consumser: %v", err)
			}
		}()
		c.transportClient.consumer = receiver
	} else {
		if err := c.transportClient.consumer.Reconnect(ctx, c.transportConfig); err != nil {
			return fmt.Errorf("failed to reconnect the consumer: %w", err)
		}
	}
	klog.Info("the transport(kafka) consumer is created/updated")
	return nil
}

// ReconcileInventory, transport config is changed, then create/update the inventory client
func (c *TransportCtrl) ReconcileRequester(ctx context.Context) error {
	if c.transportClient.requester == nil {

		if c.transportConfig.RestfulCredential == nil {
			return fmt.Errorf("the restful credential must not be nil")
		}
		inventoryClient, err := requester.NewInventoryClient(ctx, c.transportConfig.RestfulCredential)
		if err != nil {
			return fmt.Errorf("initial the inventory client error %w", err)
		}
		c.transportClient.requester = inventoryClient
	} else {
		c.transportClient.requester.RefreshClient(ctx, c.transportConfig.RestfulCredential)
	}
	return nil
}

// ReconcileKafkaCredential update the kafka connection credentail based on the secret, return true if the kafka
// credentail is updated, It also create/update the consumer if not in the standalone mode
func (c *TransportCtrl) ReconcileKafkaCredential(ctx context.Context, secret *corev1.Secret) (bool, error) {
	// load the kafka connection credentail based on the transport type. kafka, multiple
	kafkaConn, err := config.GetKafkaCredentailBySecret(secret, c.runtimeClient)
	if err != nil {
		return false, err
	}

	// update the wathing secret lits
	if kafkaConn.CASecretName != "" || !utils.ContainsString(c.extraSecretNames, kafkaConn.CASecretName) {
		c.extraSecretNames = append(c.extraSecretNames, kafkaConn.CASecretName)
	}
	if kafkaConn.ClientSecretName != "" || utils.ContainsString(c.extraSecretNames, kafkaConn.ClientSecretName) {
		c.extraSecretNames = append(c.extraSecretNames, kafkaConn.ClientSecretName)
	}

	// if credentials aren't updated, then return
	if reflect.DeepEqual(c.transportConfig.KafkaCredential, kafkaConn) {
		return false, nil
	}
	c.transportConfig.KafkaCredential = kafkaConn
	return true, nil
}

func (c *TransportCtrl) ReconcileRestfulCredential(ctx context.Context, secret *corev1.Secret) (
	updated bool, err error,
) {
	restfulConn, err := config.GetRestfulConnBySecret(secret, c.runtimeClient)
	if err != nil {
		return updated, err
	}

	// update the wathing secret lits
	if restfulConn.CASecretName != "" || !utils.ContainsString(c.extraSecretNames, restfulConn.CASecretName) {
		c.extraSecretNames = append(c.extraSecretNames, restfulConn.CASecretName)
	}
	if restfulConn.ClientSecretName != "" || utils.ContainsString(c.extraSecretNames, restfulConn.ClientSecretName) {
		c.extraSecretNames = append(c.extraSecretNames, restfulConn.ClientSecretName)
	}

	if reflect.DeepEqual(c.transportConfig.RestfulCredential, restfulConn) {
		return
	}
	updated = true
	c.transportConfig.RestfulCredential = restfulConn
	return
}

// SetupWithManager sets up the controller with the Manager.
func (c *TransportCtrl) SetupWithManager(mgr ctrl.Manager) error {
	c.runtimeClient = mgr.GetClient()
	secretPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return c.credentialSecret(e.Object.GetName())
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !c.credentialSecret(e.ObjectNew.GetName()) {
				return false
			}
			newSecret := e.ObjectNew.(*corev1.Secret)
			oldSecret := e.ObjectOld.(*corev1.Secret)
			// only enqueue the obj when secret data changed
			return !reflect.DeepEqual(newSecret.Data, oldSecret.Data)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(secretPred)).
		Complete(c)
}

func (c *TransportCtrl) credentialSecret(name string) bool {
	if c.secretName == name {
		return true
	}
	if c.extraSecretNames == nil || len(c.extraSecretNames) == 0 {
		return false
	}
	for _, secretName := range c.extraSecretNames {
		if name == secretName {
			return true
		}
	}
	return false
}
