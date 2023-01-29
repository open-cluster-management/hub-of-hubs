// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	"github.com/go-logr/logr"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/nonk8sapi"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/scheme"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer/db2transport/db/postgresql"
	specsyncer "github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer/db2transport/syncer"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer/spec2db"
	statusbundle "github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/transport2db/bundle"
	"github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/transport2db/db/workerpool"
	statussyncer "github.com/stolostron/multicluster-global-hub/manager/pkg/statussyncer/transport2db/syncer"
	mgrwebhook "github.com/stolostron/multicluster-global-hub/manager/pkg/webhook"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/helpers"
	"github.com/stolostron/multicluster-global-hub/pkg/conflator"
	commonobjects "github.com/stolostron/multicluster-global-hub/pkg/objects"
	"github.com/stolostron/multicluster-global-hub/pkg/statistics"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/consumer"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/producer"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/protocol"
)

const (
	metricsHost                = "0.0.0.0"
	metricsPort          int32 = 8384
	webhookPort                = 9443
	webhookCertDir             = "/webhook-certs"
	kafkaTransportType         = "kafka"
	leaderElectionLockID       = "multicluster-global-hub-lock"
)

var (
	errFlagParameterEmpty        = errors.New("flag parameter empty")
	errFlagParameterIllegalValue = errors.New("flag parameter illegal value")
)

type hohManagerConfig struct {
	managerNamespace      string
	watchNamespace        string
	syncerConfig          *syncerConfig
	databaseConfig        *databaseConfig
	transportConfig       *transport.TransportConfig
	statisticsConfig      *statistics.StatisticsConfig
	nonK8sAPIServerConfig *nonk8sapi.NonK8sAPIServerConfig
	electionConfig        *commonobjects.LeaderElectionConfig
}

type syncerConfig struct {
	specSyncInterval              time.Duration
	statusSyncInterval            time.Duration
	deletedLabelsTrimmingInterval time.Duration
}

type databaseConfig struct {
	processDatabaseURL         string
	transportBridgeDatabaseURL string
}

