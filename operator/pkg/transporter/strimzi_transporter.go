package transporter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	"github.com/go-logr/logr"
	subv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

const (
	KafkaClusterName = "kafka"

	// kafka storage
	DefaultKafkaDefaultStorageSize = "10Gi"

	// subscription - common
	DefaultKafkaSubName           = "strimzi-kafka-operator"
	DefaultInstallPlanApproval    = subv1alpha1.ApprovalAutomatic
	DefaultCatalogSourceNamespace = "openshift-marketplace"

	// subscription - production
	DefaultAMQChannel        = "amq-streams-2.5.x"
	DefaultAMQPackageName    = "amq-streams"
	DefaultCatalogSourceName = "redhat-operators"

	// subscription - community
	CommunityChannel           = "strimzi-0.36.x"
	CommunityPackageName       = "strimzi-kafka-operator"
	CommunityCatalogSourceName = "community-operators"

	// users
	DefaultGlobalHubKafkaUser = "global-hub-kafka-user"

	// topic names
	StatusTopicTemplate    = "status.%s"
	GlobalRegexStatusTopic = "^status.*"
	GlobalHubClusterName   = "global"
)

var (
	KafkaStorageIdentifier   int32 = 0
	KafkaStorageDeleteClaim        = false
	KafkaVersion                   = "3.5.0"
	DefaultPartition         int32 = 1
	DefaultPartitionReplicas int32 = 2
)

// install the strimzi kafka cluster by operator
type strimziTransporter struct {
	log       logr.Logger
	ctx       context.Context
	name      string
	namespace string

	// subscription properties
	subName              string
	subCommunity         bool
	subChannel           string
	subCatalogSourceName string
	subPackageName       string

	// global hub config
	mgh           *operatorv1alpha4.MulticlusterGlobalHub
	runtimeClient client.Client

	// wait until kafka cluster status is ready when initialize
	waitReady              bool
	enableTLS              bool
	multiTopic             bool
	topicPartitionReplicas int32
}

type KafkaOption func(*strimziTransporter)

func NewStrimziTransporter(c client.Client, mgh *operatorv1alpha4.MulticlusterGlobalHub,
	opts ...KafkaOption,
) (*strimziTransporter, error) {
	k := &strimziTransporter{
		log:       ctrl.Log.WithName("strimzi-transporter"),
		ctx:       context.TODO(),
		name:      KafkaClusterName,
		namespace: constants.GHDefaultNamespace,

		subName:              DefaultKafkaSubName,
		subCommunity:         false,
		subChannel:           DefaultAMQChannel,
		subPackageName:       DefaultAMQPackageName,
		subCatalogSourceName: DefaultCatalogSourceName,

		waitReady:              true,
		enableTLS:              true,
		multiTopic:             true,
		topicPartitionReplicas: DefaultPartitionReplicas,

		runtimeClient: c,
		mgh:           mgh,
	}
	// apply options
	for _, opt := range opts {
		opt(k)
	}

	if k.subCommunity {
		k.subChannel = CommunityChannel
		k.subPackageName = CommunityPackageName
		k.subCatalogSourceName = CommunityCatalogSourceName
	}

	if mgh.Spec.AvailabilityConfig == operatorv1alpha4.HABasic {
		k.topicPartitionReplicas = 1
	}

	err := k.initialize(k.mgh)
	return k, err
}

func WithNamespacedName(name types.NamespacedName) KafkaOption {
	return func(sk *strimziTransporter) {
		sk.name = name.Name
		sk.namespace = name.Namespace
	}
}

func WithContext(ctx context.Context) KafkaOption {
	return func(sk *strimziTransporter) {
		sk.ctx = ctx
	}
}

func WithCommunity(val bool) KafkaOption {
	return func(sk *strimziTransporter) {
		sk.subCommunity = val
	}
}

func WithSubName(name string) KafkaOption {
	return func(sk *strimziTransporter) {
		sk.subName = name
	}
}

