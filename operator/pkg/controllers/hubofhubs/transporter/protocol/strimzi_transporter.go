package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/go-logr/logr"
	subv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
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
	DefaultAMQChannel        = "amq-streams-2.6.x"
	DefaultAMQPackageName    = "amq-streams"
	DefaultCatalogSourceName = "redhat-operators"

	// subscription - community
	CommunityChannel           = "strimzi-0.38.x"
	CommunityPackageName       = "strimzi-kafka-operator"
	CommunityCatalogSourceName = "community-operators"

	// users
	DefaultGlobalHubKafkaUser = "global-hub-kafka-user"

	// topic names
	StatusTopicTemplate  = "status.%s"
	StatusTopicRegex     = "^status.*"
	StatusTopicPrefix    = "status"
	GlobalHubClusterName = "global"
)

var (
	KafkaStorageIdentifier   int32 = 0
	KafkaStorageDeleteClaim        = false
	KafkaVersion                   = "3.6.0"
	DefaultPartition         int32 = 1
	DefaultPartitionReplicas int32 = 3
	// kafka metrics constants
	KakfaMetricsConfigmapName       = "kafka-metrics"
	KafkaMetricsConfigmapKeyRef     = "kafka-metrics-config.yml"
	ZooKeeperMetricsConfigmapKeyRef = "zookeeper-metrics-config.yml"
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
	k.log.Info("reconcile global hub kafka transport...")
	k.namespace = mgh.Namespace
	err := k.ensureSubscription(mgh)
	if err != nil {
		return err
	}
	err = wait.PollUntilContextTimeout(k.ctx, 2*time.Second, 30*time.Second, true,
		func(ctx context.Context) (bool, error) {
			if !config.GetKafkaResourceReady() {
				return false, fmt.Errorf("the kafka crds is not ready")
			}
			err, _ = k.CreateUpdateKafkaCluster(mgh)
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

	return k.kafkaClusterReady()
}

func (k *strimziTransporter) GenerateUserName(clusterIdentity string) string {
	return fmt.Sprintf("%s-kafka-user", clusterIdentity)
}

func (k *strimziTransporter) CreateAndUpdateUser(username string) error {
	kafkaUser := &kafkav1beta2.KafkaUser{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      username,
		Namespace: k.namespace,
	}, kafkaUser)
	if err != nil {
		if errors.IsNotFound(err) {
			return k.runtimeClient.Create(k.ctx, k.newKafkaUser(username))
		}
		return err
	}
	desiredKafkaUser := k.newKafkaUser(username)

	updatedKafkaUser := &kafkav1beta2.KafkaUser{}
	err = mergeObjects(kafkaUser, desiredKafkaUser, updatedKafkaUser)
	if err != nil {
		return err
	}

	if !equality.Semantic.DeepDerivative(updatedKafkaUser.Spec, kafkaUser.Spec) {
		return k.runtimeClient.Update(k.ctx, updatedKafkaUser)
	}

	return nil
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
		SpecTopic:   transport.GenericSpecTopic,
		StatusTopic: transport.GenericStatusTopic,
		EventTopic:  transport.GenericEventTopic,
	}
	if k.multiTopic {
		topic.StatusTopic = fmt.Sprintf(StatusTopicTemplate, clusterIdentity)
		// the status topic for global hub manager should be "^status.*"
		if clusterIdentity == GlobalHubClusterName {
			topic.StatusTopic = StatusTopicRegex
		}
	}

	return topic
}

