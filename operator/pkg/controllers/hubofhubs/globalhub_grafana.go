package hubofhubs

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	globalhubv1alpha4 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/deployer"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/renderer"
	operatorutils "github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

const (
	datasourceKey  = "datasources.yaml"
	datasourceName = "multicluster-global-hub-grafana-datasources"

	mergedAlertName   = "multicluster-global-hub-alerting"
	defaultAlertName  = "multicluster-global-hub-default-alerting"
	alertConfigMapKey = "alerting.yaml"

	mergedGrafanaIniName  = "multicluster-global-hub-grafana-config"
	defaultGrafanaIniName = "multicluster-global-hub-default-grafana-config"
	grafanaIniKey         = "grafana.ini"

	grafanaDeploymentName = "multicluster-global-hub-grafana"
)

var (
	supportAlertConfig = sets.NewString(
		"groups",
		"policies",
		"contactPoints",
		"muteTimes",
		"templates",
		"resetPolicies",
	)
	supportDeleteAlertConfig = sets.NewString(
		"deleteRules",
		"deleteContactPoints",
		"deleteTemplates",
		"deleteMuteTimes",
	)
)

func (r *MulticlusterGlobalHubReconciler) reconcileGrafana(ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) error {
	log := r.Log.WithName("grafana")

	// generate random session secret for oauth-proxy
	proxySessionSecret, err := config.GetOauthSessionSecret()
	if err != nil {
		return fmt.Errorf("failed to generate random session secret for grafana oauth-proxy: %v", err)
	}

	imagePullPolicy := corev1.PullAlways
	if mgh.Spec.ImagePullPolicy != "" {
		imagePullPolicy = mgh.Spec.ImagePullPolicy
	}

	replicas := int32(1)
	if mgh.Spec.AvailabilityConfig == globalhubv1alpha4.HAHigh {
		replicas = 2
	}

	// get the grafana objects
	grafanaRenderer, grafanaDeployer := renderer.NewHoHRenderer(fs), deployer.NewHoHDeployer(r.Client)
	grafanaObjects, err := grafanaRenderer.Render("manifests/grafana", "", func(profile string) (interface{}, error) {
		return struct {
			Namespace            string
			Replicas             int32
			SessionSecret        string
			ProxyImage           string
			GrafanaImage         string
			ImagePullSecret      string
			ImagePullPolicy      string
			DatasourceSecretName string
			NodeSelector         map[string]string
			Tolerations          []corev1.Toleration
			Resources            *corev1.ResourceRequirements
			EnableMetrics        bool
		}{
			Namespace:            utils.GetDefaultNamespace(),
			Replicas:             replicas,
			SessionSecret:        proxySessionSecret,
			ProxyImage:           config.GetImage(config.OauthProxyImageKey),
			GrafanaImage:         config.GetImage(config.GrafanaImageKey),
			ImagePullSecret:      mgh.Spec.ImagePullSecret,
			ImagePullPolicy:      string(imagePullPolicy),
			DatasourceSecretName: datasourceName,
			NodeSelector:         mgh.Spec.NodeSelector,
			Tolerations:          mgh.Spec.Tolerations,
			EnableMetrics:        mgh.Spec.EnableMetrics,
			Resources:            operatorutils.GetResources(operatorconstants.Grafana, mgh.Spec.AdvancedConfig),
		}, nil
	})
	if err != nil {
		return fmt.Errorf("failed to render grafana manifests: %w", err)
	}

	// create restmapper for deployer to find GVR
	dc, err := discovery.NewDiscoveryClientForConfig(r.Manager.GetConfig())
	if err != nil {
		return err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	if err = r.manipulateObj(ctx, grafanaDeployer, mapper, grafanaObjects, mgh, log); err != nil {
		return fmt.Errorf("failed to create/update grafana objects: %w", err)
	}

	// generate datasource secret: must before the grafana objects
	changedDatasourceSecret, err := r.GenerateGrafanaDataSourceSecret(ctx, mgh)
	if err != nil {
		return fmt.Errorf("failed to generate grafana datasource secret: %v", err)
	}

	changedAlert, err := r.generateAlertConfigMap(ctx, mgh)
	if err != nil {
		return fmt.Errorf("failed to generate merged alert configmap. err:%v", err)
	}

	changedGrafanaIni, err := r.generateGrafanaIni(ctx, mgh)
	if err != nil {
		return fmt.Errorf("failed to generate grafana init. err:%v", err)
	}

	if changedAlert || changedGrafanaIni || changedDatasourceSecret {
		err = utils.RestartPod(ctx, r.KubeClient, utils.GetDefaultNamespace(), grafanaDeploymentName)
		if err != nil {
			return fmt.Errorf("failed to restart grafana pod. err:%v", err)
		}
	}

	log.Info("grafana objects created/updated successfully")
	return nil
}

// generateGranafaIni append the custom grafana.ini to default grafana.ini
func (r *MulticlusterGlobalHubReconciler) generateGrafanaIni(
	ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) (bool, error) {
	configNamespace := utils.GetDefaultNamespace()
	defaultGrafanaIniSecret, err := r.KubeClient.CoreV1().
		Secrets(configNamespace).
		Get(ctx,
			defaultGrafanaIniName,
			metav1.GetOptions{},
		)
	if err != nil {
		return false, fmt.Errorf(
			"failed to get default grafana.ini secret. Namespace:%v, Name:%v, Error: %w",
			configNamespace,
			defaultGrafanaIniName,
			err,
		)
	}

	customGrafanaIniSecret, err := r.KubeClient.CoreV1().
		Secrets(configNamespace).
		Get(ctx,
			constants.CustomGrafanaIniName,
			metav1.GetOptions{},
		)
	if err != nil && !errors.IsNotFound(err) {
		return false, fmt.Errorf(
			"failed to get custom grafana.ini secret. Namespace:%v, Name:%v, Error: %w",
			configNamespace,
			constants.CustomGrafanaIniName,
			err,
		)
	}

	mergedGrafanaIniSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mergedGrafanaIniName,
			Namespace: configNamespace,
			Labels: map[string]string{
				"name":                           grafanaDeploymentName,
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			},
		},
	}

	// Replace the grafana domain to grafana route url
	if r.RouteV1Client != nil {
		grafanaRoute, err := r.RouteV1Client.RouteV1().
			Routes(configNamespace).
			Get(ctx,
				grafanaDeploymentName,
				metav1.GetOptions{},
			)
		if err != nil {
			klog.Errorf("Failed to get grafana route: %v", err)
		} else {
			if grafanaRoute != nil && len(grafanaRoute.Spec.Host) != 0 {
				defaultGrafanaIniSecret.Data[grafanaIniKey] = []byte(
					strings.ReplaceAll(
						string(defaultGrafanaIniSecret.Data[grafanaIniKey]),
						"localhost",
						grafanaRoute.Spec.Host,
					),
				)
			}
		}
	}

	if customGrafanaIniSecret == nil {
		mergedGrafanaIniSecret.Data = map[string][]byte{
			grafanaIniKey: defaultGrafanaIniSecret.Data[grafanaIniKey],
		}
	} else {
		var mergedBytes []byte
		mergedBytes, err = mergeGrafanaIni(
			defaultGrafanaIniSecret.Data[grafanaIniKey],
			customGrafanaIniSecret.Data[grafanaIniKey],
		)
		if err != nil {
			klog.Errorf("Failed to merge default and custom grafana.ini, err:%v", err)
			mergedBytes = defaultGrafanaIniSecret.Data[grafanaIniKey]
		}
		mergedGrafanaIniSecret.Data = map[string][]byte{
			grafanaIniKey: mergedBytes,
		}
	}

	if err = controllerutil.SetControllerReference(mgh, mergedGrafanaIniSecret, r.Scheme); err != nil {
		return false, err
	}
	return operatorutils.ApplySecret(ctx, r.KubeClient, mergedGrafanaIniSecret)
}