func WithWaitReady(wait bool) KafkaOption {
	return func(sk *strimziTransporter) {
		sk.waitReady = wait
	}
}

// initialize the kafka cluster, return nil if the instance is launched successfully!
func (k *strimziTransporter) initialize(mgh *operatorv1alpha4.MulticlusterGlobalHub) error {
	k.log.Info("reconcile global hub kafka subscription")
	k.namespace = mgh.Namespace
	err := k.ensureSubscription(mgh)
	if err != nil {
		return err
	}

	k.log.Info("reconcile global hub kafka instance")
	err = wait.PollUntilContextTimeout(k.ctx, 2*time.Second, 30*time.Second, true,
		func(ctx context.Context) (bool, error) {
			err = k.createKafkaCluster(mgh)
			if err != nil {
				k.log.Info("the kafka instance is not created, retrying...", "message", err.Error())
				return false, nil
			}
			return true, nil
		})
	if err != nil {
		return err
	}

	if !k.waitReady {
		return nil
	}

	k.log.Info("waiting the kafka cluster instance to be ready...")
	return k.kafkaClusterReady()
}

func (k *strimziTransporter) GenerateUserName(clusterIdentity string) string {
	return fmt.Sprintf("%s-kafka-user", clusterIdentity)
}

func (k *strimziTransporter) CreateUser(username string) error {
	kafkaUser := &kafkav1beta2.KafkaUser{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      username,
		Namespace: k.namespace,
	}, kafkaUser)
	if err != nil && errors.IsNotFound(err) {
		return k.runtimeClient.Create(k.ctx, k.newKafkaUser(username))
	}
	return err
}

func (k *strimziTransporter) DeleteUser(topicName string) error {
	kafkaUser := &kafkav1beta2.KafkaUser{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      topicName,
		Namespace: k.namespace,
	}, kafkaUser)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return k.runtimeClient.Delete(k.ctx, kafkaUser)
}

func (k *strimziTransporter) GenerateClusterTopic(clusterIdentity string) *transport.ClusterTopic {
	topic := &transport.ClusterTopic{
		SpecTopic:   "spec",
		StatusTopic: "status",
		EventTopic:  "event",
	}
	if k.multiTopic {
		topic.StatusTopic = fmt.Sprintf(StatusTopicTemplate, clusterIdentity)
		// the status topic for global hub manager should be "^status.*"
		if clusterIdentity == GlobalHubClusterName {
			topic.StatusTopic = GlobalRegexStatusTopic
		}
	}

	return topic
}

func (k *strimziTransporter) CreateTopic(topic *transport.ClusterTopic) error {
	for _, topicName := range []string{topic.SpecTopic, topic.StatusTopic, topic.EventTopic} {
		// if the topicName = "^status.*", convert it to status.global for creating
		if topicName == GlobalRegexStatusTopic {
			topicName = fmt.Sprintf(StatusTopicTemplate, GlobalHubClusterName)
		}
		kafkaTopic := &kafkav1beta2.KafkaTopic{}
		err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
			Name:      topicName,
			Namespace: k.namespace,
		}, kafkaTopic)
		if err != nil && errors.IsNotFound(err) {
			if e := k.runtimeClient.Create(k.ctx, k.newKafkaTopic(topicName)); e != nil {
				return e
			}
		}
	}
	return nil
}

