# Multicluster Global Hub

The multicluster global hub is a set of components that enable the management of multiple hub clusters from a single hub cluster. You can complete the following tasks by using the multicluster global hub:

- Deploy managed hub clusters
- List the managed clusters that are managed by all of the managed hub clusters

The multicluster global hub is useful when a single hub cluster cannot manage the large number of clusters in a high-scale environment. The multicluster global hub designates multiple managed clusters as multiple managed hub clusters. The global hub cluster manages the managed hub clusters.

- [Multicluster Global Hub](#multicluster-global-hub)
  - [Use Cases](./global_hub_use_cases.md)
  - [Architecture](#architecture)
    - [Multicluster Global Hub Operator](#multicluster-global-hub-operator)
    - [Multicluster Global Hub Manager](#multicluster-global-hub-manager)
    - [Multicluster Global Hub Agent](#multicluster-global-hub-agent)
    - [Multicluster Global Hub Observability](#multicluster-global-hub-observability)
  - [Workings of Global Hub](./how_global_hub_works.md)
  - [Quick Start](#quick-start)
    - [Prerequisites](#prerequisites)
      - [Dependencies](#dependencies)
      - [Network configuration](#network-configuration)
    - [Installation](#installation)
      - [1. Install the multicluster global hub operator on a disconnected environment](#1-install-the-multicluster-global-hub-operator-on-a-disconnected-environment)
      - [2. Install the multicluster global hub operator from OpenShift console](#2-install-the-multicluster-global-hub-operator-from-openshift-console)
    - [Import a managed hub cluster in default mode (tech preview)](#import-a-managed-hub-cluster-in-default-mode-tech-preview)
    - [Access the grafana](#access-the-grafana)
    - [Grafana dashboards](#grafana-dashboards)
    - [Cronjobs and Metrics](#cronjobs-and-metrics)
  - [Troubleshooting](./troubleshooting.md)
  - [Development preview features](./dev-preview.md)
  - [Known issues](#known-issues)

## Use Cases

You can read about the use cases for multicluster global hub in [Use Cases](./global_hub_use_cases.md).

## Architecture

![ArchitectureDiagram](architecture/multicluster-global-hub-arch.png)

### Multicluster Global Hub Operator

The Multicluster Global Hub Operator contains the components of multicluster global hub. The Operator deploys all of the required components for global multicluster management. The components include `multicluster-global-hub-manager` in the global hub cluster and `multicluster-global-hub-agent` in the managed hub clusters.

The Operator also leverages the `manifestwork` custom resource to deploy the Red Hat Advanced Cluster Management for Kubernetes Operator on the managed cluster. After the Red Hat Advanced Cluster Management Operator is deployed on the managed cluster, the managed cluster becomes a standard Red Hat Advanced Cluster Management Hub cluster. This hub cluster is now a managed hub cluster.

### Multicluster Global Hub Manager

The Multicluster Global Hub Manager is used to persist the data into the `postgreSQL` database. The data is from Kafka transport. The manager also posts the data to the Kafka transport, so it can be synchronized with the data on the managed hub clusters.

### Multicluster Global Hub Agent

The Multicluster Global Hub Agent runs on the managed hub clusters. It synchronizes the data between the global hub cluster and the managed hub clusters. For example, the agent synchronizes the information of the managed clusters from the managed hub clusters with the global hub cluster and synchronizes the policy or application from the global hub cluster and the managed hub clusters.

### Multicluster Global Hub Observability

Grafana runs on the global hub cluster as the main service for Global Hub Observability. The Postgres data collected by the Global Hub Manager is its default DataSource. By exposing the service using the route called `multicluster-global-hub-grafana`, you can access the global hub Grafana dashboards by accessing the Red Hat OpenShift Container Platform console.

## Workings of Global Hub

To understand how Global Hub functions, see [How global hub works](how_global_hub_works.md).

## Quick Start

The following sections provide the steps to start using the Multicluster Global Hub.

### Prerequisites
#### Dependencies

- Red Hat Advanced Cluster Management for Kubernetes verison 2.7 or later must be installed and configured. [Learn more details about Red Hat Advanced Cluster Management](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.8)

- Storage secret

    Both the global hub manager and Grafana services need a postgres database to collect and display data. The data can be accessed by creating a storage secret, 
    which contains the following two fields:

    - `database_uri`: Required, the URI user must have the permission to create the global hub database in the postgres.
    - `database_uri_with_readonlyuser`: Required, the URI user must have the permission to read the global hub database in the postgres.
    - `ca.crt`: Optional, if your database service has TLS enabled, you can provide the appropriate certificate depending on the SSL mode of the connection. If 
    the SSL mode is `verify-ca` and `verify-full`, then the `ca.crt` certificate must be provided.

    **Note:** There is a [sample script](https://github.com/stolostron/multicluster-global-hub/tree/main/operator/config/samples/storage) available to install postgres in `hoh-postgres` namespace and create the secret `storage-secret` in namespace `open-cluster- 
    management` automatically. The client version of kubectl must be verison 1.21, or later. 

- Transport secret

    Right now, only Kafka transport is supported. You need to create a secret for the Kafka transport. The secret contains the following fields:

    - `bootstrap.servers`: Required, the Kafka bootstrap servers.
    - `ca.crt`: Optional, if you use the `KafkaUser` custom resource to configure authentication credentials, see [User authentication](https://strimzi.io/docs/operators/latest/deploying.html#con-securing-client-authentication-str) in the STRIMZI documentation for the steps to extract the `ca.crt` certificate from the secret.
    - `client.crt`: Optional, see [User authentication](https://strimzi.io/docs/operators/latest/deploying.html#con-securing-client-authentication-str) in the STRIMZI documentation for the steps to extract the `user.crt` certificate from the secret.
    - `client.key`: Optional, see [User authentication](https://strimzi.io/docs/operators/latest/deploying.html#con-securing-client-authentication-str) in the STRIMZI documentation for the steps to extract the `user.key` from the secret.

    **Note:** There is a [sample script](https://github.com/stolostron/multicluster-global-hub/tree/main/operator/config/samples/transport) available to automatically install kafka in the `kafka` namespace and create the secret `transport-secret` in namespace `open-cluster-management`.

- Crunchy Postgres for Kubernetes version 5.0 or later needs to be installed

    Crunchy Postgres for Kubernetes provide a declarative Postgres solution that automatically manages PostgreSQL clusters.
    
    See [Crunchy Postgres for Kubernetes](https://access.crunchydata.com/documentation/postgres-operator/v5/) for more information about Crunchy Postgres for Kubernetes. 

    Global hub manager and Grafana services need Postgres database to collect and display data. The data can be accessed by creating a storage secret named `multicluster-global-hub-storage` in the `open-cluster-management` namespace. This secret should contain the following two fields:

    - `database_uri`: Required: The URI user should have the required permission to create the global hub database in the postgres.
    - `database_uri_with_readonlyuser`: Required, the URI user must have the permission to read the global hub database in the postgres.
    - `ca.crt`: Optional: If your database service has TLS enabled, you can provide the appropriate certificate depending on the SSL mode of the connection. If the SSL mode is `verify-ca` and `verify-full`, then the `ca.crt` certificate must be provided.

    **Note:** There is a sample script available [here](https://github.com/stolostron/multicluster-global-hub/tree/main/operator/config/samples/storage)(Note:the client version of kubectl must be v1.21+) to install postgres in `hoh-postgres` namespace and automatically create the secret `multicluster-global-hub-storage` in namespace `open-cluster-management`.

- Strimzi 0.33 or later needs to be installed

    Strimzi provides a way to run Kafka cluster on Kubernetes in various deployment configurations. 
    
    See the [Strimzi documentation](https://strimzi.io/documentation/) to learn more about Strimzi.

    Global hub agent need to synchronize cluster information and policy information to Kafka transport. The global hub manager persists the Kafka transport data to Postgres database.

#### Sizing
1. [Sizing your Red Hat Advanced Cluster Management cluster](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.7/html/install/installing#sizing-your-cluster)

2. **Minimum requirements for Crunchy Postgres**

    | vCPU | Memory | Storage size | Namespace |
    | ---- | ------ | ------ | ------ |
    | 100m | 2G | 20Gi*3 | hoh-postgres
    | 10m | 500M | N/A | postgres-operator
    
3. **Minimum requirements for Strimzi**

    | vCPU | Memory | Storage size | Namespace |
    | ---- | ------ | ------ | ------ |
    | 100m | 8G | 20Gi*3 | kafka


#### Network configuration

The managed hub is also a managed cluster of global hub in Red Hat Advanced Cluster Management. The network configuration in Red Hat Advanced Cluster Management is necessary. See [Networking](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.7/html/networking/networking) for Red Hat Advanced Cluster Management networking details.

1. Global hub networking requirements

| Direction | Protocol | Connection | Port (if specified) | Source address |	Destination address |
| ------ | ------ | ------ | ------ |------ | ------ |
|Inbound from browser of the user | HTTPS | User need to access the Grafana dashboard | 443 | Browser of the user | IP address of Grafana route |
| Outbound to Kafka Cluster | HTTPS | Global hub manager need to get data from Kafka cluster | 443 | multicluster-global-hub-manager-xxx pod | Kafka route host |
| Outbound to Postgres database | HTTPS | Global hub manager need to persist data to Postgres database | 443 | multicluster-global-hub-manager-xxx pod | IP address of Postgres database |

2. Managed hub networking requirements

| Direction | Protocol | Connection | Port (if specified) | Source address |	Destination address |
| ------ | ------ | ------ | ------ | ------ | ------ |
| Outbound to Kafka Cluster | HTTPS | Global hub agent need to sync cluster info and policy info to Kafka cluster | 443 | multicluster-global-hub-agent pod | Kafka route host |

### Installation

1. [Install the multicluster global hub operator on a disconnected environment](./disconnected_environment/README.md)

2. Install the multicluster global hub operator from the Red Hat OpenShift Container Platform console:

    1. Log in to the Red Hat OpenShift Container Platform console as a user with the `cluster-admin` role.
    2. Click **Operators** > OperatorHub icon in the navigation.
    3. Search for and select the `multicluster global hub operator`.
    4. Click `Install` to start the installation.
    5. After the installation completes, check the status on the *Installed Operators* page.
    6. Click **multicluster global hub operator** to go to the *Operator* page.
    7. Click the *multicluster global hub* tab to see the `multicluster global hub` instance.
    8. Click **Create multicluster global hub** to create the `multicluster global hub` instance.
    9. Enter the required information and click **Create** to create the `multicluster global hub` instance.

    **Notes:**
    * The multicluster global hub is only available for the x86 platform.
    * The policy and application are disabled in Red Hat Advanced Cluster Management after the multicluster global hub is installed.

### Import a managed hub cluster in default mode (Technology Preview)

You must disable the cluster self-management in the existing Red Hat Advanced Cluster Management hub cluster. Set `disableHubSelfManagement=true` in the `multiclusterhub` custom resource to disable the automatic importing of the hub cluster as a managed cluster.

Import the managed hub cluster by completing the steps in [Import cluster](https://access.redhat.com/documentation/en-us/red_hat_advanced_cluster_management_for_kubernetes/2.8/html-single/clusters/index#importing-a-target-managed-cluster-to-the-hub-cluster).

After the managed hub cluster is imported, check the global hub agent status to ensure that the agent is running in the managed hub cluster by running the following command:

```
oc get managedclusteraddon multicluster-global-hub-controller -n ${MANAGED_HUB_CLUSTER_NAME}
```

### Access the Grafana data

The Grafana data is exposed through the route. Run the following command to display the login URL:

```
oc get route multicluster-global-hub-grafana -n <the-namespace-of-multicluster-global-hub-instance>
```

The authentication method of this URL is same as authenticating to the Red Hat OpenShift Container Platform console.

### Grafana dashboards

After accessing the global hub Grafana data, you can begin monitoring the policies that were configured through the hub cluster environments that are managed. From the global hub dashboard, you can identify the compliance status of the policies of the system over a selected time range. The policy compliance status is updated daily, so the dashboard does not display the status of the current day until the following day.

![Global Hub Policy Group Compliancy Overview](./images/global-hub-policy-group-compliancy-overview.gif)

To navigate the global hub dashboards, you can choose to observe and filter the policy data by grouping them either by `policy` or `cluster`. If you prefer to examine the policy data by using the `policy` grouping, you should start from the default dashboard called `Global Hub - Policy Group Compliancy Overview`. This dashboard allows you to filter the policy data based on `standard`, `category`, and `control`. After selecting a specific point in time on the graph, you are directed to the `Global Hub - Offending Policies` dashboard, which lists the non-compliant or unknown policies at that time. After selecting a target policy, you can view related events and see what has changed by accessing the `Global Hub - What's Changed / Policies` dashboard.

![Global Hub Cluster Group Compliancy Overview](./images/global-hub-cluster-group-compliancy-overview.gif)

Similarly, if you want to examine the policy data by `cluster` grouping, begin by using the `Global Hub - Cluster Group Compliancy Overview` dashboard. The navigation flow is identical to the `policy` grouping flow, but you select filters that are related to the cluster, such as managed cluster `labels` and `values`. Instead of viewing policy events for all clusters, after reaching the `Global Hub - What's Changed / Clusters` dashboard, you can view policy events related to an individual cluster.

### Cronjobs and Metrics

After installing the global hub operand, the global hub manager starts running and pull ups a job scheduler to schedule two cronjobs:

- Local compliance status sync job

  At 0 o'clock every day, based on the policy status and events collected by the manager on the previous day. Running the job to summarize the compliance status and change frequency of the policy on the cluster, and store them to the `history.local_compliance` table as the data source of grafana dashboards. Please refer to [here](./how_global_hub_works.md) for more details.

- Partition job

  Some data tables in global hub will continue to grow over time. Generally, they fall into two categories: the policy event tables and the `history.local_compliance` growing every day, the tables containing soft deleted records. The former generates a large amount of data, we use range partitioning to break down the large tables into small partitions. Which helps in executing queries/deletions on these tables faster. The later has a small amount of data, and we add `deletedAt` indexes to these tables to obtain better hard delete performance.

  At the practical level, we run a scheduled job to delete expired data, so as to avoid the table being too large, and there is an additional task for it which is to create a buffer partition table for the next month.

  How long the job should keep the data can be configured through the [retention](https://github.com/stolostron/multicluster-global-hub/blob/main/operator/apis/v1alpha4/multiclusterglobalhub_types.go#L90) on the global hub operand. it's recommended minimum value is 1 month, default value is 18 months. Therefore, the execution interval of this job should be less than one month.

The above cronjobs are executed every time the global hub manager starts. The compliance sync job is run once a day and can be run multiple times within the day without changing the result. The partitioning job is run once a week and also can be run many times per month, the results will not change. 
These two jobs' status are saved in the metrics named `multicluster_global_hub_jobs_status`, as shown in the figure below from the console of the Openshift cluster. Where `0` means the job runs successfully, otherwise `1` means failure. 

![Global Hub Jobs Status Metrics Panel](./images/global-hub-jobs-status-metrics-panel.png)

If there is a failed job, then you can dive into the log tables(`history.local_compliance_job_log`, `event.data_retention_job_log`) for more details.

## Troubleshooting

For common Troubleshooting issues, see [Troubleshooting](troubleshooting.md).

## Known issues

1. If the database is empty, the Grafana dashboards show the error `db query syntax error for {dashboard_name} dashboard`. The error is resolved when there is data in the database. The top-level dashboards are populated only the day after data collection is started, as explained in [Workings of Global Hub](how_global_hub_works.md)

2. You cannot drill down by selecting the first datapoint from the `Policy Group Compliancy Overview` dashboard. You can drill down the `Offending Policies` dashboard when you click a datapoint from the `Policy Group Compliancy Overview` dashboard, but it is not working for the first datapoint in the list. This issue also applies to the `Cluster Group Compliancy Overview` dashboard.

3. A managed cluster that is not created successfully (clusterclaim `id.k8s.io` does not exist in the managed cluster) is not counted in global hub policy compliance database, but shows in the Red Hat Advanced Cluster Management policy console.