// generateAlertConfigMap generate the alert configmap which grafana direclly use
// if there is no custom configmap, apply the alert configmap based on default alert configmap data.
// if there is the custom configmap, merge the custom configmap and default configmap then apply the merged configmap
func (r *MulticlusterGlobalHubReconciler) generateAlertConfigMap(
	ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) (bool, error) {
	configNamespace := utils.GetDefaultNamespace()
	defaultAlertConfigMap, err := r.KubeClient.CoreV1().
		ConfigMaps(configNamespace).
		Get(ctx,
			defaultAlertName,
			metav1.GetOptions{},
		)
	if err != nil {
		return false, fmt.Errorf("failed to get default alert configmap: %w", err)
	}

	customAlertConfigMap, err := r.KubeClient.CoreV1().
		ConfigMaps(configNamespace).
		Get(ctx,
			constants.CustomAlertName,
			metav1.GetOptions{},
		)
	if err != nil && !errors.IsNotFound(err) {
		return false, err
	}

	var mergedAlertConfigMap *corev1.ConfigMap
	mergedAlertConfigMap, err = mergeAlertConfigMap(defaultAlertConfigMap, customAlertConfigMap)
	if err != nil {
		klog.Errorf("Failed to merge custom alert configmap:%v. err: %v", customAlertConfigMap.Name, err)
		mergedAlertConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mergedAlertName,
				Namespace: configNamespace,
				Labels: map[string]string{
					constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
				},
			},
			Data: defaultAlertConfigMap.Data,
		}
	}

	// Set MGH instance as the owner and controller
	if err = controllerutil.SetControllerReference(mgh, mergedAlertConfigMap, r.Scheme); err != nil {
		return false, err
	}
	return operatorutils.ApplyConfigMap(ctx, r.KubeClient, mergedAlertConfigMap)
}