func (k *strimziTransporter) DeleteTopic(topic *transport.ClusterTopic) error {
	for _, topicName := range []string{topic.SpecTopic, topic.StatusTopic, topic.EventTopic} {
		kafkaTopic := &kafkav1beta2.KafkaTopic{}
		err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
			Name:      topicName,
			Namespace: k.namespace,
		}, kafkaTopic)
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		err = k.runtimeClient.Delete(k.ctx, kafkaTopic)
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *strimziTransporter) GetConnCredential(username string) (*transport.ConnCredential, error) {
	kafkaCluster := &kafkav1beta2.Kafka{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      k.name,
		Namespace: k.namespace,
	}, kafkaCluster)
	if err != nil {
		return nil, err
	}

	if kafkaCluster.Status == nil || kafkaCluster.Status.Conditions == nil {
		return nil, fmt.Errorf("kafka cluster %s has no status conditions", kafkaCluster.Name)
	}

	kafkaUserSecret := &corev1.Secret{}
	err = k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      username,
		Namespace: k.namespace,
	}, kafkaUserSecret)
	if err != nil {
		return nil, err
	}

	for _, condition := range kafkaCluster.Status.Conditions {
		if *condition.Type == "Ready" && *condition.Status == "True" {
			credential := &transport.ConnCredential{
				BootstrapServer: *kafkaCluster.Status.Listeners[1].BootstrapServers,
				CACert:          base64.StdEncoding.EncodeToString([]byte(kafkaCluster.Status.Listeners[1].Certificates[0])),
			}
			if k.enableTLS {
				credential.ClientCert = base64.StdEncoding.EncodeToString(kafkaUserSecret.Data["user.crt"])
				credential.ClientKey = base64.StdEncoding.EncodeToString(kafkaUserSecret.Data["user.key"])
			} else {
				k.log.Info("the kafka cluster hasn't enable tls for user", "username", username)
			}
			return credential, nil
		}
	}

	return nil, fmt.Errorf("kafka user %s/%s is not ready", k.namespace, username)
}

func (k *strimziTransporter) newKafkaTopic(topicName string) *kafkav1beta2.KafkaTopic {
	return &kafkav1beta2.KafkaTopic{
		ObjectMeta: metav1.ObjectMeta{
			Name:      topicName,
			Namespace: k.namespace,
			Labels: map[string]string{
				// It is important to set the cluster label otherwise the topic will not be ready
				"strimzi.io/cluster":             k.name,
				constants.GlobalHubOwnerLabelKey: constants.GlobalHubAddonOwnerLabelVal,
			},
		},
		Spec: &kafkav1beta2.KafkaTopicSpec{
			Partitions: &DefaultPartition,
			Replicas:   &k.topicPartitionReplicas,
			Config: &apiextensions.JSON{Raw: []byte(`{
				"cleanup.policy": "compact"
			}`)},
		},
	}
}

func (k *strimziTransporter) newKafkaUser(username string) *kafkav1beta2.KafkaUser {
	return &kafkav1beta2.KafkaUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      username,
			Namespace: k.namespace,
			Labels: map[string]string{
				// It is important to set the cluster label otherwise the user will not be ready
				"strimzi.io/cluster":             k.name,
				constants.GlobalHubOwnerLabelKey: constants.GlobalHubAddonOwnerLabelVal,
			},
		},
		Spec: &kafkav1beta2.KafkaUserSpec{
			Authentication: &kafkav1beta2.KafkaUserSpecAuthentication{
				Type: kafkav1beta2.KafkaUserSpecAuthenticationTypeTls,
			},
		},
	}
}

// waits for kafka cluster to be ready and returns nil if kafka cluster ready
func (k *strimziTransporter) kafkaClusterReady() error {
	err := wait.PollUntilContextTimeout(k.ctx, 5*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			kafkaCluster := &kafkav1beta2.Kafka{}
			err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
				Name:      k.name,
				Namespace: k.namespace,
			}, kafkaCluster)
			if err != nil {
				k.log.Info("fail to get the kafka cluster, waiting", "message", err.Error())
				return false, nil
			}
			if kafkaCluster.Status == nil || kafkaCluster.Status.Conditions == nil {
				k.log.Info("kafka cluster status is not ready")
				return false, nil
			}

			if kafkaCluster.Spec != nil && kafkaCluster.Spec.Kafka.Listeners != nil {
				// if the kafka cluster is already created, check if the tls is enabled
				enableTLS := false
				for _, listener := range kafkaCluster.Spec.Kafka.Listeners {
					if listener.Tls {
						enableTLS = true
						break
					}
				}
				k.enableTLS = enableTLS
			}

			for _, condition := range kafkaCluster.Status.Conditions {
				if *condition.Type == "Ready" && *condition.Status == "True" {
					return true, nil
				}
			}

			k.log.Info("kafka cluster status condition is not ready")
			return false, nil
		})

	return err
}

