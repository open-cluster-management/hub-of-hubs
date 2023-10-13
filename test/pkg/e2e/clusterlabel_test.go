package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	CLUSTER_LABEL_KEY   = "label"
	CLUSTER_LABEL_VALUE = "test"
)

var _ = Describe("Updating cluster label from HoH manager", Label("e2e-tests-label"), Ordered, func() {
	It("add the label to the managed cluster", func() {
		patches := []patch{
			{
				Op:    "add", // or remove
				Path:  "/metadata/labels/" + CLUSTER_LABEL_KEY,
				Value: CLUSTER_LABEL_VALUE,
			},
		}

		By("Check the label is added")
		Eventually(func() error {
			err := updateClusterLabelByAPI(httpClient, patches, GetClusterID(managedClusters[0]))
			if err != nil {
				return err
			}
			managedClusterInfo, err := getManagedClusterByName(httpClient, managedClusters[0].Name)
			if err != nil {
				return err
			}
			if val, ok := managedClusterInfo.Labels[CLUSTER_LABEL_KEY]; ok {
				if val == CLUSTER_LABEL_VALUE {
					return nil
				}
			}
			return fmt.Errorf("the label [%s: %s] is not exist", CLUSTER_LABEL_KEY, CLUSTER_LABEL_VALUE)
		}, 3*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("add the label to the managed cluster", func() {
		for i := 1; i < len(managedClusters); i++ {
			patches := []patch{
				{
					Op:    "add", // or remove
					Path:  "/metadata/labels/" + CLUSTER_LABEL_KEY,
					Value: CLUSTER_LABEL_VALUE,
				},
			}

			By("Check the label is added")
			Eventually(func() error {
				err := updateClusterLabelByAPI(httpClient, patches, GetClusterID(managedClusters[i]))
				if err != nil {
					return err
				}
				managedClusterInfo, err := getManagedClusterByName(httpClient, managedClusters[i].Name)
				if err != nil {
					return err
				}
				if val, ok := managedClusterInfo.Labels[CLUSTER_LABEL_KEY]; ok {
					if val == CLUSTER_LABEL_VALUE {
						return nil
					}
				}
				return fmt.Errorf("the label [%s: %s] is not exist", CLUSTER_LABEL_KEY, CLUSTER_LABEL_VALUE)
			}, 3*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		}
	})

	It("remove the label from the managed cluster", func() {
		for _, managedCluster := range managedClusters {
			patches := []patch{
				{
					Op:    "remove",
					Path:  "/metadata/labels/" + CLUSTER_LABEL_KEY,
					Value: CLUSTER_LABEL_VALUE,
				},
			}

			By("Check the label is deleted")
			Eventually(func() error {
				err := updateClusterLabelByAPI(httpClient, patches, GetClusterID(managedCluster))
				if err != nil {
					return err
				}
				managedClusterInfo, err := getManagedClusterByName(httpClient, managedCluster.Name)
				if err != nil {
					return err
				}

				if val, ok := managedClusterInfo.Labels[CLUSTER_LABEL_KEY]; ok {
					if val == CLUSTER_LABEL_VALUE {
						return fmt.Errorf("the label %s: %s should not be exist", CLUSTER_LABEL_KEY, CLUSTER_LABEL_VALUE)
					}
				}
				return nil
			}, 3*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		}
	})
})

type patch struct {
	Op    string `json:"op" binding:"required"`
	Path  string `json:"path" binding:"required"`
	Value string `json:"value"`
}

func getManagedCluster(client *http.Client) ([]clusterv1.ManagedCluster, error) {
	managedClusterUrl := fmt.Sprintf("%s/global-hub-api/v1/managedclusters", testOptions.GlobalHub.Nonk8sApiServer)
	req, err := http.NewRequest("GET", managedClusterUrl, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var managedClusterList clusterv1.ManagedClusterList
	err = json.Unmarshal(body, &managedClusterList)
	if err != nil {
		return nil, err
	}

	if len(managedClusterList.Items) != ExpectedManagedClusterNum {
		return nil, fmt.Errorf("cannot get two managed clusters")
	}

	return managedClusterList.Items, nil
}

func getManagedClusterByName(client *http.Client, managedClusterName string) (
	*clusterv1.ManagedCluster, error,
) {
	managedClusterUrl := fmt.Sprintf("%s/global-hub-api/v1/managedclusters",
		testOptions.GlobalHub.Nonk8sApiServer)
	req, err := http.NewRequest("GET", managedClusterUrl, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var managedClusterList clusterv1.ManagedClusterList
	err = json.Unmarshal(body, &managedClusterList)
	if err != nil {
		return nil, err
	}

	for _, managedCluster := range managedClusterList.Items {
		if managedCluster.Name == managedClusterName {
			return &managedCluster, nil
		}
	}

	return nil, nil
}

func updateClusterLabelByAPI(client *http.Client, patches []patch, managedClusterID string) error {
	updateLabelUrl := fmt.Sprintf("%s/global-hub-api/v1/managedcluster/%s",
		testOptions.GlobalHub.Nonk8sApiServer, managedClusterID)
	// set method and body
	jsonBody, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH", updateLabelUrl, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	// add header
	req.Header.Add("Accept", "application/json")

	// do request
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func updateClusterLabel(managedClusterName, labelStr string) error {
	leafhubName, _ := strings.CutSuffix(managedClusterName, "-cluster1")
	_, err := testClients.Kubectl(leafhubName, "label", "managedcluster", managedClusterName, labelStr)
	if err != nil {
		return err
	}
	return nil
}
