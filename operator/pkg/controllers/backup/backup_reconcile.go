/*
Copyright 2023.

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

package backup

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

const (
	secretType    = "Secret"
	configmapType = "ConfigMap"
	mghType       = "MulticlusterGlobalHub"
	pvcType       = "PersistentVolumeClaim"
)

var allResourcesBackup = map[string]Backup{
	secretType:    NewSecretBackup(),
	configmapType: NewConfigmapBackup(),
	mghType:       NewMghBackup(),
	pvcType:       NewPvcBackup(),
}

// As we need to watch mgh, secret, configmap. they should be in the same namespace.
// So for request.Namespace, we set it as request type, like "Secret","Configmap","MulticlusterGlobalHub" and so on.
// In the reconcile, we identy the request kind and get it by request.Name.
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// mgh is used to update backup condition
	mghList := &globalhubv1alpha4.MulticlusterGlobalHubList{}
	err := r.Client.List(ctx, mghList)
	if err != nil {
		klog.Error(err, "Failed to list MulticlusterGlobalHub")
		return ctrl.Result{}, err
	}
	if len(mghList.Items) == 0 {
		return ctrl.Result{}, nil
	}
	mgh := mghList.Items[0].DeepCopy()
	// Backup condition means added backup label to all resources already
	backuped := meta.IsStatusConditionTrue(mgh.Status.Conditions, config.CONDITION_TYPE_BACKUP)

	// Check if backup is enabled
	backupEnabled, err := utils.IsBackupEnabled(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If backup is not enable, need to clean up the backup labels
	if !backupEnabled {
		if !backuped {
			return addDisableCondition(ctx, r.Client, mgh, nil)
		}
		err := r.deleteLableOfAllResources(ctx)
		if err != nil {
			r.Log.Error(err, "Failed to delete backup labels")
		}
		return addDisableCondition(ctx, r.Client, mgh, err)
	}

	// If backup is enable, need to add backup label to all resources
	if !backuped {
		err := r.addLableToAllResources(ctx)
		if err != nil {
			r.Log.Error(err, "Failed to add backup labels")
		}
		return addBackupCondition(ctx, r.Client, mgh, err)
	}

	// Watch the changed resources, then update the backuplabel
	r.Log.Info("backup reconcile:", "requestType", req.Namespace, "name", req.Name)
	_, found := allResourcesBackup[req.Namespace]
	if !found {
		return ctrl.Result{}, nil
	}
	err = allResourcesBackup[req.Namespace].AddLabelToOneObj(
		ctx, r.Client,
		config.GetMGHNamespacedName().Namespace, req.Name)
	return addBackupCondition(ctx, r.Client, mgh, err)
}

func addDisableCondition(ctx context.Context, client client.Client,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub, err error,
) (ctrl.Result, error) {
	msg := config.CONDITION_MESSAGE_BACKUP_DISABLED
	if err != nil {
		msg = fmt.Sprintf("Backup is diabled with error: %v.", err.Error())
	}
	if err := config.SetCondition(ctx, client, mgh,
		config.CONDITION_TYPE_BACKUP,
		config.CONDITION_STATUS_FALSE,
		config.CONDITION_REASON_BACKUP_DISABLED,
		msg,
	); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func addBackupCondition(ctx context.Context, client client.Client,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub, err error,
) (ctrl.Result, error) {
	msg := config.CONDITION_MESSAGE_BACKUP
	if err != nil {
		msg = fmt.Sprintf("Added backup labels with error: %v.", err.Error())
	}
	if err := config.SetCondition(ctx, client, mgh,
		config.CONDITION_TYPE_BACKUP,
		config.CONDITION_STATUS_TRUE,
		config.CONDITION_REASON_BACKUP,
		msg,
	); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupReconciler) addLableToAllResources(ctx context.Context) error {
	var errs []error
	r.Log.Info("Add backup label to resources", "namespace", config.GetMGHNamespacedName().Namespace)
	for k, v := range allResourcesBackup {
		r.Log.V(2).Info("Add label to", "kind", k)
		err := v.AddLabelToAllObjs(ctx, r.Client, config.GetMGHNamespacedName().Namespace)
		if err != nil {
			r.Log.Error(err, "Failed to add backup label", "Type", k)
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (r *BackupReconciler) deleteLableOfAllResources(ctx context.Context) error {
	r.Log.Info("Remove backup label of resources")
	var errs []error
	for k, v := range allResourcesBackup {
		err := v.DeleteLabelOfAllObjs(ctx, r.Client, config.GetMGHNamespacedName().Namespace)
		if err != nil {
			r.Log.Error(err, "Failed to add backup label", "Type", k)
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}
