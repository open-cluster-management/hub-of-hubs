// Copyright (c) 2024 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package hubofhubs

import (
	"context"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

type middlewareController struct {
	mgr        ctrl.Manager
	reconciler *MulticlusterGlobalHubReconciler
}

func (m *middlewareController) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	// trigger the globalhub reconciler
	return m.reconciler.Reconcile(ctx, request)
}

var kafkaPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return e.Object.GetNamespace() == utils.GetDefaultNamespace()
	},
}

// this controller is used to watch the Kafka/KafkaTopic/KafkaUser custom resource
func StartMiddlewareController(mgr ctrl.Manager, reconciler *MulticlusterGlobalHubReconciler) error {
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&kafkav1beta2.Kafka{},
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(kafkaPred)).
		Watches(&kafkav1beta2.KafkaUser{},
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(kafkaPred)).
		Watches(&kafkav1beta2.KafkaTopic{},
			&handler.EnqueueRequestForObject{}, builder.WithPredicates(kafkaPred)).
		Complete(&middlewareController{
			mgr:        mgr,
			reconciler: reconciler,
		})
}
