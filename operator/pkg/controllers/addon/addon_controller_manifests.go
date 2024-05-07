package addon

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/stolostron/cluster-lifecycle-api/helpers/imageregistry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
)

//go:embed manifests/templates
//go:embed manifests/templates/agent
//go:embed manifests/templates/hostedagent
//go:embed manifests/templates/hubcluster
var FS embed.FS

type ManifestsConfig struct {
	HoHAgentImage          string
	ImagePullSecretName    string
	ImagePullSecretData    string
	ImagePullPolicy        string
	LeafHubID              string
	KafkaBootstrapServer   string
	TransportType          string
	KafkaCACert            string
	KafkaClientCert        string
	KafkaClientKey         string
	KafkaConsumerTopic     string
	KafkaProducerTopic     string
	KafkaEventTopic        string
	MessageCompressionType string
	InstallACMHub          bool
	Channel                string
	CurrentCSV             string
	Source                 string
	SourceNamespace        string
	InstallHostedMode      bool
	LeaseDuration          string
	RenewDeadline          string
	RetryPeriod            string
	KlusterletNamespace    string
	KlusterletWorkSA       string
	NodeSelector           map[string]string
	Tolerations            []corev1.Toleration
	AggregationLevel       string
	EnableLocalPolicies    string
	EnableGlobalResource   bool
	AgentQPS               float32
	AgentBurst             int
	LogLevel               string
	EnablePprof            bool
	// cannot use *corev1.ResourceRequirements, addonfactory.StructToValues removes the real value
	Resources *Resources
}

type Resources struct {
	// Limits corresponds to the JSON schema field "limits".
	Limits *apiextensions.JSON `json:"limits,omitempty"`

	// Requests corresponds to the JSON schema field "requests".
	Requests *apiextensions.JSON `json:"requests,omitempty"`
}

type HohAgentAddon struct {
	ctx            context.Context
	client         client.Client
	kubeClient     kubernetes.Interface
	dynamicClient  dynamic.Interface
	log            logr.Logger
	operatorConfig *config.OperatorConfig
}

func (a *HohAgentAddon) getMulticlusterGlobalHub() (*globalhubv1alpha4.MulticlusterGlobalHub, error) {
	mghList := &globalhubv1alpha4.MulticlusterGlobalHubList{}
	err := a.client.List(a.ctx, mghList)
	if err != nil {
		return nil, err
	}
	if len(mghList.Items) != 1 {
		return nil, fmt.Errorf("the count of the mgh instance is not 1 in the cluster.%v", len(mghList.Items))
	}

	return &mghList.Items[0], nil
}

func (a *HohAgentAddon) installACMHub(cluster *clusterv1.ManagedCluster) bool {
	if _, exist := cluster.GetLabels()[operatorconstants.GHAgentACMHubInstallLabelKey]; !exist {
		return false
	}

	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name != constants.HubClusterClaimName {
			continue
		}

		if claim.Value == constants.HubNotInstalled ||
			claim.Value == constants.HubInstalledByGlobalHub {
			return true
		}
	}
	return false
}

func (a *HohAgentAddon) setInstallHostedMode(cluster *clusterv1.ManagedCluster,
	manifestsConfig *ManifestsConfig,
) {
	annotations := cluster.GetAnnotations()
	labels := cluster.GetLabels()
	if annotations[operatorconstants.AnnotationClusterDeployMode] !=
		operatorconstants.ClusterDeployModeHosted {
		return
	}
	if labels[operatorconstants.GHAgentDeployModeLabelKey] !=
		operatorconstants.GHAgentDeployModeHosted {
		return
	}

	manifestsConfig.InstallHostedMode = true
	if annotations[operatorconstants.AnnotationClusterKlusterletDeployNamespace] != "" {
		manifestsConfig.KlusterletNamespace = annotations[operatorconstants.AnnotationClusterKlusterletDeployNamespace]
	}
	manifestsConfig.KlusterletWorkSA = fmt.Sprintf("klusterlet-%s-work-sa", cluster.GetName())
}

func (a *HohAgentAddon) setACMPackageConfigs(manifestsConfig *ManifestsConfig) error {
	pm, err := GetPackageManifestConfig(a.ctx, a.dynamicClient)
	if err != nil {
		return err
	}
	manifestsConfig.Channel = pm.ACMDefaultChannel
	manifestsConfig.CurrentCSV = pm.ACMCurrentCSV
	manifestsConfig.Source = operatorconstants.ACMSubscriptionPublicSource
	manifestsConfig.SourceNamespace = operatorconstants.OpenshiftMarketPlaceNamespace
	return nil
}

