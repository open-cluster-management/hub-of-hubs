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

	"github.com/jackc/pgx/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	agentscheme "github.com/stolostron/multicluster-global-hub/agent/pkg/scheme"
	managerscheme "github.com/stolostron/multicluster-global-hub/manager/pkg/scheme"
	"github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha4"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/config"
	"github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	operatorutils "github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/database"
	commonutils "github.com/stolostron/multicluster-global-hub/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/test/pkg/kustomize"
	"github.com/stolostron/multicluster-global-hub/test/pkg/utils"
)

var (
	rootDir     string
	optionsFile string
	testOptions utils.Options
	testTimeout time.Duration

	testClients utils.TestClient
	httpClient  *http.Client

	leafHubNames    []string
	managedClusters []clusterv1.ManagedCluster

	operatorScheme *runtime.Scheme
	managerScheme  *runtime.Scheme
	agentScheme    *runtime.Scheme

	postgresConn *pgx.Conn
	db           *gorm.DB
	ctx          context.Context
	cancel       context.CancelFunc
)

const (
	ExpectedMH              = 2
	ExpectedMC              = 2
	Namespace               = "multicluster-global-hub"
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
	operatorScheme = config.GetRuntimeScheme()
	agentScheme = runtime.NewScheme()
	agentscheme.AddToScheme(agentScheme)
	managerScheme = runtime.NewScheme()
	managerscheme.AddToScheme(managerScheme)

	By("Complete the options and init clients")
	testOptions = completeOptions()
	testClients = utils.NewTestClient(testOptions)
	httpClient = testClients.HttpClient()

	By("Init postgres connection")
	databaseURI := strings.Split(testOptions.GlobalHub.DatabaseURI, "?")[0]
	var err error
	postgresConn, err = database.PostgresConnection(ctx, databaseURI, nil)
	Expect(err).Should(Succeed())
	err = database.InitGormInstance(&database.DatabaseConfig{
		URL:      strings.Replace(testOptions.GlobalHub.DatabaseURI, "sslmode=verify-ca", "sslmode=require", -1),
		Dialect:  database.PostgresDialect,
		PoolSize: 5,
	})
	Expect(err).Should(Succeed())
	db = database.GetGorm()

	By("Deploy the global hub")
	deployGlobalHub()

	By("Get the managed clusters")
	Eventually(func() (err error) {
		managedClusters, err = getManagedCluster(httpClient)
		return err
	}, 3*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	Expect(len(managedClusters)).Should(Equal(ExpectedMC))
})

var _ = AfterSuite(func() {
	cancel()
	err := postgresConn.Close(ctx)
	Expect(err).Should(Succeed())
	utils.DeleteTestingRBAC(testOptions)
})

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

	testOptions = testOptionsContainer.Options
	if testOptions.GlobalHub.KubeConfig == "" {
		testOptions.GlobalHub.KubeConfig = os.Getenv("KUBECONFIG")
	}

	s, _ := json.MarshalIndent(testOptionsContainer, "", "  ")
	klog.V(6).Infof("OptionsContainer %s", s)
	for _, cluster := range testOptions.GlobalHub.ManagedHubs {
		leafHubNames = append(leafHubNames, cluster.Name)
	}

	Expect(len(leafHubNames)).Should(Equal(ExpectedMH))
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

	Expect(kustomize.Apply(testClients, testOptions,
		kustomize.Options{KustomizationPath: fmt.Sprintf("%s/test/pkg/e2e/resources", rootDir)})).NotTo(HaveOccurred())
	Expect(kustomize.Apply(testClients, testOptions,
		kustomize.Options{KustomizationPath: fmt.Sprintf("%s/operator/config/default", rootDir)})).NotTo(HaveOccurred())

	By("Deploying operand")
	mcgh := &v1alpha4.MulticlusterGlobalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multiclusterglobalhub",
			Namespace: "multicluster-global-hub",
			Annotations: map[string]string{
				constants.AnnotationMGHSkipAuth: "true",
				"mgh-scheduler-interval":        "minute",
			},
		},
		Spec: v1alpha4.MulticlusterGlobalHubSpec{
			// Disable metrics in e2e
			EnableMetrics: false,
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
		return patchGHDeployment(runtimeClient, Namespace, "multicluster-global-hub-operator")
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
			err := checkDeployAvailable(runtimeClient, Namespace, name)
			if err != nil {
				components[name] += 1
				// restart it if the blocking time exceeds 30 seconds
				if count > 30 {
					_ = commonutils.RestartPod(ctx, testClients.KubeClient(), Namespace, name)
					components[name] = 0
				}
				return err
			}
		}
		return nil
	}, 5*time.Minute, 1*time.Second).Should(Succeed())

	// Before run test, the mgh should be ready
	_, err = operatorutils.WaitGlobalHubReady(ctx, runtimeClient, 5*time.Second)
	Expect(err).ShouldNot(HaveOccurred())
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
