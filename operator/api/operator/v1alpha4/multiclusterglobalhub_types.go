/*
Copyright 2022.

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

package v1alpha4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DataLayerType specifies the type of data layer that global hub stores and transports the data.
// +kubebuilder:validation:Enum:="largeScale"
type DataLayerType string

const (
	// // Native is a DataLayerType using kubernetes native storage and event subscription
	// Native DataLayerType = "native"
	// LargeScale is a DataLayerType using external high performance data storage and transport layer
	LargeScale DataLayerType = "largeScale"
)

// TransportFormatType specifies the type of data format based on kafka implementation.
type TransportFormatType string

const (
	KafkaMessage TransportFormatType = "message"
	CloudEvents  TransportFormatType = "cloudEvents"
)

// AvailabilityType ...
type AvailabilityType string

const (
	// HABasic stands up most app subscriptions with a replicaCount of 1
	HABasic AvailabilityType = "Basic"
	// HAHigh stands up most app subscriptions with a replicaCount of 2
	HAHigh AvailabilityType = "High"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName={mgh,mcgh}
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="The overall status of the MulticlusterGlobalHub"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{Deployment,v1,multicluster-global-hub-operator}}
// MulticlusterGlobalHub defines the configuration for an instance of the multiCluster global hub
type MulticlusterGlobalHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec specifies the desired state of multicluster global hub
	// +kubebuilder:default={dataLayer: {postgres: {retention: "18m"}}}
	Spec MulticlusterGlobalHubSpec `json:"spec,omitempty"`
	// Status specifies the observed state of multicluster global hub
	Status MulticlusterGlobalHubStatus `json:"status,omitempty"`
}

// MulticlusterGlobalHubSpec defines the desired state of multicluster global hub
type MulticlusterGlobalHubSpec struct {
	// AvailabilityType specifies deployment replication for improved availability. Options are: Basic and High (default)
	// +kubebuilder:default:="High"
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Availability Configuration"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:High","urn:alm:descriptor:com.tectonic.ui:select:Basic"}
	AvailabilityConfig AvailabilityType `json:"availabilityConfig,omitempty"`
	// ImagePullPolicy specifies the pull policy of the multicluster global hub images
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"}
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// ImagePullSecret specifies the pull secret of the multicluster global hub images
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:io.kubernetes:Secret"}
	// +optional
	ImagePullSecret string `json:"imagePullSecret,omitempty"`
	// NodeSelector specifies the desired state of NodeSelector
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations causes all components to tolerate any taints
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// DataLayerSpec can be configured to use a different data layer
	// +kubebuilder:default={postgres: {retention: "18m"}}
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	DataLayerSpec DataLayerSpec `json:"dataLayer"`
	// AdvancedSpec specifies the advanced configurations for the multicluster global hub
	// +optional
	AdvancedSpec *AdvancedSpec `json:"advanced,omitempty"`
	// EnableMetrics enables the metrics for the global hub created kafka and postgres components.
	// If the user provides the kafka and postgres, then the enablemetrics variable is useless.
	// +kubebuilder:default=true
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	// +optional
	EnableMetrics bool `json:"enableMetrics"`
}

type AdvancedSpec struct {
	// Grafana specifies the desired state of grafana
	// +optional
	Grafana *CommonSpec `json:"grafana,omitempty"`

	// Kafka specifies the desired state of kafka
	// +optional
	Kafka *CommonSpec `json:"kafka,omitempty"`

	// Postgres specifies the desired state of postgres
	// +optional
	Postgres *CommonSpec `json:"postgres,omitempty"`

	// Manager specifies the desired state of multicluster global hub manager
	// +optional
	Manager *CommonSpec `json:"manager,omitempty"`

	// Agent specifies the desired state of multicluster global hub agent
	// +optional
	Agent *CommonSpec `json:"agent,omitempty"`
}

type CommonSpec struct {
	// Compute Resources required by this component
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// ResourceRequirements copied from corev1.ResourceRequirements
// We do not need to support ResourceClaim
type ResourceRequirements struct {
	// Requests describes the minimum amount of compute resources required.
	// If requests are omitted for a container, it defaults to the specified limits.
	// If there are no specified limits, it defaults to an implementation-defined value.
	// For more information, see: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// DataLayerSpec is a discriminated union of data layer specific configuration.
type DataLayerSpec struct {
	// Kafka specifies the desired state of kafka
	// +kubebuilder:default={"topics": {"specTopic": "gh-spec", "statusTopic": "gh-status.*"}}
	Kafka KafkaSpec `json:"kafka,omitempty"`
	// Postgres specifies the desired state of postgres
	// +kubebuilder:default={retention: "18m"}
	Postgres PostgresSpec `json:"postgres,omitempty"`
	// StorageClass specifies the class for storage
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
}

// PostgresSpec defines the desired state of postgres
type PostgresSpec struct {
	// Retention is a duration string, defining how long to keep the data in the database.
	// The recommended minimum value is 1 month, and the default value is 18 months.
	// A duration string is a signed sequence of decimal numbers,
	// each with an optional fraction and a unit suffix, such as "1y6m".
	// Valid time units are "m" and "y"
	// +kubebuilder:default:="18m"
	Retention string `json:"retention,omitempty"`

	// StorageSize specifies the size for storage
	// +optional
	StorageSize string `json:"storageSize,omitempty"`
}

// KafkaSpec defines the desired state of kafka
type KafkaSpec struct {
	// KafkaTopics specify the desired topics
	// +kubebuilder:default={"specTopic": "gh-spec", "statusTopic": "gh-status.*"}
	KafkaTopics KafkaTopics `json:"topics,omitempty"`

	// StorageSize specifies the size for storage
	// +optional
	StorageSize string `json:"storageSize,omitempty"`
}

// KafkaTopics is the transport topics for the manager and agent to communicate to one another
type KafkaTopics struct {
	// SpecTopic is the topic to distribute workloads from global hub to managed hubs. The default value is "gh-spec"
	// +kubebuilder:default="gh-spec"
	SpecTopic string `json:"specTopic,omitempty"`

	// StatusTopic specifies the topic where an agent reports events and status updates to a manager.
	// Specifically, the topic can end up with an asterisk (*), indicating topics for individual managed hubs.
	// For example: the default value is "gh-status.*" for the global hub built-in kafka. Therefore, the topic
	// for the hub cluster named "hub1" would be "gh-status.hub1"; In the BYO case, the default value for all
	// managed hubs is "gh-status"
	// +kubebuilder:default="gh-status.*"
	StatusTopic string `json:"statusTopic,omitempty"`
}

// MulticlusterGlobalHubStatus defines the observed state of multicluster global hub
type MulticlusterGlobalHubStatus struct {
	// Conditions represents the latest available observations of the current state
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Components list the globalhub components status
	// +optional
	Components map[string]StatusCondition `json:"components,omitempty"`

	// Represents the running phase of the MulticlusterGlobalHub
	// +kubebuilder:default:="Progressing"
	// +optional
	Phase GlobalHubPhaseType `json:"phase"`
}
type GlobalHubPhaseType string

const (
	GlobalHubRunning      GlobalHubPhaseType = "Running"
	GlobalHubProgressing  GlobalHubPhaseType = "Progressing"
	GlobalHubUninstalling GlobalHubPhaseType = "Uninstalling"
	GlobalHubError        GlobalHubPhaseType = "Error"
)

// StatusCondition contains condition information.
type StatusCondition struct {
	// The component name
	Name string `json:"name,omitempty"`

	// The resource kind this condition represents
	Kind string `json:"kind,omitempty"`

	// Type is the type of the cluster condition.
	// +required
	Type string `json:"type,omitempty"`

	// Status is the status of the condition. One of True, False, Unknown.
	// +required
	Status metav1.ConditionStatus `json:"status,omitempty"`

	// LastTransitionTime is the last time the condition changed from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// Reason is a (brief) reason for the condition's last status change.
	// +required
	Reason string `json:"reason,omitempty"`

	// Message is a human-readable message indicating details about the last status change.
	// +required
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// MulticlusterGlobalHubList contains a list of MulticlusterGlobalHub
type MulticlusterGlobalHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MulticlusterGlobalHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MulticlusterGlobalHub{}, &MulticlusterGlobalHubList{})
}

func (mgh *MulticlusterGlobalHub) GetConditions() []metav1.Condition {
	return mgh.Status.Conditions
}

func (mgh *MulticlusterGlobalHub) SetConditions(conditions []metav1.Condition) {
	mgh.Status.Conditions = conditions
}
