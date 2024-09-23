package tests

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	agentconfig "github.com/stolostron/multicluster-global-hub/agent/pkg/config"
	managerconfig "github.com/stolostron/multicluster-global-hub/manager/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	operatorconfig "github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	operatorutils "github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	commonutils "github.com/stolostron/multicluster-global-hub/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/test/e2e/utils"
)

var (
	rootDir     string
	optionsFile string
	testOptions utils.Options
	testTimeout time.Duration

	testClients utils.TestClient
	httpClient  *http.Client

	globalHubClient client.Client
	managedHubNames []string
	hubClients      []client.Client
	managedClusters []clusterv1.ManagedCluster
	clusterClients  []client.Client

	operatorScheme        *runtime.Scheme
	managerScheme         *runtime.Scheme
	agentScheme           *runtime.Scheme
	expectComponentsCount int
	db                    *gorm.DB
	ctx                   context.Context
	cancel                context.CancelFunc
	isPrune               string
	isBYO                 string
)

const (
	ExpectedMH              = 2
	ExpectedMC              = 1
	GlobalhubNamespace      = "multicluster-global-hub"
	MghName                 = "multiclusterglobalhub"
	ServiceMonitorNamespace = "openshift-monitoring"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Suite")
}

func init() {
	// This line prevents controller-runtime from complaining about log.SetLogger never being called
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	klog.SetOutput(GinkgoWriter)
	klog.InitFlags(nil)
	flag.StringVar(&optionsFile, "options", "", "Location of an \"options.yaml\" file to provide input for various tests")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())
	By("Init schemes")
	operatorScheme = operatorconfig.GetRuntimeScheme()
	agentScheme = agentconfig.GetRuntimeScheme()
	managerScheme = managerconfig.GetRuntimeScheme()
	isBYO = os.Getenv("ISBYO")
	klog.Infof("Isbyo: %v", isBYO)
	if isBYO == "true" {
		expectComponentsCount = 2
	} else {
		expectComponentsCount = 4
	}
	isPrune = os.Getenv("ISPRUNE")
	klog.Infof("isPrune: %v", isPrune)

	By("Complete the options and init clients")
	testOptions = completeOptions()
	testClients = utils.NewTestClient(testOptions)
	httpClient = testClients.HttpClient()
	// valid the clients
	deployClient := testClients.KubeClient().AppsV1().Deployments(testOptions.GlobalHub.Namespace)
	_, err := deployClient.List(ctx, metav1.ListOptions{Limit: 2})
	Expect(err).ShouldNot(HaveOccurred())
	// Expect(len(deployList.Items) > 0).To(BeTrue())
	// valid the global hub cluster apiserver
	healthy, err := testClients.KubeClient().Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(ctx)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(string(healthy)).To(Equal("ok"))

	By("Deploy the global hub")
	deployGlobalHub()

	By("Validate the opitions")
	globalHubClient, err = testClients.RuntimeClient(testOptions.GlobalHub.Name, operatorScheme)
	Expect(err).To(Succeed())
	var clusterNames []string
	for _, hub := range testOptions.GlobalHub.ManagedHubs {
		managedHubNames = append(managedHubNames, hub.Name)
		// it will validate the kubeconfig
		hubClient, err := testClients.RuntimeClient(hub.Name, agentScheme)
		Expect(err).To(Succeed())
		hubClients = append(hubClients, hubClient)
		for _, cluster := range hub.ManagedClusters {
			clusterNames = append(clusterNames, cluster.Name)
			clusterClient, err := testClients.RuntimeClient(cluster.Name, operatorScheme)
			Expect(err).To(Succeed())
			clusterClients = append(clusterClients, clusterClient)
		}
	}
	Expect(len(managedHubNames)).To(Equal(ExpectedMH))
	Expect(len(clusterNames)).To(Equal(ExpectedMC * ExpectedMH))

	if isPrune != "true" {
		By("Init postgres connection")
		if isBYO == "true" {
			databaseBYOSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).
				Get(ctx, "multicluster-global-hub-storage", metav1.GetOptions{})
			Expect(err).Should(Succeed())

			err = database.InitGormInstance(&database.DatabaseConfig{
				URL:      strings.Replace(string(databaseBYOSecret.Data["database_uri"]), "sslmode=verify-ca", "sslmode=require", -1),
				Dialect:  database.PostgresDialect,
				PoolSize: 5,
			})
			Expect(err).Should(Succeed())
		} else {
			err = createPostgresService(testOptions.GlobalHub.Namespace)
			Expect(err).Should(Succeed())
			time.Sleep(1 * time.Second)
			databaseDefaultCaConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).
				Get(ctx, "multicluster-global-hub-postgres-ca", metav1.GetOptions{})
			Expect(err).Should(Succeed())
			caPath := "postgres.ca"
			klog.Infof("postgres ca path: %v", caPath)

			err = writeFile([]byte(databaseDefaultCaConfigMap.Data["service-ca.crt"]), caPath)
			Expect(err).Should(Succeed())

			databaseDefaultSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).
				Get(ctx, "multicluster-global-hub-postgres", metav1.GetOptions{})
			Expect(err).Should(Succeed())
			globalhubIp, err := getIP(testOptions.GlobalHub.ApiServer)
			Expect(err).Should(Succeed())
			superuserDatabaseURI := "postgresql://postgres" + ":" +
				string(databaseDefaultSecret.Data["database-admin-password"]) + "@" + globalhubIp +
				":32433/hoh?sslmode=verify-ca"
			Eventually(func() (err error) {
				err = database.InitGormInstance(&database.DatabaseConfig{
					URL:        superuserDatabaseURI,
					Dialect:    database.PostgresDialect,
					PoolSize:   5,
					CaCertPath: caPath,
				})
				return err
			}, 1*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())
		}
		db = database.GetGorm()
	}
	By("Validate the clusters on database")
	Eventually(func() (err error) {
		managedClusters, err = getManagedCluster(httpClient)
		klog.Errorf("get managedcluster error:%v", err)
		return err
	}, 6*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())
	Expect(len(managedClusters)).Should(Equal(ExpectedMC * ExpectedMH))
})

