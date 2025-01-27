#!/bin/bash

set -euo pipefail

CURRENT_DIR=$(
  cd "$(dirname "$0")" || exit
  pwd
)
source "$CURRENT_DIR/util.sh"

CONFIG_DIR=$CURRENT_DIR/config
GH_KUBECONFIG="${CONFIG_DIR}/global-hub"
KAFKA_KUBECONFIG="${CONFIG_DIR}/hub2"
POSTGRES_KUBECONFIG="${CONFIG_DIR}/hub1"

export ISBYO="true"

target_namespace=${TARGET_NAMESPACE:-"multicluster-global-hub"}

######################################### Generate Storage Secret ###################################################
storage_secret=${STORAGE_SECRET_NAME:-"multicluster-global-hub-storage"}
pg_ns="hoh-postgres"
ps_user="hoh-pguser-postgres"
pg_cert="hoh-cluster-cert"

if kubectl get secret "$storage_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG" >/dev/null 2>&1; then
  echo "storage: $storage_secret already exists in $target_namespace namespace"
  kubectl delete secret "$storage_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG"
fi

# wait the pg cluster is ready
wait_cmd "kubectl get pods --kubeconfig $POSTGRES_KUBECONFIG -l postgres-operator.crunchydata.com/instance-set=pgha1 -n $pg_ns | grep Running"
kubectl wait --for=condition=ready pod -l postgres-operator.crunchydata.com/instance-set=pgha1 -n $pg_ns --timeout=100s --kubeconfig "$POSTGRES_KUBECONFIG"
echo "postgres cluster is ready!"

database_uri=$(kubectl get secrets -n "${pg_ns}" --kubeconfig "$POSTGRES_KUBECONFIG" "${ps_user}" -o go-template='{{index (.data) "uri" | base64decode}}')
kubectl get secret $pg_cert -n $pg_ns --kubeconfig "$POSTGRES_KUBECONFIG" -o jsonpath='{.data.ca\.crt}' | base64 -d >"$CONFIG_DIR/postgres-cluster-ca.crt"

# covert the database uri into external uri
external_host=$(kubectl config view --minify --kubeconfig "$POSTGRES_KUBECONFIG" -o jsonpath='{.clusters[0].cluster.server}' | sed -e 's#^https\?://##' -e 's/:.*//')
external_port=32432
database_uri=$(echo "${database_uri}" | sed "s|@[^/]*|@$external_host:$external_port|")

kubectl create namespace "$target_namespace" --dry-run=client -o yaml | kubectl --kubeconfig "$GH_KUBECONFIG" apply -f -
kubectl create secret generic "$storage_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG" \
  --from-literal=database_uri="${database_uri}?sslmode=verify-ca" \
  --from-file=ca.crt="$CONFIG_DIR/postgres-cluster-ca.crt"

echo "storage secret is ready in $target_namespace namespace!"

######################################### Generate Transport Secret ###################################################
byo_user=global-hub-byo-user
transport_secret=${TRANSPORT_SECRET_NAME:-"multicluster-global-hub-transport"}
kafka_namespace=${KAFKA_NAMESPACE:-"kafka"}

if kubectl get secret "$transport_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG" >/dev/null 2>&1; then
  echo "transport: $transport_secret already exists in $target_namespace namespace"
  kubectl delete secret "$transport_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG"
fi

# wait the cluster is ready
wait_cmd "kubectl get kafka kafka -n $kafka_namespace --kubeconfig $KAFKA_KUBECONFIG -o jsonpath='{.status.listeners[0]}' | grep bootstrapServers"

# wait the byo kafkatopic and kafkauser
wait_cmd "kubectl get kafkatopic gh-spec -n $kafka_namespace --kubeconfig $KAFKA_KUBECONFIG | grep -C 1 True"
wait_cmd "kubectl get kafkatopic gh-status -n $kafka_namespace --kubeconfig $KAFKA_KUBECONFIG | grep -C 1 True"
wait_cmd "kubectl get kafkauser $byo_user -n $kafka_namespace --kubeconfig $KAFKA_KUBECONFIG | grep -C 1 True"
echo "Kafka topic and user is ready"

bootstrap_server=$(kubectl get kafka kafka -n "$kafka_namespace" --kubeconfig "$KAFKA_KUBECONFIG" -o jsonpath='{.status.listeners[0].bootstrapServers}')
kubectl get kafka kafka -n "$kafka_namespace" --kubeconfig "$KAFKA_KUBECONFIG" -o jsonpath='{.status.listeners[0].certificates[0]}' >"$CURRENT_DIR"/config/kafka-ca-cert.pem
kubectl get secret $byo_user -n "$kafka_namespace" --kubeconfig "$KAFKA_KUBECONFIG" -o jsonpath='{.data.user\.crt}' | base64 -d >"$CURRENT_DIR"/config/kafka-client-cert.pem
kubectl get secret $byo_user -n "$kafka_namespace" --kubeconfig "$KAFKA_KUBECONFIG" -o jsonpath='{.data.user\.key}' | base64 -d >"$CURRENT_DIR"/config/kafka-client-key.pem

# generate the secret in the target cluster: GH_KUBECONFIG
kubectl create secret generic "$transport_secret" -n "$target_namespace" --kubeconfig "$GH_KUBECONFIG" \
  --from-literal=bootstrap_server="$bootstrap_server" \
  --from-file=ca.crt="$CURRENT_DIR"/config/kafka-ca-cert.pem \
  --from-file=client.crt="$CURRENT_DIR"/config/kafka-client-cert.pem \
  --from-file=client.key="$CURRENT_DIR"/config/kafka-client-key.pem

echo "transport secret is ready in $target_namespace namespace!"
## run e2e
bash "$CURRENT_DIR/e2e_run.sh" -f "e2e-test-localpolicy,e2e-test-grafana"

UNSET ISBYO