// mergeGrafanaIni merge the default grafana.ini and custom grafana.ini
// If a section exsts in both default and custom, use the default one
func mergeGrafanaIni(defaultIni, customIni []byte) ([]byte, error) {
	if len(defaultIni) == 0 {
		return customIni, nil
	}
	if len(customIni) == 0 {
		return defaultIni, nil
	}
	defaultCfg, err := ini.Load(defaultIni)
	if err != nil {
		return nil, err
	}
	customCfg, err := ini.Load(customIni)
	if err != nil {
		return nil, err
	}

	// delete sections from custom grafan.ini if the section exist in default grafana.ini
	for _, v := range customCfg.Sections() {
		if !defaultCfg.HasSection(v.Name()) {
			continue
		}
		customCfg.DeleteSection(v.Name())
	}

	var defaultBuf bytes.Buffer
	_, err = defaultCfg.WriteTo(&defaultBuf)
	if err != nil {
		return nil, err
	}

	var customBuf bytes.Buffer
	_, err = customCfg.WriteTo(&customBuf)
	if err != nil {
		return nil, err
	}
	mergedBuf := defaultBuf.String() + "\n" + customBuf.String()

	return []byte(mergedBuf), err
}

// mergeAlertYaml merge two grafana alert config file
func mergeAlertYaml(a, b []byte) ([]byte, error) {
	var o1, o2 map[string]interface{}
	if len(a) == 0 {
		return b, nil
	}
	if len(b) == 0 {
		return a, nil
	}

	if err := yaml.Unmarshal(a, &o1); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &o2); err != nil {
		return nil, err
	}

	for k, v := range o2 {
		if !supportAlertConfig.Has(k) && !supportDeleteAlertConfig.Has(k) {
			continue
		}
		o1Array, _ := o1[k].([]interface{})
		o2Array, _ := v.([]interface{})

		o1[k] = append(o1Array, o2Array...)
	}
	return yaml.Marshal(o1)
}

