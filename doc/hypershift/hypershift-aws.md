# Provision HyperShift Hosted Cluster on AWS

This document is used to provision HyperShift hosted cluster on AWS platform with hypershift-addon in Red Hat Advanced Cluster Management for Kubernetes version 2.5 or later.

## Prerequisites

1. Set the following environment variables:

```
export AWS_ACCESS_KEY_ID=<aws-access-key-id>
export AWS_SECRET_ACCESS_KEY=<aws-secret-access-key>
export BASE_DOMAIN=<aws-domain>
export OPENSHIFT_PULL_SECRET_FILE=<openshift-pull-secret-file>
export SSH_PRIVATE_KEY_FILE=<ssh-private-key-file>
export SSH_PUBLIC_KEY_FILE=<ssh-public-key-file>
export CLOUD_PROVIDER_SECRET_NAME=<cloud-provider-secret-name>
export CLOUD_PROVIDER_SECRET_NAMESPACE=<namespace-for-cloud-provider-secret>
```

2. Create AWS cloud provider `Credential` in a project from the Red Hat Advanced Cluster Management console (https://console-openshift-console.apps.<openshift-domain>/multicloud/credentials) or by running the following commands:

```
oc create ns ${CLOUD_PROVIDER_SECRET_NAMESPACE}
oc create secret generic ${CLOUD_PROVIDER_SECRET_NAME} \
  -n ${CLOUD_PROVIDER_SECRET_NAMESPACE} \
  --from-literal=aws_access_key_id=${AWS_ACCESS_KEY_ID} \
  --from-literal=aws_secret_access_key=${AWS_SECRET_ACCESS_KEY} \
  --from-literal=baseDomain=${BASE_DOMAIN} \
  --from-literal=httpProxy="" \
  --from-literal=httpsProxy="" \
  --from-literal=noProxy="" \
  --from-file=pullSecret=${OPENSHIFT_PULL_SECRET_FILE} \
  --from-file=ssh-privatekey=${SSH_PRIVATE_KEY_FILE} \
  --from-file=ssh-publickey=${SSH_PUBLIC_KEY_FILE} \
  --from-literal=additionalTrustBundle=""
```

## Create HyperShift Hosted Cluster on AWS

1. Set the following environment variables for the `HypershiftDeployment` resource:

```
export HYPERSHIFT_MGMT_CLUSTER=hypermgt
export HYPERSHIFT_HOSTING_NAMESPACE=clusters
export OPENSHIFT_RELEASE_IMAGE=quay.io/openshift-release-dev/ocp-release:4.10.15-x86_64
export INFRA_REGION=<cloud-provider-region>
export HYPERSHIFT_DEPLOYMENT_NAME=<hypershiftdeployment-name>
```

2. Create a `HypershiftDeployment` resource to provision the AWS hosted cluster:

```
oc apply -f - <<EOF
apiVersion: cluster.open-cluster-management.io/v1alpha1
kind: HypershiftDeployment
metadata:
  name: ${HYPERSHIFT_DEPLOYMENT_NAME}
  namespace: ${CLOUD_PROVIDER_SECRET_NAMESPACE}
spec:
  hostingCluster: ${HYPERSHIFT_MGMT_CLUSTER}
  hostingNamespace: ${HYPERSHIFT_HOSTING_NAMESPACE}
  infrastructure:
    cloudProvider:
      name: ${CLOUD_PROVIDER_SECRET_NAME}
    configure: true
    platform:
      aws:
        region: ${INFRA_REGION}
  nodePools:
  - name: ${HYPERSHIFT_DEPLOYMENT_NAME}-workers
    spec:
      clusterName: ${HYPERSHIFT_DEPLOYMENT_NAME}
      management:
        upgradeType: Replace
      nodeCount: 0
      platform:
        type: AWS
      release:
        image: ${OPENSHIFT_RELEASE_IMAGE}
EOF
```

3. Get the managed cluster name of the HyperShift hosted cluster that you created in the previous step and wait until managed cluster is available by running the following commands:

```
export HYPERSHIFT_MANAGED_CLUSTER_NAME=$(oc get managedcluster | grep ${HYPERSHIFT_DEPLOYMENT_NAME} | awk '{print $1}')
oc wait --for=condition=ManagedClusterConditionAvailable managedcluster/${HYPERSHIFT_MANAGED_CLUSTER_NAME} --timeout=600s
```

4. Retrieve kubeconfig for the HyperShift hosted cluster:

```
oc -n ${HYPERSHIFT_MGMT_CLUSTER} get secret ${HYPERSHIFT_MANAGED_CLUSTER_NAME}-admin-kubeconfig -o jsonpath="{.data.kubeconfig}" | base64 -d
```