var _ = AfterSuite(func() {
	cancel()
	utils.DeleteTestingRBAC(testOptions)
})

func getIP(apiserver string) (string, error) {
	splitapi := strings.Split(apiserver, ":")
	if len(splitapi) != 3 {
		return "", fmt.Errorf("apiserver is not right")
	}
	return splitapi[1][2:], nil
}

func completeOptions() utils.Options {
	testTimeout = time.Second * 30

	// get project rootdir path
	exePath, err := os.Executable()
	Expect(err).NotTo(HaveOccurred())

	exeDir := filepath.Dir(exePath)
	rootDir, err = findRootDir(exeDir)
	Expect(err).NotTo(HaveOccurred())

	klog.V(6).Infof("Options Path: %s", optionsFile)
	data, err := os.ReadFile(optionsFile)
	Expect(err).NotTo(HaveOccurred())

	testOptionsContainer := &utils.OptionsContainer{}
	err = yaml.UnmarshalStrict([]byte(data), testOptionsContainer)
	Expect(err).NotTo(HaveOccurred())

	s, _ := json.MarshalIndent(testOptionsContainer, "", "  ")
	klog.V(6).Infof("OptionsContainer %s", s)

	testOptions = testOptionsContainer.Options
	if testOptions.GlobalHub.KubeConfig == "" {
		testOptions.GlobalHub.KubeConfig = os.Getenv("KUBECONFIG")
	}
	return testOptions
}

func GetClusterID(cluster clusterv1.ManagedCluster) string {
	for _, claim := range cluster.Status.ClusterClaims {
		if claim.Name == "id.k8s.io" {
			return claim.Value
		}
	}
	return ""
}

// Traverse directories upwards until a directory containing go.mod is found.
func findRootDir(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("rootDir cannot find")
		}

		dir = filepath.Dir(dir)
	}
}

func deployGlobalHub() {
	By("Creating namespace for the ServiceMonitor")
	_, err := testClients.KubeClient().CoreV1().Namespaces().Get(ctx, ServiceMonitorNamespace,
		metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		_, err = testClients.KubeClient().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ServiceMonitorNamespace,
			},
		}, metav1.CreateOptions{})
	}
	Expect(err).NotTo(HaveOccurred())

	By("Creating namespace for the multicluster global hub")
	_, err = testClients.KubeClient().CoreV1().Namespaces().Get(ctx,
		commonutils.GetDefaultNamespace(), metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		_, err = testClients.KubeClient().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: commonutils.GetDefaultNamespace(),
			},
		}, metav1.CreateOptions{})
	}
	Expect(err).NotTo(HaveOccurred())

	Expect(utils.Apply(testClients, testOptions,
		utils.RenderOptions{KustomizationPath: fmt.Sprintf("%s/test/manifest/resources", rootDir)})).NotTo(HaveOccurred())
	Expect(utils.Apply(testClients, testOptions,
		utils.RenderOptions{KustomizationPath: fmt.Sprintf("%s/operator/config/default", rootDir)})).NotTo(HaveOccurred())

	By("Deploying operand")
	mcgh := &v1alpha4.MulticlusterGlobalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MghName,
			Namespace: GlobalhubNamespace,
			Annotations: map[string]string{
				constants.AnnotationMGHSkipAuth:                                  "true",
				"mgh-scheduler-interval":                                         "minute",
				"global-hub.open-cluster-management.io/catalog-source-name":      "operatorhubio-catalog",
				"global-hub.open-cluster-management.io/catalog-source-namespace": "olm",
				"global-hub.open-cluster-management.io/enable-kraft":             "",
			},
		},
		Spec: v1alpha4.MulticlusterGlobalHubSpec{
			// Disable metrics in e2e
			EnableMetrics:   false,
			ImagePullPolicy: corev1.PullIfNotPresent,
			// the topic partition replicas(depend on the HA) should less than broker replicas
			AvailabilityConfig: v1alpha4.HABasic,
			DataLayer: v1alpha4.DataLayerConfig{
				Kafka: v1alpha4.KafkaConfig{},
				Postgres: v1alpha4.PostgresConfig{
					Retention: "18m",
				},
			},
		},
	}

	runtimeClient, err := testClients.RuntimeClient(testOptions.GlobalHub.Name, operatorScheme)
	Expect(err).ShouldNot(HaveOccurred())

	// patch global hub operator to enable global resources
	Eventually(func() error {
		return patchGHDeployment(runtimeClient, GlobalhubNamespace, "multicluster-global-hub-operator")
	}, 1*time.Minute, 1*time.Second).Should(Succeed())

	err = runtimeClient.Create(ctx, mcgh)
	if !errors.IsAlreadyExists(err) {
		Expect(err).ShouldNot(HaveOccurred())
	}

	By("Verifying the multicluster-global-hub-grafana/manager")
	components := map[string]int{}
	components["multicluster-global-hub-operator"] = 0
	components["multicluster-global-hub-manager"] = 0
	components["multicluster-global-hub-grafana"] = 0
	Eventually(func() error {
		for name, count := range components {
			err := checkDeployAvailable(runtimeClient, GlobalhubNamespace, name)
			if err != nil {
				components[name] += 1
				// restart it if the blocking time exceeds 30 seconds
				if count > 120 {
					_ = commonutils.RestartPod(ctx, testClients.KubeClient(), GlobalhubNamespace, name)
					components[name] = 0
				}
				return err
			}
		}
		return nil
	}, 5*time.Minute, 1*time.Second).Should(Succeed())

	if isPrune != "true" {
		// check components avaibable and phase
		Eventually(func() error {
			return checkComponentsAvailableAndPhase(runtimeClient)
		}, 2*time.Minute, 1*time.Second).Should(Succeed())
		Expect(err).ShouldNot(HaveOccurred())
	}

	// Before run test, the mgh should be ready
	operatorconfig.SetMGHNamespacedName(types.NamespacedName{Namespace: mcgh.Namespace, Name: mcgh.Name})
	_, err = operatorutils.WaitGlobalHubReady(ctx, runtimeClient, 5*time.Second)
	Expect(err).ShouldNot(HaveOccurred())
}

