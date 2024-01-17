// Copyright (c) 2024 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package hubofhubs

import (
	"context"

	kafkav1beta2 "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

type middlewareController struct {
	mgr        ctrl.Manager
	reconciler *MulticlusterGlobalHubReconciler
}

func (m *middlewareController) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	// get the mcgh cr name and then trigger the globalhub reconciler
	mgh := &globalhubv1alpha4.MulticlusterGlobalHub{}
	err := m.mgr.GetClient().Get(ctx, config.GetMGHNamespacedName(), mgh)
	if err != nil {
		klog.Error(err, "Failed to get MulticlusterGlobalHub")
		return ctrl.Result{}, err
	}
	_, err = m.reconciler.ReconcileTransport(ctx, mgh)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

var kafkaPred = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectNew.GetResourceVersion() != e.ObjectOld.GetResourceVersion()
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return e.Object.GetNamespace() == utils.GetDefaultNamespace()
	},
}

// this controller is used to watch the Kafka/KafkaTopic/KafkaUser custom resource
func StartMiddlewareController(ctx context.Context, mgr ctrl.Manager, reconciler *MulticlusterGlobalHubReconciler) (
	*builder.Builder, error) {
	transProtocol, err := detectTransportProtocol(ctx, mgr.GetClient())
	if err != nil {
		return nil, err
	}
	if transProtocol == transport.StrimziTransporter {
		controller := ctrl.NewControllerManagedBy(mgr)
		return controller, controller.
			Named("kafka_middleware_controller").
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
	return nil, nil
}
