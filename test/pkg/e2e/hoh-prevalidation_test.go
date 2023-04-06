package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"


	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/stolostron/multicluster-global-hub/test/pkg/utils"
)

var _ = Describe("Check all the connection of clients and necessary parameter validation", Label("e2e-tests-prevalidation"), func() {
	Context("Check all the clients could connect to the HoH servers", func() {
		It("connect to the apiserver with kubernetes interface", func() {
			hubClient := clients.KubeClient()
			deployClient := hubClient.AppsV1().Deployments(testOptions.HubCluster.Namespace)
			deployList, err := deployClient.List(context.TODO(), metav1.ListOptions{Limit: 2})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(deployList.Items) > 0).To(BeTrue())
		})

		It("connect to the apiserver with dynamic interface", func() {
			dynamicClient := clients.KubeDynamicClient()
			hohConfigMapGVR := utils.NewHoHConfigMapGVR()
			configMapList, err := dynamicClient.Resource(hohConfigMapGVR).Namespace(
				"open-cluster-management-global-hub-system").List(context.TODO(), metav1.ListOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(configMapList.Items) > 0).To(BeTrue())
		})

		It("check whether the cluster is running properly", func() {
			hubClient := clients.KubeClient()
			healthy, err := hubClient.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(context.TODO())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(string(healthy)).To(Equal("ok"))
		})

		It("connect to the nonk8s-server with specific user", func() {
			identityUrl := testOptions.HubCluster.ApiServer + "/apis/user.openshift.io/v1/users/~"

			req, err := http.NewRequest("GET", identityUrl, nil)
			Expect(err).ShouldNot(HaveOccurred())
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", httpToken))

			resp, err := httpClient.Do(req)
			Expect(err).ShouldNot(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			Expect(err).ShouldNot(HaveOccurred())

			var result map[string]interface{}
			json.Unmarshal(body, &result)
			userRes, _ := json.MarshalIndent(result, "", "  ")
			klog.V(6).Info(fmt.Sprintf("The Test User Infomation: %s", userRes))
			users := [2]string{"kube:admin", "system:masters"}
			Expect(users).To(ContainElement(result["metadata"].(map[string]interface{})["name"].(string)))
		})
	})

	Context("Check all the parameters for e2e-tests", func() {	
		var HubClusterNum = 2
		var ManagedClusterNum = 1
		It("Check the num of hub clusters and managed clusters on options-local.yaml", func() {
			opt := testOptions.ManagedClusters
			var hubClusters []string
			var managedClusters []string

			Eventually(func() error {
				for _, c := range opt {
					if c.Name == c.LeafHubName {
						hubClusters = append(hubClusters, c.Name)
					} else {
						managedClusters = append(managedClusters, c.Name)
					}
				}
				
				if len(hubClusters) != HubClusterNum || len(managedClusters) != HubClusterNum*ManagedClusterNum {
					return fmt.Errorf("generate %d hub cluster and %d managed cluster error", HubClusterNum, ManagedClusterNum)
				}
				return nil
			}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		})

		It("Check the num of hub clusters and managed clusters in the kubeconfig", func() {
			Eventually(func() error {
				for i := 1; i <= HubClusterNum; i++ {
					hubFileName := fmt.Sprintf("../../resources/kubeconfig/kubeconfig-hub%d", i)
					_, err := os.Stat(hubFileName)
					if os.IsNotExist(err) {
						return fmt.Errorf("kubeconfig-hub%d is not exist", i)
					}
					for j := 1; j <= ManagedClusterNum; j++ {
						managedFileName := fmt.Sprintf("../../resources/kubeconfig/kubeconfig-hub%d-cluster%d", i, j)
						_, err := os.Stat(managedFileName)
						if os.IsNotExist(err) {
							return fmt.Errorf("kubeconfig-hub%d-cluster%d is not exist", i, j)
						}
					}
				}
				return nil
			}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		})
	})
})
