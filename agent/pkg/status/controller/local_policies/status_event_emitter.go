package localpolicies

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/go-logr/logr"
	lru "github.com/hashicorp/golang-lru"
	corev1 "k8s.io/api/core/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/generic"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/event"
	"github.com/stolostron/multicluster-global-hub/pkg/bundle/metadata"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/enum"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

var (
	_                          generic.MultiEventEmitter = &statusEventEmitter{}
	MessageCompliaceStateRegex                           = regexp.MustCompile(`(\w+);`)
)

type statusEventEmitter struct {
	ctx             context.Context
	log             logr.Logger
	eventType       string
	runtimeClient   client.Client
	currentVersion  *metadata.BundleVersion
	lastSentVersion metadata.BundleVersion
	payload         event.ReplicatedPolicyEventPayload
	cache           *lru.Cache
	topic           string
}

func StatusEventEmitter(ctx context.Context, runtimeClient client.Client, topic string) generic.MultiEventEmitter {
	cache, _ := lru.New(30)
	return &statusEventEmitter{
		ctx:             ctx,
		log:             ctrl.Log.WithName("local-policy-syncer/status-event"),
		eventType:       string(enum.LocalReplicatedPolicyEventType),
		topic:           topic,
		runtimeClient:   runtimeClient,
		currentVersion:  metadata.NewBundleVersion(),
		lastSentVersion: *metadata.NewBundleVersion(),
		cache:           cache,
		payload:         make([]event.ReplicatedPolicyEvent, 0),
	}
}

// replicated policy
func (h *statusEventEmitter) Predicate(obj client.Object) bool {
	return utils.HasItemKey(obj.GetLabels(), constants.PolicyEventRootPolicyNameLabelKey)
}

func (h *statusEventEmitter) PreSend() bool {
	return h.currentVersion.NewerThan(&h.lastSentVersion)
}

func (h *statusEventEmitter) Topic() string {
	return h.topic
}

func (h *statusEventEmitter) Update(obj client.Object) {
	policy, ok := obj.(*policiesv1.Policy)
	if !ok {
		return // do not handle objects other than policy
	}
	if policy.Status.Details == nil {
		return // no status to update
	}

	rootPolicy, clusterID, err := GetRootPolicyAndClusterID(h.ctx, policy, h.runtimeClient)
	if err != nil {
		h.log.Error(err, "failed to get get rootPolicy/clusterID by replicatedPolicy")
		return
	}

	updated := false
	for _, detail := range policy.Status.Details {
		if detail.History != nil {
			for _, evt := range detail.History {
				key := fmt.Sprintf("%s.%s", evt.EventName, evt.LastTimestamp)
				if h.cache.Contains(key) {
					continue
				}

				h.payload = append(h.payload, event.ReplicatedPolicyEvent{
					BaseEvent: event.BaseEvent{
						EventName:      evt.EventName,
						EventNamespace: policy.Namespace,
						Message:        evt.Message,
						Reason:         "PolicyStatusSync",
						Count:          1,
						Source: corev1.EventSource{
							Component: "policy-status-history-sync",
						},
						CreatedAt: evt.LastTimestamp,
					},
					PolicyID:   string(rootPolicy.GetUID()),
					ClusterID:  clusterID,
					Compliance: GetComplianceState(MessageCompliaceStateRegex, evt.Message, string(detail.ComplianceState)),
				})
				h.cache.Add(key, nil)
				updated = true
			}
		}
	}
	if updated {
		h.currentVersion.Incr()
	}
}

func (*statusEventEmitter) Delete(client.Object) {
	// do nothing
}

func (h *statusEventEmitter) ToCloudEvent() *cloudevents.Event {
	if len(h.payload) < 1 {
		return nil
	}
	e := cloudevents.NewEvent()
	e.SetType(h.eventType)
	e.SetExtension(metadata.ExtVersion, h.currentVersion.String())
	err := e.SetData(cloudevents.ApplicationJSON, h.payload)
	if err != nil {
		h.log.Error(err, "failed to set the payload to cloudvents.Data")
	}
	return &e
}

func (h *statusEventEmitter) PostSend() {
	// update version and clean the cache
	h.payload = make([]event.ReplicatedPolicyEvent, 0)
	h.currentVersion.Next()
	h.lastSentVersion = *h.currentVersion
}

func GetComplianceState(regex *regexp.Regexp, message, defaultVal string) string {
	match := regex.FindStringSubmatch(message)
	if len(match) > 1 {
		firstWord := strings.TrimSpace(match[1])
		return firstWord
	}
	return defaultVal
}

func GetRootPolicyAndClusterID(ctx context.Context, replicatedPolicy *policiesv1.Policy, c client.Client) (
	rootPolicy *policiesv1.Policy, clusterID string, err error,
) {
	rootPolicyNamespacedName := replicatedPolicy.Labels[constants.PolicyEventRootPolicyNameLabelKey]
	rootPolicy, err = utils.GetRootPolicy(ctx, c, rootPolicyNamespacedName)
	if err != nil {
		return nil, "", err
	}

	clusterName, ok := replicatedPolicy.Labels[constants.PolicyEventClusterNameLabelKey]
	if !ok {
		return rootPolicy, clusterID,
			fmt.Errorf("label %s not found in policy %s/%s",
				constants.PolicyEventClusterNameLabelKey, replicatedPolicy.Namespace, replicatedPolicy.Name)
	}
	clusterID, err = utils.GetClusterId(ctx, c, clusterName)
	return rootPolicy, clusterID, err
}
