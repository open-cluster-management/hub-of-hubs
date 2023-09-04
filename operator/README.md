# multicluster-global-hub-operator

The operator of multicluster global hub (see: https://github.com/stolostron/multicluster-global-hub)

## Prerequisites

1. Connect to a Kubernetes cluster with `kubectl`
2. ACM or OCM is installed on the Kubernetes cluster
3. PostgreSQL is installed and a database is created for multicluster global hub, also a secret with name `multicluster-global-hub-storage` that contains the credential should be created in `multicluster-global-hub` namespace. The credential format like `postgres://<user>:<password>@<host>:<port>/<database>`:

```bash
kubectl create secret generic multicluster-global-hub-storage -n "multicluster-global-hub" \
    --from-literal=database_uri=<postgresql-uri> 
```
> You can run this sample script `config/samples/storage/deploy_postgres.sh` to install postgres in `multicluster-global-hub-postgres` namespace and create the secret `multicluster-global-hub-storage` in namespace `multicluster-global-hub` automatically. To override the secret namespace, set `TARGET_NAMESPACE` environment variable to the ACM installation namespace before executing the script.

4. Kafka is installed and two topics `spec` and `status` are created, also a secret with name `multicluster-global-hub-transport` that contains the kafka access information should be created in `multicluster-global-hub` namespace:

```bash
kubectl create secret generic multicluster-global-hub-transport -n "multicluster-global-hub" \
    --from-literal=bootstrap_server=<kafka-bootstrap-server-address> \
    --from-literal=CA=<CA-for-kafka-server>
```
> As above, You can run this sample script `config/samples/transport/deploy_kafka.sh` to install kafka in kafka namespace and create the secret `multicluster-global-hub-transport` in namespace `multicluster-global-hub` automatically. To override the secret namespace, set `TARGET_NAMESPACE` environment variable to the ACM installation namespace before executing the script.

## Getting started

_Note:_ You can also install Multicluster Global Hub Operator from [Operator Hub](https://docs.openshift.com/container-platform/4.6/operators/understanding/olm-understanding-operatorhub.html) if you have ACM installed in an OpenShift Container Platform, the operator can be found in community operators by searching "multicluster global hub" keyword in the filter box, then follow the document to install the operator.

Follow the steps below to instal Multicluster Global Hub Operator in developing environment:

### Running on the cluster

1. Build and push your image to the location specified by `IMG`:

```bash
make docker-build docker-push IMG=<some-registry>/multicluster-global-hub-operator:<tag>
```

2. Deploy the controller to the cluster with the image specified by `IMG`:

```bash
make deploy IMG=<some-registry>/multicluster-global-hub-operator:<tag>
```

3. Install Instances of Custom Resource:

```bash
kubectl apply -k config/samples/
```

### Undeploy from the cluster

Undeploy the controller and CRD from the cluster:

```bash
make undeploy
```

## Contributing

### Test It Out Locally

1. Install CRD and run operator locally:

```bash
make install run
```

2. Install Instances of Custom Resource:

```bash
kubectl apply -k config/samples/
```

### Modifying the API definitions

If you are editing the API definitions, generate the generated code and manifests such as CRs, CRDs, CSV using:

```bash
make generate manifests bundle
```

NOTE: Run `make --help` for more information on all potential make targets