// mergeAlertConfigMap merge the default alert configmap and custom alert configmap
func mergeAlertConfigMap(defaultAlertConfigMap, customAlertConfigMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	if defaultAlertConfigMap == nil && customAlertConfigMap == nil {
		return nil, nil
	}

	var mergedAlertConfigMap corev1.ConfigMap
	mergedAlertConfigMap.Name = mergedAlertName
	mergedAlertConfigMap.Namespace = utils.GetDefaultNamespace()

	if defaultAlertConfigMap == nil {
		mergedAlertConfigMap.Data = customAlertConfigMap.Data
		return &mergedAlertConfigMap, nil
	}
	if customAlertConfigMap == nil {
		mergedAlertConfigMap.Data = defaultAlertConfigMap.Data
		return &mergedAlertConfigMap, nil
	}

	mergedAlertYaml, err := mergeAlertYaml(
		[]byte(defaultAlertConfigMap.Data[alertConfigMapKey]),
		[]byte(customAlertConfigMap.Data[alertConfigMapKey]))
	if err != nil {
		return nil, err
	}
	mergedAlertConfigMap.Data = map[string]string{
		alertConfigMapKey: string(mergedAlertYaml),
	}
	return &mergedAlertConfigMap, nil
}

// GenerateGrafanaDataSource is used to generate the GrafanaDatasource as a secret.
// the GrafanaDatasource points to multicluster-global-hub cr
func (r *MulticlusterGlobalHubReconciler) GenerateGrafanaDataSourceSecret(
	ctx context.Context,
	mgh *globalhubv1alpha4.MulticlusterGlobalHub,
) (bool, error) {
	if r.MiddlewareConfig == nil || r.MiddlewareConfig.StorageConn == nil {
		return false, fmt.Errorf("middleware PgConnection config is null")
	}

	saToken := ""
	if mgh.Spec.EnableMetrics {
		saSecret := &corev1.Secret{}
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      "multicluster-global-hub-grafana-sa-secret",
			Namespace: utils.GetDefaultNamespace(),
		}, saSecret)
		if err != nil {
			return false, err
		}
		saToken = string(saSecret.Data["token"])
	}

	datasourceVal, err := GrafanaDataSource(r.MiddlewareConfig.StorageConn.ReadonlyUserDatabaseURI,
		r.MiddlewareConfig.StorageConn.CACert, saToken)
	if err != nil {
		datasourceVal, err = GrafanaDataSource(r.MiddlewareConfig.StorageConn.SuperuserDatabaseURI,
			r.MiddlewareConfig.StorageConn.CACert, saToken)
		if err != nil {
			return false, err
		}
	}

	dsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      datasourceName,
			Namespace: utils.GetDefaultNamespace(),
			Labels: map[string]string{
				"datasource/time-tarted":         time.Now().Format("2006-1-2.1504"),
				constants.GlobalHubOwnerLabelKey: constants.GHOperatorOwnerLabelVal,
			},
		},
		Type: "Opaque",
		Data: map[string][]byte{
			datasourceKey: datasourceVal,
		},
	}

	// Set MGH instance as the owner and controller
	if err = controllerutil.SetControllerReference(mgh, dsSecret, r.Scheme); err != nil {
		return false, err
	}

	// Check if this already exists
	grafanaDSFound := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      dsSecret.Name,
		Namespace: dsSecret.Namespace,
	}, grafanaDSFound)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Client.Create(ctx, dsSecret)
			return true, err
		}
		return false, err
	}

	if !bytes.Equal(grafanaDSFound.Data[datasourceKey], datasourceVal) {
		grafanaDSFound.Data[datasourceKey] = datasourceVal
		err = r.Client.Update(ctx, grafanaDSFound)
		return true, err
	}
	return false, nil
}

