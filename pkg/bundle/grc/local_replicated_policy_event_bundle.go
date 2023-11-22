package grc

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/pkg/bundle"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/base"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	utils "github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var (
	_ bundle.AgentBundle   = (*LocalReplicatedPolicyEventBundle)(nil)
	_ bundle.ManagerBundle = (*LocalReplicatedPolicyEventBundle)(nil)
)

type LocalReplicatedPolicyEventBundle struct {
	base.BaseReplicatedPolicyEventBundle
	lock          sync.Mutex
	runtimeClient client.Client
	ctx           context.Context
	regex         *regexp.Regexp
	log           logr.Logger
}

// NewAgentLocalReplicatedPolicyEventBundle creates a new instance of ClustersPerPolicyBundle.
func NewAgentLocalReplicatedPolicyEventBundle(ctx context.Context, leafhub string, c client.Client) bundle.AgentBundle {
	return &LocalReplicatedPolicyEventBundle{
		BaseReplicatedPolicyEventBundle: base.BaseReplicatedPolicyEventBundle{
			ReplicatedPolicyEvents: make(map[string][]*base.ReplicatedPolicyEvent),
			LeafHubName:            leafhub,
			BundleVersion:          metadata.NewBundleVersion(),
		},
		lock:          sync.Mutex{},
		runtimeClient: c,
		ctx:           ctx,
		regex:         regexp.MustCompile(`(\w+);`),
		log:           ctrl.Log.WithName("replicas-policy-event-bundle"),
	}
}

func NewManagerLocalReplicatedPolicyEventBundle() bundle.ManagerBundle {
	return &LocalReplicatedPolicyEventBundle{}
}

// Manager - GetLeafHubName returns the leaf hub name that sent the bundle.
func (bundle *LocalReplicatedPolicyEventBundle) GetLeafHubName() string {
	return bundle.LeafHubName
}

// Manager - GetObjects returns the objects in the bundle.
func (bundle *LocalReplicatedPolicyEventBundle) GetObjects() []interface{} {
	objects := make([]interface{}, 0)
	for _, events := range bundle.ReplicatedPolicyEvents {
		for _, event := range events {
			objects = append(objects, event)
		}
	}
	return objects
}

// Manager
func (bundle *LocalReplicatedPolicyEventBundle) SetVersion(version *metadata.BundleVersion) {
	bundle.BundleVersion = version
}

// Agent - UpdateObject function to update a single object inside a bundle.
func (b *LocalReplicatedPolicyEventBundle) UpdateObject(object bundle.Object) {
	b.lock.Lock()
	defer b.lock.Unlock()

	policy, ok := object.(*policiesv1.Policy)
	if !ok {
		return // do not handle objects other than policy
	}
	if policy.Status.Details == nil {
		return // no status to update
	}

	// root policy id
	rootPolicyNamespacedName, ok := policy.Labels[constants.PolicyEventRootPolicyNameLabelKey]
	if !ok {
		return
	}
	rootPolicy, err := utils.GetRootPolicy(b.ctx, b.runtimeClient, rootPolicyNamespacedName)
	if err != nil {
		return
	}

	// cluster id
	clusterName, ok := policy.Labels[constants.PolicyEventClusterNameLabelKey]
	if !ok {
		return
	}
	clusterId, err := utils.GetClusterId(b.ctx, b.runtimeClient, clusterName)
	if err != nil {
		return
	}

	// update the object to bundle
	bundlePolicyStatusEvents, ok := b.ReplicatedPolicyEvents[string(policy.GetUID())]
	if !ok {
		bundlePolicyStatusEvents = make([]*base.ReplicatedPolicyEvent, 0)
	}

	// deprecated events, cause it has been synced before
	deprecatedBundleEvents := make(map[string]*base.ReplicatedPolicyEvent)
	for _, e := range bundlePolicyStatusEvents {
		deprecatedBundleEvents[e.EventName] = e
	}

	for _, detail := range policy.Status.Details {
		if detail.History != nil {
			for _, event := range detail.History {
				bundlePolicyStatusEvents = b.updatePolicyEvents(event,
					string(detail.ComplianceState), deprecatedBundleEvents,
					string(rootPolicy.GetUID()), clusterId, bundlePolicyStatusEvents)
				delete(deprecatedBundleEvents, event.EventName)
			}
		}
	}

	// only load the 'new' events to bundle
	deltaPolicyEvents := make([]*base.ReplicatedPolicyEvent, 0)
	for _, event := range bundlePolicyStatusEvents {
		if _, ok := deprecatedBundleEvents[event.EventName]; !ok {
			deltaPolicyEvents = append(deltaPolicyEvents, event)
		}
	}

	if len(deltaPolicyEvents) > 0 {
		b.ReplicatedPolicyEvents[string(policy.GetUID())] = deltaPolicyEvents
		b.BundleVersion.Incr()
	}
}

// Agent - DeleteObject function to delete a single object inside a bundle.
func (bundle *LocalReplicatedPolicyEventBundle) DeleteObject(object bundle.Object) {
	bundle.lock.Lock()
	defer bundle.lock.Unlock()

	policy, isPolicy := object.(*policiesv1.Policy)
	if !isPolicy {
		return // do not handle objects other than policy
	}

	delete(bundle.ReplicatedPolicyEvents, string(policy.GetUID()))
	// bundle.BundleVersion.Incr() // if the policy is deleted, we don't need to delete the event from database
}

// Agent - GetBundleVersion function to get bundle version.
func (bundle *LocalReplicatedPolicyEventBundle) GetVersion() *metadata.BundleVersion {
	return bundle.BundleVersion
}

func (bundle *LocalReplicatedPolicyEventBundle) ParseCompliance(message string) string {
	match := bundle.regex.FindStringSubmatch(message)
	if len(match) > 1 {
		firstWord := strings.TrimSpace(match[1])
		return firstWord
	}
	return ""
}

// add/update the current status events to bundle, remove the updated event from deprecatedBundleEvents
func (bundle *LocalReplicatedPolicyEventBundle) updatePolicyEvents(event policiesv1.ComplianceHistory,
	parentCompliance string, deprecatedBundleEvents map[string]*base.ReplicatedPolicyEvent,
	rootPolicyId, clusterId string, bundlePolicyStatusEvents []*base.ReplicatedPolicyEvent,
) []*base.ReplicatedPolicyEvent {
	compliance := bundle.ParseCompliance(event.Message)
	if compliance == "" {
		compliance = parentCompliance
	}
	eventTime := event.LastTimestamp.Time
	bundleEvent, ok := deprecatedBundleEvents[event.EventName]
	if ok {
		if !bundleEvent.CreatedAt.Equal(eventTime) {
			bundleEvent.Message = event.Message
			bundleEvent.Count++
			bundleEvent.CreatedAt = eventTime
			bundleEvent.Compliance = compliance
			bundlePolicyStatusEvents = append(bundlePolicyStatusEvents, bundleEvent)

			// the event is updated, remove it from deprecatedBundleEvents
			delete(deprecatedBundleEvents, event.EventName)
		}
	} else {
		bundlePolicyStatusEvents = append(bundlePolicyStatusEvents,
			&base.ReplicatedPolicyEvent{
				PolicyID:   rootPolicyId,
				ClusterID:  clusterId,
				EventName:  event.EventName,
				Compliance: compliance,
				Message:    event.Message,
				Reason:     "PolicyStatusSync", // using this value as a placeholder
				Source:     nil,
				Count:      1,
				CreatedAt:  eventTime,
			})
	}
	return bundlePolicyStatusEvents
}
