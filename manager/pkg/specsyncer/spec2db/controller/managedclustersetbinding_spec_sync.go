// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controller

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/specsyncer/db2transport/db"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

func AddManagedClusterSetBindingController(mgr ctrl.Manager, specDB db.SpecDB) error {
	managedclustersetbindingPredicate, _ := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      constants.GlobalHubLocalResource,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			},
		},
	})
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1beta2.ManagedClusterSetBinding{}).
		WithEventFilter(managedclustersetbindingPredicate).
		Complete(&genericSpecToDBReconciler{
			client:        mgr.GetClient(),
			specDB:        specDB,
			log:           ctrl.Log.WithName("managedclustersetbindings-spec-syncer"),
			tableName:     "managedclustersetbindings",
			finalizerName: constants.GlobalHubCleanupFinalizer,
			createInstance: func() client.Object {
				return &clusterv1beta2.ManagedClusterSetBinding{}
			},
			cleanObject: cleanManagedClusterSetBindingsStatus,
			areEqual:    areManagedClusterSetBindingsEqual,
		}); err != nil {
		return fmt.Errorf("failed to add managed cluster set binding controller to the manager: %w", err)
	}

	return nil
}

func cleanManagedClusterSetBindingsStatus(instance client.Object) {
	_, ok := instance.(*clusterv1beta2.ManagedClusterSetBinding)
	// ManagedClusterSetBinding has no status
	if !ok {
		panic("wrong instance passed to cleanManagedClusterSetBindingsStatus: not a ManagedClusterSetBinding")
	}
}

func areManagedClusterSetBindingsEqual(instance1, instance2 client.Object) bool {
	managedClusterSetBinding1, ok1 := instance1.(*clusterv1beta2.ManagedClusterSetBinding)
	managedClusterSetBinding2, ok2 := instance2.(*clusterv1beta2.ManagedClusterSetBinding)

	if !ok1 || !ok2 {
		return false
	}

	specMatch := equality.Semantic.DeepEqual(managedClusterSetBinding1.Spec, managedClusterSetBinding2.Spec)
	annotationsMatch := equality.Semantic.DeepEqual(instance1.GetAnnotations(), instance2.GetAnnotations())
	labelsMatch := equality.Semantic.DeepEqual(instance1.GetLabels(), instance2.GetLabels())

	return specMatch && annotationsMatch && labelsMatch
}