func parseFlags() (*hohManagerConfig, error) {
	managerConfig := &hohManagerConfig{
		syncerConfig:   &syncerConfig{},
		databaseConfig: &databaseConfig{},
		transportConfig: &transport.TransportConfig{
			KafkaConfig: &protocol.KafkaConfig{
				EnableTSL:      true,
				ProducerConfig: &protocol.KafkaProducerConfig{},
				ConsumerConfig: &protocol.KafkaConsumerConfig{},
			},
		},
		statisticsConfig:      &statistics.StatisticsConfig{},
		nonK8sAPIServerConfig: &nonk8sapi.NonK8sAPIServerConfig{},
		electionConfig:        &commonobjects.LeaderElectionConfig{},
	}

	pflag.StringVar(&managerConfig.managerNamespace, "manager-namespace", "open-cluster-management",
		"The manager running namespace, also used as leader election namespace.")
	pflag.StringVar(&managerConfig.watchNamespace, "watch-namespace", "",
		"The watching namespace of the controllers, multiple namespace must be splited by comma.")
	pflag.DurationVar(&managerConfig.syncerConfig.specSyncInterval, "spec-sync-interval", 5*time.Second,
		"The synchronization interval of resources in spec.")
	pflag.DurationVar(&managerConfig.syncerConfig.statusSyncInterval, "status-sync-interval", 5*time.Second,
		"The synchronization interval of resources in status.")
	pflag.DurationVar(&managerConfig.syncerConfig.deletedLabelsTrimmingInterval, "deleted-labels-trimming-interval",
		5*time.Second, "The trimming interval of deleted labels.")
	pflag.StringVar(&managerConfig.databaseConfig.processDatabaseURL, "process-database-url", "",
		"The URL of database server for the process user.")
	pflag.StringVar(&managerConfig.databaseConfig.transportBridgeDatabaseURL,
		"transport-bridge-database-url", "", "The URL of database server for the transport-bridge user.")
	pflag.StringVar(&managerConfig.transportConfig.TransportType, "transport-type", "kafka",
		"The transport type, 'kafka'.")
	pflag.StringVar(&managerConfig.transportConfig.MessageCompressionType, "transport-message-compression-type",
		"gzip", "The message compression type for transport layer, 'gzip' or 'no-op'.")
	pflag.DurationVar(&managerConfig.transportConfig.CommitterInterval, "transport-committer-interval",
		40*time.Second, "The committer interval for transport layer.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.BootstrapServer, "kafka-bootstrap-server",
		"kafka-brokers-cluster-kafka-bootstrap.kafka.svc:9092", "The bootstrap server for kafka.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.CertPath, "kafka-ca-path", "",
		"The certificate path of CA certificate for kafka bootstrap server.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.ProducerConfig.ProducerID, "kakfa-producer-id",
		"multicluster-global-hub", "ID for the kafka producer.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.ProducerConfig.ProducerTopic, "kakfa-producer-topic",
		"spec", "Topic for the kafka producer.")
	pflag.IntVar(&managerConfig.transportConfig.KafkaConfig.ProducerConfig.MessageSizeLimitKB, "kafka-message-size-limit", 940,
		"The limit for kafka message size in KB.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.ConsumerConfig.ConsumerID, "kakfa-consumer-id", "multicluster-global-hub",
		"ID for the kafka consumer.")
	pflag.StringVar(&managerConfig.transportConfig.KafkaConfig.ConsumerConfig.ConsumerTopic, "kakfa-consumer-topic", "status",
		"Topic for the kafka consumer.")
	pflag.DurationVar(&managerConfig.statisticsConfig.LogInterval, "statistics-log-interval", 0*time.Second,
		"The log interval for statistics.")
	pflag.StringVar(&managerConfig.nonK8sAPIServerConfig.ClusterAPIURL, "cluster-api-url",
		"https://kubernetes.default.svc:443", "The cluster API URL for nonK8s API server.")
	pflag.StringVar(&managerConfig.nonK8sAPIServerConfig.ClusterAPICABundlePath, "cluster-api-cabundle-path",
		"/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", "The CA bundle path for cluster API.")
	pflag.StringVar(&managerConfig.nonK8sAPIServerConfig.ServerBasePath, "server-base-path",
		"/global-hub-api/v1", "The base path for nonK8s API server.")
	pflag.IntVar(&managerConfig.electionConfig.LeaseDuration, "lease-duration", 137, "controller leader lease duration")
	pflag.IntVar(&managerConfig.electionConfig.RenewDeadline, "renew-deadline", 107, "controller leader renew deadline")
	pflag.IntVar(&managerConfig.electionConfig.RetryPeriod, "retry-period", 26, "controller leader retry period")
	// add flags for logger
	pflag.CommandLine.AddFlagSet(zap.FlagSet())
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if managerConfig.databaseConfig.processDatabaseURL == "" {
		return nil, fmt.Errorf("database url for process user: %w", errFlagParameterEmpty)
	}

	if managerConfig.databaseConfig.transportBridgeDatabaseURL == "" {
		return nil, fmt.Errorf("database url for transport-bridge user: %w", errFlagParameterEmpty)
	}

	if managerConfig.transportConfig.KafkaConfig.ProducerConfig.MessageSizeLimitKB > producer.MaxMessageSizeLimit {
		return nil, fmt.Errorf("%w - size must not exceed %d : %s", errFlagParameterIllegalValue,
			managerConfig.transportConfig.KafkaConfig.ProducerConfig.MessageSizeLimitKB, "kafka-message-size-limit")
	}

	return managerConfig, nil
}

func initializeLogger() logr.Logger {
	ctrl.SetLogger(zap.Logger())
	log := ctrl.Log.WithName("cmd")

	return log
}