func (k *strimziTransporter) CreateAndUpdateTopic(topic *transport.ClusterTopic) error {
	for _, topicName := range []string{topic.SpecTopic, topic.StatusTopic, topic.EventTopic} {
		// if the topicName = "^status.*", convert it to status.global for creating
		if topicName == StatusTopicRegex {
			topicName = fmt.Sprintf(StatusTopicTemplate, GlobalHubClusterName)
		}
		kafkaTopic := &kafkav1beta2.KafkaTopic{}
		err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
			Name:      topicName,
			Namespace: k.namespace,
		}, kafkaTopic)
		if err != nil {
			if errors.IsNotFound(err) {
				err = k.runtimeClient.Create(k.ctx, k.newKafkaTopic(topicName))
				if err != nil {
					return err
				}
				continue
			}
			return err
		}

		desiredTopic := k.newKafkaTopic(topicName)

		updatedKafkaTopic := &kafkav1beta2.KafkaTopic{}
		err = mergeObjects(kafkaTopic, desiredTopic, updatedKafkaTopic)
		if err != nil {
			return err
		}
		// Kafka do not support change exitsting kafaka topic replica directly.
		updatedKafkaTopic.Spec.Replicas = kafkaTopic.Spec.Replicas

		if !equality.Semantic.DeepDerivative(updatedKafkaTopic.Spec, kafkaTopic.Spec) {
			err = k.runtimeClient.Update(k.ctx, updatedKafkaTopic)
			if err != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func (k *strimziTransporter) DeleteTopic(topic *transport.ClusterTopic) error {
	// This topic is shared between different managed hub clusters
	if StatusTopicPrefix == topic.StatusTopic {
		return nil
	}

	// Only delete the specific cluster topic
	kafkaTopic := &kafkav1beta2.KafkaTopic{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      topic.StatusTopic,
		Namespace: k.namespace,
	}, kafkaTopic)
	if err != nil && errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	return k.runtimeClient.Delete(k.ctx, kafkaTopic)
}

// authorize
func (k *strimziTransporter) GrantRead(userName string, topicName string) error {
	kafkaUser := &kafkav1beta2.KafkaUser{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      userName,
		Namespace: k.namespace,
	}, kafkaUser)
	if err != nil {
		return err
	}

	expectedAcls := []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{
		topicReadAcl(topicName),
		consumeGroupReadAcl(),
	}

	return k.addPermissions(kafkaUser, expectedAcls)
}

func topicReadAcl(topicName string) kafkav1beta2.KafkaUserSpecAuthorizationAclsElem {
	host := "*"
	patternType := kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourcePatternTypeLiteral
	if topicName == StatusTopicRegex {
		// give the topic permission for the manager user
		patternType = kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourcePatternTypePrefix
		topicName = StatusTopicPrefix
	}
	return kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{
		Host: &host,
		Resource: kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResource{
			Type:        kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourceTypeTopic,
			Name:        &topicName,
			PatternType: &patternType,
		},
		Operations: []kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElem{
			kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElemDescribe,
			kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElemRead,
		},
	}
}

func consumeGroupReadAcl() kafkav1beta2.KafkaUserSpecAuthorizationAclsElem {
	// expected consumer group ACL
	host := "*"
	consumerGroup := "*"
	consumerPatternType := kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourcePatternTypeLiteral
	consumerAcl := kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{
		Host: &host,
		Resource: kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResource{
			Type:        kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourceTypeGroup,
			Name:        &consumerGroup,
			PatternType: &consumerPatternType,
		},
		Operations: []kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElem{
			kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElemRead,
		},
	}
	return consumerAcl
}

func (k *strimziTransporter) GrantWrite(userName string, topicName string) error {
	kafkaUser := &kafkav1beta2.KafkaUser{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      userName,
		Namespace: k.namespace,
	}, kafkaUser)
	if err != nil {
		return err
	}

	expectedAcls := []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{
		topicWriteAcl(topicName),
	}

	return k.addPermissions(kafkaUser, expectedAcls)
}