func (k *strimziTransporter) createKafkaCluster(mgh *operatorv1alpha4.MulticlusterGlobalHub) error {
	existingKafka := &kafkav1beta2.Kafka{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      k.name,
		Namespace: mgh.Namespace,
	}, existingKafka)
	if err != nil && !errors.IsNotFound(err) {
		return err
	} else if errors.IsNotFound(err) {
		if e := k.runtimeClient.Create(k.ctx, k.newKafkaCluster(mgh)); e != nil {
			return e
		}
	}
	return nil
}

func (k *strimziTransporter) getKafkaResources(
	mgh *operatorv1alpha4.MulticlusterGlobalHub) *kafkav1beta2.KafkaSpecKafkaResources {
	kafkaRes := utils.GetResources(operatorconstants.Kafka, mgh.Spec.AdvancedConfig)
	kafkaSpecRes := &kafkav1beta2.KafkaSpecKafkaResources{}
	jsonData, err := json.Marshal(kafkaRes)
	if err != nil {
		k.log.Error(err, "failed to marshal kafka resources")
	}
	err = json.Unmarshal(jsonData, kafkaRes)
	if err != nil {
		k.log.Error(err, "failed to unmarshal to KafkaSpecKafkaResources")
	}

	return kafkaSpecRes
}

func (k *strimziTransporter) getZookeeperResources(
	mgh *operatorv1alpha4.MulticlusterGlobalHub) *kafkav1beta2.KafkaSpecZookeeperResources {
	zookeeperRes := utils.GetResources(operatorconstants.Zookeeper, mgh.Spec.AdvancedConfig)

	zookeeperSpecRes := &kafkav1beta2.KafkaSpecZookeeperResources{}
	jsonData, err := json.Marshal(zookeeperRes)
	if err != nil {
		k.log.Error(err, "failed to marshal zookeeper resources")
	}
	err = json.Unmarshal(jsonData, zookeeperSpecRes)
	if err != nil {
		k.log.Error(err, "failed to unmarshal to KafkaSpecZookeeperResources")
	}
	return zookeeperSpecRes
}