func printVersion(log logr.Logger) {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

// function to determine whether the transport component requires initial-dependencies between bundles to be checked
// (on load). If the returned is false, then we may assume that dependency of the initial bundle of
// each type is met. Otherwise, there are no guarantees and the dependencies must be checked.
func requireInitialDependencyChecks(transportType string) bool {
	switch transportType {
	case kafkaTransportType:
		return false
		// once kafka consumer loads up, it starts reading from the earliest un-processed bundle,
		// as in all bundles that precede the latter have been processed, which include its dependency
		// bundle.

		// the order guarantee also guarantees that if while loading this component, a new bundle is written to a-
		// partition, then surely its dependency was written before it (leaf-hub-status-sync on kafka guarantees).
	default:
		return true
	}
}

// function to choose status transport type based on env var.
func getStatusTransport(transportConfig *transport.TransportConfig, conflationMgr *conflator.ConflationManager, statistics *statistics.Statistics,
) (consumer.Consumer, error) {
	switch transportConfig.TransportType {
	case kafkaTransportType:
		kafkaConsumer, err := consumer.NewKafkaConsumer(
			transportConfig.KafkaConfig.BootstrapServer, transportConfig.KafkaConfig.CertPath, transportConfig.KafkaConfig.ConsumerConfig,
			ctrl.Log.WithName("kafka-consumer"))
		if err != nil {
			return nil, fmt.Errorf("failed to create kafka-consumer: %w", err)
		}
		kafkaConsumer.SetConflationManager(conflationMgr)
		kafkaConsumer.SetCommitter(consumer.NewCommitter(
			transportConfig.CommitterInterval,
			transportConfig.KafkaConfig.ConsumerConfig.ConsumerTopic, kafkaConsumer.Consumer(),
			conflationMgr.GetBundlesMetadata, ctrl.Log.WithName("kafka-consumer")),
		)
		kafkaConsumer.SetStatistics(statistics)

		return kafkaConsumer, nil
	default:
		return nil, fmt.Errorf("%w: transport-type - %s is not a valid option",
			errFlagParameterIllegalValue, transportConfig.TransportType)
	}
}

func createManager(restConfig *rest.Config, managerConfig *hohManagerConfig, processPostgreSQL,
	transportBridgePostgreSQL *postgresql.PostgreSQL, workersPool *workerpool.DBWorkerPool,
	conflationManager *conflator.ConflationManager, conflationReadyQueue *conflator.ConflationReadyQueue,
	statistics *statistics.Statistics,
) (ctrl.Manager, error) {
	leaseDuration := time.Duration(managerConfig.electionConfig.LeaseDuration) * time.Second
	renewDeadline := time.Duration(managerConfig.electionConfig.RenewDeadline) * time.Second
	retryPeriod := time.Duration(managerConfig.electionConfig.RetryPeriod) * time.Second
	options := ctrl.Options{
		Namespace:               managerConfig.watchNamespace,
		MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		LeaderElection:          true,
		LeaderElectionNamespace: managerConfig.managerNamespace,
		LeaderElectionID:        leaderElectionLockID,
		LeaseDuration:           &leaseDuration,
		RenewDeadline:           &renewDeadline,
		RetryPeriod:             &retryPeriod,
		Port:                    webhookPort,
		CertDir:                 webhookCertDir,
		TLSOpts: []func(*tls.Config){
			func(config *tls.Config) {
				config.MinVersion = tls.VersionTLS12
			},
		},
	}

	// Add support for MultiNamespace set in WATCH_NAMESPACE (e.g ns1,ns2)
	// Note that this is not intended to be used for excluding namespaces, this is better done via a Predicate
	// Also note that you may face performance issues when using this with a high number of namespaces.
	// More Info: https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/cache#MultiNamespacedCacheBuilder
	if strings.Contains(managerConfig.watchNamespace, ",") {
		options.Namespace = ""
		options.NewCache = cache.MultiNamespacedCacheBuilder(
			strings.Split(managerConfig.watchNamespace, ","))
	}

	mgr, err := ctrl.NewManager(restConfig, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new manager: %w", err)
	}

	if err := scheme.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, fmt.Errorf("failed to add schemes: %w", err)
	}

	// DB worker pool initialization
	if err := mgr.Add(workersPool); err != nil {
		return nil, fmt.Errorf("failed to add DB worker pool: %w", err)
	}

	// status transport layer initialization
	statusTransportObj, err := getStatusTransport(managerConfig.transportConfig, conflationManager, statistics)
	if err != nil {
		return nil, fmt.Errorf("failed to init status transport bridge: %w", err)
	}

	// spec transport layer initialization
	producer, err := producer.NewGenericProducer(managerConfig.transportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to init spec transport bridge: %w", err)
	}
	// specTransportObj, err := getSpecTransport(managerConfig.transportConfig)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to init spec transport bridge: %w", err)
	// }
	// if err := mgr.Add(specTransportObj); err != nil {
	// 	return nil, fmt.Errorf("failed to add spec transport bridge: %w", err)
	// }

	if err := mgr.Add(statusTransportObj); err != nil {
		return nil, fmt.Errorf("failed to add status transport bridge: %w", err)
	}

	if err := nonk8sapi.AddNonK8sApiServer(mgr, processPostgreSQL,
		managerConfig.nonK8sAPIServerConfig); err != nil {
		return nil, fmt.Errorf("failed to add non-k8s-api-server: %w", err)
	}

	if err := spec2db.AddSpec2DBControllers(mgr, processPostgreSQL); err != nil {
		return nil, fmt.Errorf("failed to add spec-to-db controllers: %w", err)
	}

	if err := specsyncer.AddDB2TransportSyncers(mgr, transportBridgePostgreSQL, producer,
		managerConfig.syncerConfig.specSyncInterval); err != nil {
		return nil, fmt.Errorf("failed to add db-to-transport syncers: %w", err)
	}

	if err := specsyncer.AddStatusDBWatchers(mgr, transportBridgePostgreSQL, transportBridgePostgreSQL,
		managerConfig.syncerConfig.deletedLabelsTrimmingInterval); err != nil {
		return nil, fmt.Errorf("failed to add status db watchers: %w", err)
	}

	if err := statussyncer.AddTransport2DBSyncers(mgr, workersPool, conflationManager, conflationReadyQueue,
		statusTransportObj, statistics); err != nil {
		return nil, fmt.Errorf("failed to add transport-to-db syncers: %w", err)
	}

	return mgr, nil
}