func (a *HohAgentAddon) GetValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	log := a.log.WithValues("cluster", cluster.Name)
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = operatorconstants.GHAgentInstallNamespace
	}
	mgh, err := a.getMulticlusterGlobalHub()
	if err != nil {
		log.Error(err, "failed to get MulticlusterGlobalHub")
		return nil, err
	}

	image, err := a.getOverrideImage(mgh, cluster)
	if err != nil {
		return nil, err
	}

	imagePullPolicy := corev1.PullAlways
	if mgh.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mgh.Spec.ImagePullPolicy
	}

	agentQPS, agentBurst := config.GetAgentRestConfig()

	err = utils.WaitTransporterReady(a.ctx, 10*time.Minute)
	if err != nil {
		log.Error(err, "failed to wait transporter")
	}
	transporter := config.GetTransporter()

	// will block until the credential is ready
	kafkaConnection, err := transporter.GetConnCredential(transporter.GenerateUserName(cluster.Name))
	if err != nil {
		return nil, err
	}
	clusterTopic := transporter.GenerateClusterTopic(cluster.Name)

	agentResReq := utils.GetResources(operatorconstants.Agent, mgh.Spec.AdvancedConfig)
	agentRes := &Resources{}
	jsonData, err := json.Marshal(agentResReq)
	if err != nil {
		log.Error(err, "failed to marshal agent resources")
	}
	err = json.Unmarshal(jsonData, agentRes)
	if err != nil {
		log.Error(err, "failed to unmarshal to agent resources")
	}

	electionConfig, err := config.GetElectionConfig()
	if err != nil {
		log.Error(err, "failed to get election config")
	}

	manifestsConfig := ManifestsConfig{
		HoHAgentImage:          image,
		ImagePullPolicy:        string(imagePullPolicy),
		LeafHubID:              cluster.Name,
		KafkaBootstrapServer:   kafkaConnection.BootstrapServer,
		KafkaCACert:            kafkaConnection.CACert,
		KafkaClientCert:        kafkaConnection.ClientCert,
		KafkaClientKey:         kafkaConnection.ClientKey,
		KafkaConsumerTopic:     clusterTopic.SpecTopic,
		KafkaProducerTopic:     clusterTopic.StatusTopic,
		KafkaEventTopic:        clusterTopic.EventTopic,
		MessageCompressionType: string(operatorconstants.GzipCompressType),
		TransportType:          string(transport.Kafka),
		LeaseDuration:          strconv.Itoa(electionConfig.LeaseDuration),
		RenewDeadline:          strconv.Itoa(electionConfig.RenewDeadline),
		RetryPeriod:            strconv.Itoa(electionConfig.RetryPeriod),
		KlusterletNamespace:    "open-cluster-management-agent",
		KlusterletWorkSA:       "klusterlet-work-sa",
		EnableGlobalResource:   a.operatorConfig.GlobalResourceEnabled,
		AgentQPS:               agentQPS,
		AgentBurst:             agentBurst,
		LogLevel:               a.operatorConfig.LogLevel,
		EnablePprof:            a.operatorConfig.EnablePprof,
		Resources:              agentRes,
	}

	if err := a.setImagePullSecret(mgh, cluster, &manifestsConfig); err != nil {
		return nil, err
	}
	log.V(4).Info("rendering manifests", "pullSecret", manifestsConfig.ImagePullSecretName,
		"image", manifestsConfig.HoHAgentImage)

	manifestsConfig.AggregationLevel = config.AggregationLevel
	manifestsConfig.EnableLocalPolicies = config.EnableLocalPolicies

	if a.installACMHub(cluster) {
		manifestsConfig.InstallACMHub = true
		log.Info("installing ACM on managed hub")
		if err := a.setACMPackageConfigs(&manifestsConfig); err != nil {
			return nil, err
		}
	}

	manifestsConfig.Tolerations = mgh.Spec.Tolerations
	manifestsConfig.NodeSelector = mgh.Spec.NodeSelector

	a.setInstallHostedMode(cluster, &manifestsConfig)

	return addonfactory.StructToValues(manifestsConfig), nil
}

// GetImagePullSecret returns the image pull secret name and data
func (a *HohAgentAddon) setImagePullSecret(mgh *globalhubv1alpha4.MulticlusterGlobalHub,
	cluster *clusterv1.ManagedCluster, manifestsConfig *ManifestsConfig,
) error {
	imagePullSecret := &corev1.Secret{}
	// pull secret from the mgh
	if len(mgh.Spec.ImagePullSecret) > 0 {
		err := a.client.Get(context.Background(), client.ObjectKey{
			Namespace: mgh.Namespace,
			Name:      mgh.Spec.ImagePullSecret,
		}, imagePullSecret, &client.GetOptions{})
		if err != nil {
			return err
		}
	}

	// pull secret from the cluster annotation(added by ManagedClusterImageRegistry controller)
	c := imageregistry.NewClient(a.kubeClient)
	if pullSecret, err := c.Cluster(cluster).PullSecret(); err != nil {
		return err
	} else if pullSecret != nil {
		imagePullSecret = pullSecret
	}

	if len(imagePullSecret.Name) > 0 && len(imagePullSecret.Data[corev1.DockerConfigJsonKey]) > 0 {
		manifestsConfig.ImagePullSecretName = imagePullSecret.GetName()
		manifestsConfig.ImagePullSecretData = base64.StdEncoding.EncodeToString(
			imagePullSecret.Data[corev1.DockerConfigJsonKey])
	}
	return nil
}

func (a *HohAgentAddon) getOverrideImage(mgh *globalhubv1alpha4.MulticlusterGlobalHub,
	cluster *clusterv1.ManagedCluster,
) (string, error) {
	// image registry override by operator environment variable and mgh annotation
	configOverrideImage := config.GetImage(config.GlobalHubAgentImageKey)

	// image registry override by cluster annotation(added by the ManagedClusterImageRegistry)
	image, err := imageregistry.OverrideImageByAnnotation(cluster.GetAnnotations(), configOverrideImage)
	if err != nil {
		return "", err
	}
	return image, nil
}
