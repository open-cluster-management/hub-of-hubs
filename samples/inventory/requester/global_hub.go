package main

import (
	"context"
	"fmt"

	kessel "github.com/project-kessel/inventory-api/api/kessel/inventory/v1beta1/resources"
	clusterinfov1beta1 "github.com/stolostron/cluster-lifecycle-api/clusterinfo/v1beta1"
	"github.com/stolostron/multicluster-global-hub/agent/pkg/status/controller/managedclusters"
	transportconfig "github.com/stolostron/multicluster-global-hub/pkg/transport/config"
	"github.com/stolostron/multicluster-global-hub/pkg/transport/requester"
	"github.com/stolostron/multicluster-global-hub/samples/config"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// export SECRET_NAMESPACE=multicluster-global-hub
// export SECRET_NAME=transport-config-inventory-guest
// ./test/script/event_exporter_inventory.sh
func globalHub(ctx context.Context) error {
	transportConfigSecret, err := config.GetTransportConfigSecret("multicluster-global-hub",
		"transport-config-inventory-guest")
	if err != nil {
		return err
	}

	c, err := getRuntimeClient()
	if err != nil {
		return err
	}
	restfulConn, err := transportconfig.GetRestfulConnBySecret(transportConfigSecret, c)
	if err != nil {
		return err
	}
	// utils.PrettyPrint(restfulConn)

	requesterClient, err := requester.NewInventoryClient(ctx, restfulConn)
	if err != nil {
		return err
	}

	clusterInfo := createMockClusterInfo("local-cluster")
	k8sCluster := managedclusters.GetK8SCluster(clusterInfo, "guest")
	createResp, err := requesterClient.GetHttpClient().K8sClusterService.CreateK8SCluster(ctx,
		&kessel.CreateK8SClusterRequest{K8SCluster: k8sCluster})
	if err != nil {
		return err
	}
	fmt.Println("creating response", createResp)

	clusterInfo = createMockClusterInfo("local-cluster-updating")
	k8sCluster = managedclusters.GetK8SCluster(clusterInfo, "guest")
	updatingResponse, err := requesterClient.GetHttpClient().K8sClusterService.UpdateK8SCluster(ctx,
		&kessel.UpdateK8SClusterRequest{K8SCluster: k8sCluster})
	if err != nil {
		return err
	}
	fmt.Println("updating response", updatingResponse)

	clusterInfo = createMockClusterInfo("local-cluster")
	k8sCluster = managedclusters.GetK8SCluster(clusterInfo, "guest")
	deletingResponse, err := requesterClient.GetHttpClient().K8sClusterService.DeleteK8SCluster(ctx,
		&kessel.DeleteK8SClusterRequest{ReporterData: k8sCluster.ReporterData})
	if err != nil {
		return err
	}
	fmt.Println("deleting response", deletingResponse)

	return nil
}

func createMockClusterInfo(name string) *clusterinfov1beta1.ManagedClusterInfo {
	clusterInfo := &clusterinfov1beta1.ManagedClusterInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
		Spec: clusterinfov1beta1.ClusterInfoSpec{
			MasterEndpoint: "https://api.test-cluster.example.com",
		},
		Status: clusterinfov1beta1.ClusterInfoStatus{
			ClusterID:   "test-cluster-id",
			Version:     "1.23.0",
			ConsoleURL:  "https://console.test-cluster.example.com",
			CloudVendor: "Amazon",
			KubeVendor:  "OpenShift",
			Conditions: []metav1.Condition{
				{
					Type:   clusterv1.ManagedClusterConditionAvailable,
					Status: metav1.ConditionTrue,
				},
			},
			DistributionInfo: clusterinfov1beta1.DistributionInfo{
				OCP: clusterinfov1beta1.OCPDistributionInfo{
					Version: "4.15.24",
				},
			},
			NodeList: []clusterinfov1beta1.NodeStatus{
				{
					Name: "ip-10-0-14-217.ec2.internal",
					Capacity: clusterinfov1beta1.ResourceList{
						clusterv1.ResourceCPU:    resource.MustParse("16"),
						clusterv1.ResourceMemory: resource.MustParse("64453796Ki"),
					},
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "m6a.4xlarge",
					},
				},
			},
		},
	}

	return clusterInfo
}