// function to handle defers with exit, see https://stackoverflow.com/a/27629493/553720.
func doMain(ctx context.Context, restConfig *rest.Config) int {
	log := initializeLogger()
	printVersion(log)
	// create hoh manager configuration from command parameters
	managerConfig, err := parseFlags()
	if err != nil {
		log.Error(err, "flags parse error")
		return 1
	}

	// create statistics
	stats := statistics.NewStatistics(ctrl.Log.WithName("statistics"), managerConfig.statisticsConfig,
		[]string{
			helpers.GetBundleType(&statusbundle.ManagedClustersStatusBundle{}),
			helpers.GetBundleType(&statusbundle.ClustersPerPolicyBundle{}),
			helpers.GetBundleType(&statusbundle.CompleteComplianceStatusBundle{}),
			helpers.GetBundleType(&statusbundle.DeltaComplianceStatusBundle{}),
			helpers.GetBundleType(&statusbundle.MinimalComplianceStatusBundle{}),
			helpers.GetBundleType(&statusbundle.PlacementRulesBundle{}),
			helpers.GetBundleType(&statusbundle.PlacementsBundle{}),
			helpers.GetBundleType(&statusbundle.PlacementDecisionsBundle{}),
			helpers.GetBundleType(&statusbundle.SubscriptionStatusesBundle{}),
			helpers.GetBundleType(&statusbundle.SubscriptionReportsBundle{}),
			helpers.GetBundleType(&statusbundle.ControlInfoBundle{}),
			helpers.GetBundleType(&statusbundle.LocalPolicySpecBundle{}),
			helpers.GetBundleType(&statusbundle.LocalClustersPerPolicyBundle{}),
			helpers.GetBundleType(&statusbundle.LocalCompleteComplianceStatusBundle{}),
			helpers.GetBundleType(&statusbundle.LocalPlacementRulesBundle{}),
		})

	// db layer initialization for process user
	processPostgreSQL, err := postgresql.NewPostgreSQL(managerConfig.databaseConfig.processDatabaseURL)
	if err != nil {
		log.Error(err, "failed to initilize process PostgreSQL")
		return 1
	}
	defer processPostgreSQL.Stop()

	// db layer initialization for transport-bridge user
	transportBridgePostgreSQL, err := postgresql.NewPostgreSQL(
		managerConfig.databaseConfig.transportBridgeDatabaseURL)
	if err != nil {
		log.Error(err, "failed to initilize transport-bridge PostgreSQL")
		return 1
	}
	defer transportBridgePostgreSQL.Stop()

	// db layer initialization - worker pool + connection pool
	dbWorkerPool, err := workerpool.NewDBWorkerPool(ctrl.Log.WithName("db-worker-pool"),
		managerConfig.databaseConfig.transportBridgeDatabaseURL, stats)
	if err != nil {
		log.Error(err, "failed to initilize DBWorkerPool")
		return 1
	}

	// conflationReadyQueue is shared between conflation manager and dispatcher
	conflationReadyQueue := conflator.NewConflationReadyQueue(stats)
	requireInitialDependencyChecks := requireInitialDependencyChecks(
		managerConfig.transportConfig.TransportType)
	conflationManager := conflator.NewConflationManager(ctrl.Log.WithName("conflation"), conflationReadyQueue,
		requireInitialDependencyChecks, stats) // manage all Conflation Units

	mgr, err := createManager(restConfig, managerConfig, processPostgreSQL, transportBridgePostgreSQL,
		dbWorkerPool, conflationManager, conflationReadyQueue, stats)
	if err != nil {
		log.Error(err, "failed to create manager")
		return 1
	}

	hookServer := mgr.GetWebhookServer()
	log.Info("registering webhooks to the webhook server")
	hookServer.Register("/mutating", &webhook.Admission{
		Handler: &mgrwebhook.AdmissionHandler{Client: mgr.GetClient()},
	})

	log.Info("Starting the Cmd.")

	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "manager exited non-zero")
		return 1
	}

	return 0
}

func main() {
	os.Exit(doMain(ctrl.SetupSignalHandler(), ctrl.GetConfigOrDie()))
}