func (k *strimziTransporter) newKafkaCluster(mgh *operatorv1alpha4.MulticlusterGlobalHub) *kafkav1beta2.Kafka {
	storageSize := config.GetKafkaStorageSize(mgh)

	kafkaSpecKafkaStorageVolumesElem := kafkav1beta2.KafkaSpecKafkaStorageVolumesElem{
		Id:          &KafkaStorageIdentifier,
		Size:        &storageSize,
		Type:        kafkav1beta2.KafkaSpecKafkaStorageVolumesElemTypePersistentClaim,
		DeleteClaim: &KafkaStorageDeleteClaim,
	}
	kafkaSpecZookeeperStorage := kafkav1beta2.KafkaSpecZookeeperStorage{
		Type:        kafkav1beta2.KafkaSpecZookeeperStorageTypePersistentClaim,
		Size:        &storageSize,
		DeleteClaim: &KafkaStorageDeleteClaim,
	}

	if mgh.Spec.DataLayer.StorageClass != "" {
		kafkaSpecKafkaStorageVolumesElem.Class = &mgh.Spec.DataLayer.StorageClass
		kafkaSpecZookeeperStorage.Class = &mgh.Spec.DataLayer.StorageClass
	}

	return &kafkav1beta2.Kafka{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.name,
			Namespace: k.namespace,
		},
		Spec: &kafkav1beta2.KafkaSpec{
			Kafka: kafkav1beta2.KafkaSpecKafka{
				Config: &apiextensions.JSON{Raw: []byte(`{
"default.replication.factor": 3,
"inter.broker.protocol.version": "3.5",
"min.insync.replicas": 2,
"offsets.topic.replication.factor": 3,
"transaction.state.log.min.isr": 2,
"transaction.state.log.replication.factor": 3
}`)},
				Listeners: []kafkav1beta2.KafkaSpecKafkaListenersElem{
					{
						Name: "plain",
						Port: 9092,
						Tls:  false,
						Type: kafkav1beta2.KafkaSpecKafkaListenersElemTypeInternal,
					},
					{
						Name: "tls",
						Port: 9093,
						Tls:  true,
						Type: kafkav1beta2.KafkaSpecKafkaListenersElemTypeRoute,
						Authentication: &kafkav1beta2.KafkaSpecKafkaListenersElemAuthentication{
							Type: kafkav1beta2.KafkaSpecKafkaListenersElemAuthenticationTypeTls,
						},
					},
				},
				Resources: k.getKafkaResources(mgh),
				Replicas:  3,
				Storage: kafkav1beta2.KafkaSpecKafkaStorage{
					Type: kafkav1beta2.KafkaSpecKafkaStorageTypeJbod,
					Volumes: []kafkav1beta2.KafkaSpecKafkaStorageVolumesElem{
						kafkaSpecKafkaStorageVolumesElem,
					},
				},
				Version: &KafkaVersion,
			},
			Zookeeper: kafkav1beta2.KafkaSpecZookeeper{
				Replicas:  3,
				Storage:   kafkaSpecZookeeperStorage,
				Resources: k.getZookeeperResources(mgh),
			},
			EntityOperator: &kafkav1beta2.KafkaSpecEntityOperator{
				TopicOperator: &kafkav1beta2.KafkaSpecEntityOperatorTopicOperator{},
				UserOperator:  &kafkav1beta2.KafkaSpecEntityOperatorUserOperator{},
			},
		},
	}
}

// create/ update the kafka subscription
func (k *strimziTransporter) ensureSubscription(mgh *operatorv1alpha4.MulticlusterGlobalHub) error {
	// get subscription
	existingSub := &subv1alpha1.Subscription{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      k.subName,
		Namespace: mgh.GetNamespace(),
	}, existingSub)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	expectedSub := k.newSubscription(mgh)
	if errors.IsNotFound(err) {
		return k.runtimeClient.Create(k.ctx, expectedSub)
	} else {
		startingCSV := expectedSub.Spec.StartingCSV
		// if updating channel must remove startingCSV
		if existingSub.Spec.Channel != expectedSub.Spec.Channel {
			startingCSV = ""
		}
		if !equality.Semantic.DeepEqual(existingSub.Spec, expectedSub.Spec) {
			existingSub.Spec = expectedSub.Spec
		}
		existingSub.Spec.StartingCSV = startingCSV
		return k.runtimeClient.Update(k.ctx, existingSub)
	}
}

// newSubscription returns an CrunchyPostgres subscription with desired default values
func (k *strimziTransporter) newSubscription(mgh *operatorv1alpha4.MulticlusterGlobalHub) *subv1alpha1.Subscription {
	labels := map[string]string{
		"installer.name":                 mgh.Name,
		"installer.namespace":            mgh.Namespace,
		constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
	}
	// Generate sub config from mgh CR
	subConfig := &subv1alpha1.SubscriptionConfig{
		NodeSelector: mgh.Spec.NodeSelector,
		Tolerations:  mgh.Spec.Tolerations,
	}

	sub := &subv1alpha1.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: subv1alpha1.SubscriptionCRDAPIVersion,
			Kind:       subv1alpha1.SubscriptionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.subName,
			Namespace: mgh.Namespace,
			Labels:    labels,
		},
		Spec: &subv1alpha1.SubscriptionSpec{
			Channel:                k.subChannel,
			InstallPlanApproval:    DefaultInstallPlanApproval,
			Package:                k.subPackageName,
			CatalogSource:          k.subCatalogSourceName,
			CatalogSourceNamespace: DefaultCatalogSourceNamespace,
			Config:                 subConfig,
		},
	}
	return sub
}