func GrafanaDataSource(databaseURI string, cert []byte, serviceAccountToken string) ([]byte, error) {
	postgresURI := string(databaseURI)
	objURI, err := url.Parse(postgresURI)
	if err != nil {
		return nil, err
	}
	password, ok := objURI.User.Password()
	if !ok {
		return nil, fmt.Errorf("failed to get password from database_uri: %s", postgresURI)
	}

	// get the database from the objURI
	database := "hoh"
	paths := strings.Split(objURI.Path, "/")
	if len(paths) > 1 {
		database = paths[1]
	}

	postgresDS := &GrafanaDatasource{
		Name:      "Global-Hub-DataSource",
		Type:      "postgres",
		Access:    "proxy",
		IsDefault: true,
		URL:       objURI.Host,
		User:      objURI.User.Username(),
		Database:  database,
		Editable:  false,
		JSONData: &JsonData{
			QueryTimeout: "300s",
			TimeInterval: "30s",
		},
		SecureJSONData: &SecureJsonData{
			Password: password,
		},
	}
	datasource := []*GrafanaDatasource{postgresDS}

	if serviceAccountToken != "" {
		prometheusDS := &GrafanaDatasource{
			Name:      "Prometheus",
			Type:      "prometheus",
			Access:    "proxy",
			IsDefault: false,
			URL:       "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091",
			BasicAuth: false,
			Editable:  false,
			JSONData: &JsonData{
				QueryTimeout:    "300s",
				TimeInterval:    "30s",
				TLSSkipVerify:   true,
				HttpHeaderName1: "Authorization",
			},
			SecureJSONData: &SecureJsonData{
				HttpHeaderValue1: "Bearer " + serviceAccountToken,
			},
		}
		datasource = append(datasource, prometheusDS)
	}

	if len(cert) > 0 {
		postgresDS.JSONData.SSLMode = objURI.Query().Get("sslmode") // sslmode == "verify-full" || sslmode == "verify-ca"
		postgresDS.JSONData.TLSAuth = true
		postgresDS.JSONData.TLSAuthWithCACert = true
		postgresDS.JSONData.TLSSkipVerify = true
		postgresDS.JSONData.TLSConfigurationMethod = "file-content"
		postgresDS.SecureJSONData.TLSCACert = string(cert)
	}
	grafanaDatasources, err := yaml.Marshal(GrafanaDatasources{
		APIVersion:  1,
		Datasources: datasource,
	})
	if err != nil {
		return nil, err
	}
	return grafanaDatasources, nil
}

type GrafanaDatasources struct {
	APIVersion  int                  `yaml:"apiVersion,omitempty"`
	Datasources []*GrafanaDatasource `yaml:"datasources,omitempty"`
}

type GrafanaDatasource struct {
	Access            string          `yaml:"access,omitempty"`
	BasicAuth         bool            `yaml:"basicAuth,omitempty"`
	BasicAuthPassword string          `yaml:"basicAuthPassword,omitempty"`
	BasicAuthUser     string          `yaml:"basicAuthUser,omitempty"`
	Editable          bool            `yaml:"editable,omitempty"`
	IsDefault         bool            `yaml:"isDefault,omitempty"`
	Name              string          `yaml:"name,omitempty"`
	OrgID             int             `yaml:"orgId,omitempty"`
	Type              string          `yaml:"type,omitempty"`
	URL               string          `yaml:"url,omitempty"`
	Database          string          `yaml:"database,omitempty"`
	User              string          `yaml:"user,omitempty"`
	Version           int             `yaml:"version,omitempty"`
	JSONData          *JsonData       `yaml:"jsonData,omitempty"`
	SecureJSONData    *SecureJsonData `yaml:"secureJsonData,omitempty"`
}

type JsonData struct {
	SSLMode                string `yaml:"sslmode,omitempty"`
	TLSAuth                bool   `yaml:"tlsAuth,omitempty"`
	TLSAuthWithCACert      bool   `yaml:"tlsAuthWithCACert,omitempty"`
	TLSConfigurationMethod string `yaml:"tlsConfigurationMethod,omitempty"`
	TLSSkipVerify          bool   `yaml:"tlsSkipVerify,omitempty"`
	QueryTimeout           string `yaml:"queryTimeout,omitempty"`
	HttpMethod             string `yaml:"httpMethod,omitempty"`
	TimeInterval           string `yaml:"timeInterval,omitempty"`
	HttpHeaderName1        string `yaml:"httpHeaderName1,omitempty"`
}

type SecureJsonData struct {
	Password         string `yaml:"password,omitempty"`
	TLSCACert        string `yaml:"tlsCACert,omitempty"`
	TLSClientCert    string `yaml:"tlsClientCert,omitempty"`
	TLSClientKey     string `yaml:"tlsClientKey,omitempty"`
	HttpHeaderValue1 string `yaml:"httpHeaderValue1,omitempty"`
}
