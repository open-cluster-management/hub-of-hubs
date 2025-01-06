/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/stolostron/cluster-lifecycle-api/helpers/imageregistry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/util/sets"
	"open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

const SignerName = "open-cluster-management.io/globalhub-signer"

var isGlobalhubAgentRemoved = true

func GetAgentImage(cluster *clusterv1.ManagedCluster) (string, error) {
	// image registry override by operator environment variable and mgh annotation
	configOverrideImage := GetImage(GlobalHubAgentImageKey)
	// image registry override by cluster annotation(added by the ManagedClusterImageRegistry)
	image, err := imageregistry.OverrideImageByAnnotation(cluster.GetAnnotations(), configOverrideImage)
	if err != nil {
		return "", err
	}
	return image, nil
}

func SetGlobalhubAgentRemoved(agentRemoved bool) {
	isGlobalhubAgentRemoved = agentRemoved
}

func GetGlobalhubAgentRemoved() bool {
	return isGlobalhubAgentRemoved
}

// https://github.com/open-cluster-management-io/ocm/blob/main/pkg/registration/spoke/addon/configuration.go
func AgentCertificateSecretName() string {
	return fmt.Sprintf("%s-%s-client-cert", constants.GHManagedClusterAddonName,
		strings.ReplaceAll(SignerName, "/", "-"))
}

var HostedAddonList = sets.NewString(
	"work-manager",
	"cluster-proxy",
	"managed-serviceaccount",
)

var GlobalHubHostedAddonPlacementStrategy = v1alpha1.PlacementStrategy{
	PlacementRef: v1alpha1.PlacementRef{
		Namespace: utils.GetDefaultNamespace(),
		Name:      "non-local-cluster",
	},
	Configs: []v1alpha1.AddOnConfig{
		{
			ConfigReferent: v1alpha1.ConfigReferent{
				Name:      "global-hub",
				Namespace: utils.GetDefaultNamespace(),
			},
			ConfigGroupResource: v1alpha1.ConfigGroupResource{
				Group:    "addon.open-cluster-management.io",
				Resource: "addondeploymentconfigs",
			},
		},
	},
}

type ManifestsConfig struct {
	HoHAgentImage             string
	ImagePullPolicy           string
	LeafHubID                 string
	TransportConfigSecret     string
	LeaseDuration             string
	RenewDeadline             string
	RetryPeriod               string
	KlusterletNamespace       string
	KlusterletWorkSA          string
	EnableGlobalResource      bool
	TransportFailureThreshold int
	AgentQPS                  float32
	AgentBurst                int
	LogLevel                  string
	EnablePprof               bool
	// cannot use *corev1.ResourceRequirements, addonfactory.StructToValues removes the real value
	Resources                 *Resources
	EnableStackroxIntegration bool
	StackroxPollInterval      time.Duration

	ImagePullSecretName     string
	ImagePullSecretData     string
	KafkaConfigYaml         string
	KafkaClusterCASecret    string
	KafkaClusterCACert      string
	InventoryConfigYaml     string
	InventoryServerCASecret string
	InventoryServerCACert   string
	InstallACMHub           bool
	Channel                 string
	CurrentCSV              string
	Source                  string
	SourceNamespace         string
	InstallHostedMode       bool
	NodeSelector            map[string]string
	Tolerations             []corev1.Toleration
	AggregationLevel        string
	EnableLocalPolicies     string
}

type Resources struct {
	// Requests corresponds to the JSON schema field "requests".
	Requests *apiextensions.JSON `json:"requests,omitempty"`
}
