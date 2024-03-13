package grc

type Compliance struct {
	PolicyID                  string   `json:"policyId"`
	NamespacedName            string   `json:"-"` // need it to delete obj from bundle for these without finalizer.
	CompliantClusters         []string `json:"compliantClusters"`
	NonCompliantClusters      []string `json:"nonCompliantClusters"`
	UnknownComplianceClusters []string `json:"unknownComplianceClusters"`
}

type ComplianceData []Compliance

type DeltaComplianceData []Compliance
