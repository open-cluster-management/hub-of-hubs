package cluster

import (
	"sync"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/base"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

var (
	_ bundle.ManagerBundle = (*HubClusterInfoBundle)(nil)
	_ bundle.AgentBundle   = (*HubClusterInfoBundle)(nil)
)

// HubClusterInfoBundle holds information for leaf hub cluster info status bundle.
type HubClusterInfoBundle struct {
	base.BaseHubClusterInfoBundle
	lock sync.Mutex
}

// LeafHubClusterInfoStatusBundle creates a new instance of LeafHubClusterInfoStatusBundle.
func NewAgentHubClusterInfoBundle(leafHubName string) bundle.AgentBundle {
	return &HubClusterInfoBundle{
		BaseHubClusterInfoBundle: base.BaseHubClusterInfoBundle{
			Objects:       make([]*base.HubClusterInfo, 0),
			LeafHubName:   leafHubName,
			BundleVersion: metadata.NewBundleVersion(),
		},
	}
}

func NewManagerHubClusterInfoBundle() bundle.ManagerBundle {
	return &HubClusterInfoBundle{}
}

// Manager - GetObjects return all the objects that the bundle holds.
func (baseBundle *HubClusterInfoBundle) GetObjects() []interface{} {
	result := make([]interface{}, len(baseBundle.Objects))
	for i, obj := range baseBundle.Objects {
		result[i] = obj
	}

	return result
}

// Manager - GetLeafHubName returns the leaf hub name that sent the bundle.
func (baseBundle *HubClusterInfoBundle) GetLeafHubName() string {
	return baseBundle.LeafHubName
}

// Manager
func (baseBundle *HubClusterInfoBundle) SetVersion(version *metadata.BundleVersion) {
	baseBundle.lock.Lock()
	defer baseBundle.lock.Unlock()
	baseBundle.BundleVersion = version
}

// UpdateObject function to update a single object inside a bundle.
func (bundle *HubClusterInfoBundle) UpdateObject(object bundle.Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	if len(bundle.Objects) == 0 {
		bundle.Objects = []*base.HubClusterInfo{
			{
				ConsoleURL: "",
				GrafanaURL: "",
			},
		}
	}

	var routeURL string
	route := object.(*routev1.Route)
	if len(route.Spec.Host) != 0 {
		routeURL = "https://" + route.Spec.Host
	}

	if route.GetName() == constants.OpenShiftConsoleRouteName && bundle.Objects[0].ConsoleURL != routeURL {
		bundle.Objects[0].ConsoleURL = routeURL
		bundle.BundleVersion.Incr()
	}
	if route.GetName() == constants.ObservabilityGrafanaRouteName && bundle.Objects[0].GrafanaURL != routeURL {
		bundle.Objects[0].GrafanaURL = routeURL
		bundle.BundleVersion.Incr()
	}
}

// DeleteObject function to delete a single object inside a bundle.
func (bundle *HubClusterInfoBundle) DeleteObject(object bundle.Object) {
	if len(bundle.Objects) == 0 {
		return
	}
	if object.GetName() == constants.OpenShiftConsoleRouteName && bundle.Objects[0].ConsoleURL != "" {
		bundle.Objects[0].ConsoleURL = ""
		bundle.BundleVersion.Incr()
	}
	if object.GetName() == constants.ObservabilityGrafanaRouteName && bundle.Objects[0].GrafanaURL != "" {
		bundle.Objects[0].GrafanaURL = ""
		bundle.BundleVersion.Incr()
	}
}

// GetBundleVersion function to get bundle version.
func (bundle *HubClusterInfoBundle) GetVersion() *metadata.BundleVersion {
	return bundle.BundleVersion
}
