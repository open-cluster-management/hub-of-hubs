package utils

type OptionsContainer struct {
	Options Options `yaml:"options"`
}

// Define options available for Tests to consume
type Options struct {
	HubCluster      GlobalHubCluster `yaml:"hub"`
	ManagedClusters []ManagedCluster `yaml:"clusters"`
}

// Define the shape of clusters that may be added under management
type GlobalHubCluster struct {
	Name             string `yaml:"name,omitempty"`
	Namespace        string `yaml:"namespace,omitempty"`
	ApiServer        string `yaml:"apiServer,omitempty"`
	KubeConfig       string `yaml:"kubeconfig,omitempty"`
	KubeContext      string `yaml:"kubecontext,omitempty"`
	Nonk8sApiServer  string `yaml:"nonk8sApiServer,omitempty"`
	DatabaseURI      string `yaml:"databaseURI,omitempty"`
	ManagerImageREF  string `yaml:"ManagerImageREF,omitempty"`
	AgentImageREF    string `yaml:"AgentImageREF,omitempty"`
	OperatorImageREF string `yaml:"OperatorImageREF,omitempty"`
}

type ManagedCluster struct {
	Name        string `yaml:"name,omitempty"`
	LeafHubName string `yaml:"leafhubname,omitempty"`
	KubeConfig  string `yaml:"kubeconfig,omitempty"`
	KubeContext string `yaml:"kubecontext,omitempty"`
}