func createPostgresService(ns string) error {
	externalPostServiceName := "multicluster-global-hub-postgres-external"
	postgresService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalPostServiceName,
			Namespace: ns,
			Labels: map[string]string{
				"name":    externalPostServiceName,
				"service": "multicluster-global-hub-postgres-external",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "external",
					NodePort:   32433,
					TargetPort: intstr.FromInt(5432),
					Port:       32433,
				},
			},
			Selector: map[string]string{
				"name": "multicluster-global-hub-postgres",
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	_, err := testClients.KubeClient().CoreV1().Services(ns).Get(ctx, externalPostServiceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err := testClients.KubeClient().CoreV1().Services(ns).Create(ctx, postgresService, metav1.CreateOptions{})
			return err
		}
		return err
	}
	_, err = testClients.KubeClient().CoreV1().Services(ns).Update(ctx, postgresService, metav1.UpdateOptions{})
	return err
}

func checkDeployAvailable(runtimeClient client.Client, namespace, name string) error {
	deployment := &appsv1.Deployment{}
	err := runtimeClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, deployment)
	if err != nil {
		return err
	}
	if deployment.Status.AvailableReplicas > 0 {
		return nil
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		fmt.Printf("deployment image: %s/%s: %s\n", deployment.Name, container.Name, container.Image)
	}
	return fmt.Errorf("deployment: %s is not ready", deployment.Name)
}

func checkComponentsAvailableAndPhase(runtimeClient client.Client) error {
	mgh := &v1alpha4.MulticlusterGlobalHub{}
	err := runtimeClient.Get(ctx, client.ObjectKey{
		Namespace: GlobalhubNamespace,
		Name:      MghName,
	}, mgh)
	if err != nil {
		return err
	}
	if len(mgh.Status.Components) != expectComponentsCount {
		return fmt.Errorf("expected components is %v, but current components: %v", expectComponentsCount, len(mgh.Status.Components))
	}
	for _, v := range mgh.Status.Components {
		if v.Status != metav1.ConditionTrue {
			return fmt.Errorf("component %v is not available.", v)
		}
	}
	if mgh.Status.Phase != v1alpha4.GlobalHubRunning {
		return fmt.Errorf("expected mgh status running, but got: %v", mgh.Status.Phase)
	}
	return nil
}

func patchGHDeployment(runtimeClient client.Client, namespace, name string) error {
	deployment := &appsv1.Deployment{}
	err := runtimeClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, deployment)
	if err != nil {
		return err
	}
	args := deployment.Spec.Template.Spec.Containers[0].Args
	deployment.Spec.Template.Spec.Containers[0].Args = append(args, "--global-resource-enabled=true")
	return runtimeClient.Update(ctx, deployment)
}

func writeFile(bytes []byte, file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	// remember to close the file
	defer f.Close()

	// write bytes to the file
	_, err = f.Write(bytes)
	if err != nil {
		return err
	}
	return nil
}