func (k *strimziTransporter) addPermissions(kafkaUser *kafkav1beta2.KafkaUser,
	expectedAcls []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem,
) error {
	authorization := kafkaUser.Spec.Authorization
	if authorization == nil {
		authorization = &kafkav1beta2.KafkaUserSpecAuthorization{
			Type: kafkav1beta2.KafkaUserSpecAuthorizationTypeSimple,
			Acls: []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{},
		}
	}

	// the acl need to be added to the authorization
	pendingAcls := []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{}
	for _, expectedAcl := range expectedAcls {
		exist := false
		for _, acl := range authorization.Acls {
			if equality.Semantic.DeepEqual(acl, expectedAcl) {
				exist = true
			}
		}
		if !exist {
			pendingAcls = append(pendingAcls, expectedAcl)
			k.log.Info("add permission", "user", kafkaUser.Name, "type", expectedAcl.Resource.Type,
				"name", expectedAcl.Resource.Name, "permission", expectedAcl.Operations)
		}
	}

	if len(pendingAcls) > 0 {
		authorization.Acls = append(authorization.Acls, pendingAcls...)
		kafkaUser.Spec.Authorization = authorization
		return k.runtimeClient.Update(k.ctx, kafkaUser)
	}
	return nil
}

func topicWriteAcl(topicName string) kafkav1beta2.KafkaUserSpecAuthorizationAclsElem {
	host := "*"
	patternType := kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourcePatternTypeLiteral
	writeAcl := kafkav1beta2.KafkaUserSpecAuthorizationAclsElem{
		Host: &host,
		Resource: kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResource{
			Type:        kafkav1beta2.KafkaUserSpecAuthorizationAclsElemResourceTypeTopic,
			Name:        &topicName,
			PatternType: &patternType,
		},
		Operations: []kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElem{
			kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElemWrite,
		},
	}
	return writeAcl
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
			clusterIdentity := string(kafkaCluster.GetUID())
			if kafkaCluster.Status.ClusterId != nil {
				clusterIdentity = *kafkaCluster.Status.ClusterId
			}
			credential := &transport.ConnCredential{
				Identity:        clusterIdentity,
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

// func containAcl(acls []kafkav1beta2.KafkaUserSpecAuthorizationAclsElem,
// 	targetAcl kafkav1beta2.KafkaUserSpecAuthorizationAclsElem,
// ) bool {
// 	for _, acl := range acls {
// 		// resource
// 		if *targetAcl.Host == *acl.Host &&
// 			targetAcl.Resource.Type == acl.Resource.Type &&
// 			*targetAcl.Resource.Name == *acl.Resource.Name &&
// 			*targetAcl.Resource.PatternType == *acl.Resource.PatternType &&
// 			containOps(acl.Operations, targetAcl.Operations) { // operation
// 			return true
// 		}
// 	}
// 	return false
// }

// func containOps(operations []kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElem,
// 	subOpertions []kafkav1beta2.KafkaUserSpecAuthorizationAclsElemOperationsElem,
// ) bool {
// 	matchedOp := 0
// 	for _, targetOp := range subOpertions {
// 		for _, op := range operations {
// 			if string(op) == string(targetOp) {
// 				matchedOp += 1
// 				break
// 			}
// 		}
// 	}
// 	return len(subOpertions) == matchedOp
// }

func (k *strimziTransporter) newKafkaTopic(topicName string) *kafkav1beta2.KafkaTopic {
	return &kafkav1beta2.KafkaTopic{
		ObjectMeta: metav1.ObjectMeta{
			Name:      topicName,
			Namespace: k.namespace,
			Labels: map[string]string{
				// It is important to set the cluster label otherwise the topic will not be ready
				"strimzi.io/cluster":             k.name,
				constants.GlobalHubOwnerLabelKey: constants.GlobalHubOwnerLabelVal,
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
				constants.GlobalHubOwnerLabelKey: constants.GlobalHubOwnerLabelVal,
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
	k.log.Info("waiting the kafka cluster instance to be ready...")

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
	k.log.Info("kafka cluster is ready")
	return err
}

func (k *strimziTransporter) CreateUpdateKafkaCluster(mgh *operatorv1alpha4.MulticlusterGlobalHub) (error, bool) {
	existingKafka := &kafkav1beta2.Kafka{}
	err := k.runtimeClient.Get(k.ctx, types.NamespacedName{
		Name:      k.name,
		Namespace: mgh.Namespace,
	}, existingKafka)
	if err != nil {
		if errors.IsNotFound(err) {
			return k.runtimeClient.Create(k.ctx, k.newKafkaCluster(mgh)), true
		}
		return err, false
	}

	// this is only for e2e test. patch the kafka needs more time to be ready
	if _, ok := existingKafka.Annotations["skip-patch-if-exist"]; ok {
		return nil, false
	}

	desiredKafka := k.newKafkaCluster(mgh)

	updatedKafka := &kafkav1beta2.Kafka{}
	err = mergeObjects(existingKafka, desiredKafka, updatedKafka)
	if err != nil {
		return err, false
	}

	updatedKafka.Spec.Kafka.MetricsConfig = desiredKafka.Spec.Kafka.MetricsConfig
	updatedKafka.Spec.Zookeeper.MetricsConfig = desiredKafka.Spec.Zookeeper.MetricsConfig

	if !reflect.DeepEqual(updatedKafka.Spec, existingKafka.Spec) {
		return k.runtimeClient.Update(k.ctx, updatedKafka), true
	}
	return nil, false
}

// mergeObjects merge the desiredObj into the existingObj, then unmarshal to updatedObj
func mergeObjects(existingObj, desiredObj, updatedObj client.Object) error {
	existingJson, _ := json.Marshal(existingObj)
	desiredJson, _ := json.Marshal(desiredObj)

	// patch the desired json to the existing json
	patchedData, err := jsonpatch.MergePatch(existingJson, desiredJson)
	if err != nil {
		return err
	}
	err = json.Unmarshal(patchedData, updatedObj)
	if err != nil {
		return err
	}
	return nil
}

func (k *strimziTransporter) getKafkaResources(
	mgh *operatorv1alpha4.MulticlusterGlobalHub,
) *kafkav1beta2.KafkaSpecKafkaResources {
	kafkaRes := utils.GetResources(operatorconstants.Kafka, mgh.Spec.AdvancedConfig)
	kafkaSpecRes := &kafkav1beta2.KafkaSpecKafkaResources{}
	jsonData, err := json.Marshal(kafkaRes)
	if err != nil {
		k.log.Error(err, "failed to marshal kafka resources")
	}
	err = json.Unmarshal(jsonData, kafkaSpecRes)
	if err != nil {
		k.log.Error(err, "failed to unmarshal to KafkaSpecKafkaResources")
	}

	return kafkaSpecRes
}

func (k *strimziTransporter) getZookeeperResources(
	mgh *operatorv1alpha4.MulticlusterGlobalHub,
) *kafkav1beta2.KafkaSpecZookeeperResources {
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

	kafkaCluster := &kafkav1beta2.Kafka{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.name,
			Namespace: k.namespace,
			Labels: map[string]string{
				constants.GlobalHubOwnerLabelKey: constants.GlobalHubOwnerLabelVal,
			},
		},
		Spec: &kafkav1beta2.KafkaSpec{
			Kafka: kafkav1beta2.KafkaSpecKafka{
				Config: &apiextensions.JSON{Raw: []byte(`{
"default.replication.factor": 3,
"inter.broker.protocol.version": "3.6",
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
				Authorization: &kafkav1beta2.KafkaSpecKafkaAuthorization{
					Type: kafkav1beta2.KafkaSpecKafkaAuthorizationTypeSimple,
				},
				Replicas: 3,
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

	k.setAffinity(mgh, kafkaCluster)
	k.setTolerations(mgh, kafkaCluster)
	k.setMetricsConfig(mgh, kafkaCluster)
	k.setImagePullSecret(mgh, kafkaCluster)

	return kafkaCluster
}

// set metricsConfig for kafka cluster based on the mgh enableMetrics
func (k *strimziTransporter) setMetricsConfig(mgh *operatorv1alpha4.MulticlusterGlobalHub,
	kafkaCluster *kafkav1beta2.Kafka,
) {
	kafkaMetricsConfig := &kafkav1beta2.KafkaSpecKafkaMetricsConfig{}
	zookeeperMetricsConfig := &kafkav1beta2.KafkaSpecZookeeperMetricsConfig{}
	if mgh.Spec.EnableMetrics {
		kafkaMetricsConfig = &kafkav1beta2.KafkaSpecKafkaMetricsConfig{
			Type: kafkav1beta2.KafkaSpecKafkaMetricsConfigTypeJmxPrometheusExporter,
			ValueFrom: kafkav1beta2.KafkaSpecKafkaMetricsConfigValueFrom{
				ConfigMapKeyRef: &kafkav1beta2.KafkaSpecKafkaMetricsConfigValueFromConfigMapKeyRef{
					Name: &KakfaMetricsConfigmapName,
					Key:  &KafkaMetricsConfigmapKeyRef,
				},
			},
		}
		zookeeperMetricsConfig = &kafkav1beta2.KafkaSpecZookeeperMetricsConfig{
			Type: kafkav1beta2.KafkaSpecZookeeperMetricsConfigTypeJmxPrometheusExporter,
			ValueFrom: kafkav1beta2.KafkaSpecZookeeperMetricsConfigValueFrom{
				ConfigMapKeyRef: &kafkav1beta2.KafkaSpecZookeeperMetricsConfigValueFromConfigMapKeyRef{
					Name: &KakfaMetricsConfigmapName,
					Key:  &ZooKeeperMetricsConfigmapKeyRef,
				},
			},
		}
		kafkaCluster.Spec.Kafka.MetricsConfig = kafkaMetricsConfig
		kafkaCluster.Spec.Zookeeper.MetricsConfig = zookeeperMetricsConfig
	}
}

// set affinity for kafka cluster based on the mgh nodeSelector
func (k *strimziTransporter) setAffinity(mgh *operatorv1alpha4.MulticlusterGlobalHub,
	kafkaCluster *kafkav1beta2.Kafka,
) {
	kafkaPodAffinity := &kafkav1beta2.KafkaSpecKafkaTemplatePodAffinity{}
	zookeeperPodAffinity := &kafkav1beta2.KafkaSpecZookeeperTemplatePodAffinity{}
	entityOperatorPodAffinity := &kafkav1beta2.KafkaSpecEntityOperatorTemplatePodAffinity{}

	if mgh.Spec.NodeSelector != nil {
		nodeSelectorReqs := []corev1.NodeSelectorRequirement{}

		for key, value := range mgh.Spec.NodeSelector {
			req := corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{value},
			}
			nodeSelectorReqs = append(nodeSelectorReqs, req)
		}
		nodeSelectorTerms := []corev1.NodeSelectorTerm{
			{
				MatchExpressions: nodeSelectorReqs,
			},
		}

		jsonData, err := json.Marshal(nodeSelectorTerms)
		if err != nil {
			k.log.Error(err, "failed to marshall nodeSelector terms")
		}

		kafkaNodeSelectorTermsElem := make([]kafkav1beta2.
			KafkaSpecKafkaTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecutionNodeSelectorTermsElem,
			0)

		zookeeperNodeSelectorTermsElem := make([]kafkav1beta2.
			KafkaSpecZookeeperTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecutionNodeSelectorTermsElem, 0)
		entityOperatorNodeSelectorTermsElem := make([]kafkav1beta2.
			KafkaSpecEntityOperatorTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecutionNodeSelectorTermsElem, 0)

		err = json.Unmarshal(jsonData, &kafkaNodeSelectorTermsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to kafkaNodeSelectorTermsElem")
		}
		err = json.Unmarshal(jsonData, &zookeeperNodeSelectorTermsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to zookeeperNodeSelectorTermsElem")
		}
		err = json.Unmarshal(jsonData, &entityOperatorNodeSelectorTermsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to entityOperatorNodeSelectorTermsElem")
		}

		zookeeperPodAffinity.NodeAffinity = &kafkav1beta2.KafkaSpecZookeeperTemplatePodAffinityNodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &kafkav1beta2.
				KafkaSpecZookeeperTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution{
				NodeSelectorTerms: zookeeperNodeSelectorTermsElem,
			},
		}
		kafkaPodAffinity.NodeAffinity = &kafkav1beta2.KafkaSpecKafkaTemplatePodAffinityNodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &kafkav1beta2.
				KafkaSpecKafkaTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution{
				NodeSelectorTerms: kafkaNodeSelectorTermsElem,
			},
		}
		entityOperatorPodAffinity.NodeAffinity = &kafkav1beta2.KafkaSpecEntityOperatorTemplatePodAffinityNodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &kafkav1beta2.
				KafkaSpecEntityOperatorTemplatePodAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution{
				NodeSelectorTerms: entityOperatorNodeSelectorTermsElem,
			},
		}

		if kafkaCluster.Spec.Kafka.Template == nil {
			kafkaCluster.Spec.Kafka.Template = &kafkav1beta2.KafkaSpecKafkaTemplate{
				Pod: &kafkav1beta2.KafkaSpecKafkaTemplatePod{
					Affinity: kafkaPodAffinity,
				},
			}
			kafkaCluster.Spec.Zookeeper.Template = &kafkav1beta2.KafkaSpecZookeeperTemplate{
				Pod: &kafkav1beta2.KafkaSpecZookeeperTemplatePod{
					Affinity: zookeeperPodAffinity,
				},
			}
			kafkaCluster.Spec.EntityOperator.Template = &kafkav1beta2.KafkaSpecEntityOperatorTemplate{
				Pod: &kafkav1beta2.KafkaSpecEntityOperatorTemplatePod{
					Affinity: entityOperatorPodAffinity,
				},
			}
		} else {
			kafkaCluster.Spec.Kafka.Template.Pod.Affinity = kafkaPodAffinity
			kafkaCluster.Spec.Zookeeper.Template.Pod.Affinity = zookeeperPodAffinity
			kafkaCluster.Spec.EntityOperator.Template.Pod.Affinity = entityOperatorPodAffinity
		}
	}
}

// setTolerations sets the kafka tolerations based on the mgh tolerations
func (k *strimziTransporter) setTolerations(mgh *operatorv1alpha4.MulticlusterGlobalHub,
	kafkaCluster *kafkav1beta2.Kafka,
) {
	kafkaTolerationsElem := make([]kafkav1beta2.KafkaSpecKafkaTemplatePodTolerationsElem, 0)
	zookeeperTolerationsElem := make([]kafkav1beta2.KafkaSpecZookeeperTemplatePodTolerationsElem, 0)
	entityOperatorTolerationsElem := make([]kafkav1beta2.KafkaSpecEntityOperatorTemplatePodTolerationsElem, 0)

	if mgh.Spec.Tolerations != nil {
		jsonData, err := json.Marshal(mgh.Spec.Tolerations)
		if err != nil {
			k.log.Error(err, "failed to marshal tolerations")
		}
		err = json.Unmarshal(jsonData, &kafkaTolerationsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to KafkaSpecruntimeKafkaTemplatePodTolerationsElem")
		}
		err = json.Unmarshal(jsonData, &zookeeperTolerationsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to KafkaSpecZookeeperTemplatePodTolerationsElem")
		}
		err = json.Unmarshal(jsonData, &entityOperatorTolerationsElem)
		if err != nil {
			k.log.Error(err, "failed to unmarshal to KafkaSpecEntityOperatorTemplatePodTolerationsElem")
		}

		if kafkaCluster.Spec.Kafka.Template == nil {
			kafkaCluster.Spec.Kafka.Template = &kafkav1beta2.KafkaSpecKafkaTemplate{
				Pod: &kafkav1beta2.KafkaSpecKafkaTemplatePod{
					Tolerations: kafkaTolerationsElem,
				},
			}
			kafkaCluster.Spec.Zookeeper.Template = &kafkav1beta2.KafkaSpecZookeeperTemplate{
				Pod: &kafkav1beta2.KafkaSpecZookeeperTemplatePod{
					Tolerations: zookeeperTolerationsElem,
				},
			}
			kafkaCluster.Spec.EntityOperator.Template = &kafkav1beta2.KafkaSpecEntityOperatorTemplate{
				Pod: &kafkav1beta2.KafkaSpecEntityOperatorTemplatePod{
					Tolerations: entityOperatorTolerationsElem,
				},
			}
		} else {
			kafkaCluster.Spec.Kafka.Template.Pod.Tolerations = kafkaTolerationsElem
			kafkaCluster.Spec.Zookeeper.Template.Pod.Tolerations = zookeeperTolerationsElem
			kafkaCluster.Spec.EntityOperator.Template.Pod.Tolerations = entityOperatorTolerationsElem
		}
	}
}

// setImagePullSecret sets the kafka image pull secret based on the mgh imagepullsecret
func (k *strimziTransporter) setImagePullSecret(mgh *operatorv1alpha4.MulticlusterGlobalHub,
	kafkaCluster *kafkav1beta2.Kafka,
) {
	if mgh.Spec.ImagePullSecret != "" {
		existingKafkaSpec := kafkaCluster.Spec
		desiredKafkaSpec := kafkaCluster.Spec.DeepCopy()
		desiredKafkaSpec.EntityOperator.Template = &kafkav1beta2.KafkaSpecEntityOperatorTemplate{
			Pod: &kafkav1beta2.KafkaSpecEntityOperatorTemplatePod{
				ImagePullSecrets: []kafkav1beta2.KafkaSpecEntityOperatorTemplatePodImagePullSecretsElem{
					{
						Name: &mgh.Spec.ImagePullSecret,
					},
				},
			},
		}
		desiredKafkaSpec.Kafka.Template = &kafkav1beta2.KafkaSpecKafkaTemplate{
			Pod: &kafkav1beta2.KafkaSpecKafkaTemplatePod{
				ImagePullSecrets: []kafkav1beta2.KafkaSpecKafkaTemplatePodImagePullSecretsElem{
					{
						Name: &mgh.Spec.ImagePullSecret,
					},
				},
			},
		}
		desiredKafkaSpec.Zookeeper.Template = &kafkav1beta2.KafkaSpecZookeeperTemplate{
			Pod: &kafkav1beta2.KafkaSpecZookeeperTemplatePod{
				ImagePullSecrets: []kafkav1beta2.KafkaSpecZookeeperTemplatePodImagePullSecretsElem{
					{
						Name: &mgh.Spec.ImagePullSecret,
					},
				},
			},
		}
		// marshal to json
		existingKafkaJson, _ := json.Marshal(existingKafkaSpec)
		desiredKafkaJson, _ := json.Marshal(desiredKafkaSpec)

		// patch the desired kafka cluster to the existing kafka cluster
		patchedData, err := jsonpatch.MergePatch(existingKafkaJson, desiredKafkaJson)
		if err != nil {
			klog.Errorf("failed to merge patch, error: %v", err)
			return
		}

		updatedKafkaSpec := &kafkav1beta2.KafkaSpec{}
		err = json.Unmarshal(patchedData, updatedKafkaSpec)
		if err != nil {
			klog.Errorf("failed to umarshal kafkaspec, error: %v", err)
			return
		}
		kafkaCluster.Spec = updatedKafkaSpec
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
