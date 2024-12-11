package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/renderer"
	operatorutils "github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

// Postgres: sts, service, secrert(credential), ca
const BuiltinPostgresName = "multicluster-global-hub-postgresql"

var (
	builtinPostgresCAName     = fmt.Sprintf("%s-ca", BuiltinPostgresName)
	builtinPostgresCertName   = fmt.Sprintf("%s-cert", BuiltinPostgresName)
	builtinPostgresConfigName = fmt.Sprintf("%s-config", BuiltinPostgresName)
	builtinPostgresInitName   = fmt.Sprintf("%s-init", BuiltinPostgresName)
	builtinPartialPostgresURI = fmt.Sprintf("@%s.%s.svc:5432/hoh?sslmode=verify-ca", BuiltinPostgresName,
		utils.GetDefaultNamespace())
)

type postgresCredential struct {
	postgresAdminUsername        string
	postgresAdminUserPassword    string
	postgresReadonlyUsername     string
	postgresReadonlyUserPassword string
}

func InitPostgresByStatefulset(ctx context.Context, mgh *globalhubv1alpha4.MulticlusterGlobalHub,
	mgr ctrl.Manager,
) (*config.PostgresConnection, error) {
	// install the postgres statefulset only
	credential, err := getPostgresCredential(ctx, mgh, mgr.GetClient())
	if err != nil {
		return nil, err
	}
	imagePullPolicy := corev1.PullAlways
	if mgh.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mgh.Spec.ImagePullPolicy
	}

	// get the postgres objects
	postgresRenderer, postgresDeployer := renderer.NewHoHRenderer(stsPostgresFS), deployer.NewHoHDeployer(mgr.GetClient())
	postgresObjects, err := postgresRenderer.Render("manifests.sts", "",
		func(profile string) (interface{}, error) {
			return struct {
				Name                         string
				Namespace                    string
				PostgresImage                string
				PostgresExporterImage        string
				StorageSize                  string
				ImagePullSecret              string
				ImagePullPolicy              string
				NodeSelector                 map[string]string
				Tolerations                  []corev1.Toleration
				PostgresConfigName           string
				PostgresCaName               string
				PostgresCertName             string
				PostgresInitName             string
				PostgresAdminUser            string
				PostgresAdminUserPassword    string
				PostgresReadonlyUsername     string
				PostgresReadonlyUserPassword string
				PostgresURI                  string
				StorageClass                 string
				Resources                    *corev1.ResourceRequirements
				EnableMetrics                bool
				EnablePostgresMetrics        bool
				EnableInventoryAPI           bool
			}{
				Name:                         BuiltinPostgresName,
				Namespace:                    mgh.GetNamespace(),
				PostgresImage:                config.GetImage(config.PostgresImageKey),
				PostgresExporterImage:        config.GetImage(config.PostgresExporterImageKey),
				ImagePullSecret:              mgh.Spec.ImagePullSecret,
				ImagePullPolicy:              string(imagePullPolicy),
				NodeSelector:                 mgh.Spec.NodeSelector,
				Tolerations:                  mgh.Spec.Tolerations,
				StorageSize:                  config.GetPostgresStorageSize(mgh),
				PostgresConfigName:           builtinPostgresConfigName,
				PostgresCaName:               builtinPostgresCAName,
				PostgresCertName:             builtinPostgresCertName,
				PostgresInitName:             builtinPostgresInitName,
				PostgresAdminUser:            postgresAdminUsername,
				PostgresAdminUserPassword:    credential.postgresAdminUserPassword,
				PostgresReadonlyUsername:     credential.postgresReadonlyUsername,
				PostgresReadonlyUserPassword: credential.postgresReadonlyUserPassword,
				StorageClass:                 mgh.Spec.DataLayerSpec.StorageClass,
				PostgresURI:                  builtinPartialPostgresURI,
				Resources: operatorutils.GetResources(operatorconstants.Postgres,
					mgh.Spec.AdvancedSpec),
				EnableMetrics:         mgh.Spec.EnableMetrics,
				EnablePostgresMetrics: (!config.IsBYOPostgres()) && mgh.Spec.EnableMetrics,
				EnableInventoryAPI:    config.WithInventory(mgh),
			}, nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to render postgres manifests: %w", err)
	}

	// create restmapper for deployer to find GVR
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	if err = operatorutils.ManipulateGlobalHubObjects(postgresObjects, mgh, postgresDeployer,
		mapper, mgr.GetScheme()); err != nil {
		return nil, fmt.Errorf("failed to create/update postgres objects: %w", err)
	}

	ca, err := getPostgresCA(ctx, mgh, mgr.GetClient())
	if err != nil {
		return nil, err
	}
	return &config.PostgresConnection{
		SuperuserDatabaseURI: "postgresql://" + credential.postgresAdminUsername + ":" +
			credential.postgresAdminUserPassword + builtinPartialPostgresURI,
		ReadonlyUserDatabaseURI: "postgresql://" + credential.postgresReadonlyUsername + ":" +
			credential.postgresReadonlyUserPassword + builtinPartialPostgresURI,
		CACert: []byte(ca),
	}, nil
}

func getPostgresCredential(ctx context.Context, mgh *globalhubv1alpha4.MulticlusterGlobalHub,
	c client.Client,
) (*postgresCredential, error) {
	postgres := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      BuiltinPostgresName,
		Namespace: mgh.Namespace,
	}, postgres); err != nil && errors.IsNotFound(err) {
		return &postgresCredential{
			postgresAdminUsername:        postgresAdminUsername,
			postgresAdminUserPassword:    generatePassword(16),
			postgresReadonlyUsername:     postgresReadonlyUsername,
			postgresReadonlyUserPassword: generatePassword(16),
		}, nil
	} else if err != nil {
		return nil, err
	}
	return &postgresCredential{
		postgresAdminUsername:        postgresAdminUsername,
		postgresAdminUserPassword:    string(postgres.Data["database-admin-password"]),
		postgresReadonlyUsername:     string(postgres.Data["database-readonly-user"]),
		postgresReadonlyUserPassword: string(postgres.Data["database-readonly-password"]),
	}, nil
}

func getPostgresCA(ctx context.Context, mgh *globalhubv1alpha4.MulticlusterGlobalHub, c client.Client) (string, error) {
	ca := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      builtinPostgresCAName,
		Namespace: mgh.Namespace,
	}, ca); err != nil {
		return "", err
	}
	return ca.Data["service-ca.crt"], nil
}
