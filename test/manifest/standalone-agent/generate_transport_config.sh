#!/bin/bash

CURRENT_DIR=$(
  cd "$(dirname "$0")" || exit
  pwd
)

KUBECONFIG=${1:-$KUBECONFIG}        # the kubeconfig for running the kafka
SECRET_KUBECONFIG=${2:-$KUBECONFIG} # generate the crenditial secret

kafka_namespace=${KAFKA_NAMESPACE:-"kafka"}
secret_namespace=open-cluster-management

standalone_user="global-hub-standalone-user"
status_topic="gh-status.standalone"
kubectl apply -f "$CURRENT_DIR/standalone-agent-resources.yaml" -n "$kafka_namespace"
kubectl wait --for=condition=Ready kafkauser/$standalone_user --timeout=500s

# Define a 5-minute timeout
timeout=300
end=$((SECONDS + timeout))
while [[ $SECONDS -lt $end ]]; do
  if kubectl get secret $standalone_user -n "$kafka_namespace" &>/dev/null; then
    echo "Secret $kafka_namespace/$standalone_user is now available!"
    break
  fi
  echo "Waiting for secret $kafka_namespace/$standalone_user to appear..."
  sleep 5
done
if ! kubectl get secret $standalone_user -n "$kafka_namespace" &>/dev/null; then
  echo "Timeout: Secret $kafka_namespace/$standalone_user did not appear within 5 minutes."
  exit 1
fi

cat <<EOF >"$CURRENT_DIR/kafka.yaml"
bootstrap.server: $(kubectl get kafka kafka -n "$kafka_namespace" -o jsonpath='{.status.listeners[1].bootstrapServers}')
topic.status: $status_topic
ca.crt: $(kubectl get kafka kafka -n "$kafka_namespace" -o jsonpath='{.status.listeners[1].certificates[0]}' | base64 -w 0)
client.crt: $(kubectl get secret $standalone_user -n "$kafka_namespace" -o jsonpath='{.data.user\.crt}')
client.key: $(kubectl get secret $standalone_user -n "$kafka_namespace" -o jsonpath='{.data.user\.key}')
EOF

kubectl create secret generic transport-config -n $secret_namespace --kubeconfig "$SECRET_KUBECONFIG" \
  --from-file=kafka.yaml="$CURRENT_DIR/kafka.yaml"
echo "event exporter kafka configuration is ready!"

host=$(kubectl get route inventory-api -n "$kafka_namespace" -ojsonpath='{.spec.host}')
cat <<EOF >"$CURRENT_DIR/rest.yaml"
host: https://$host
ca.crt: $(kubectl get secret inventory-api-server-ca-certs -n "$kafka_namespace" -ojsonpath='{.data.ca\.crt}')
client.crt: $(kubectl get secret inventory-api-guest-certs -n "$kafka_namespace" -ojsonpath='{.data.tls\.crt}')
client.key: $(kubectl get secret inventory-api-guest-certs -n "$kafka_namespace" -ojsonpath='{.data.tls\.key}')
EOF

kubectl patch secret transport-config -n $secret_namespace --kubeconfig "$SECRET_KUBECONFIG" \
  --type='json' \
  -p='[{"op": "add", "path": "/data/rest.yaml", "value":"'"$(base64 -w 0 "$CURRENT_DIR/rest.yaml")"'"}]'

rm "$CURRENT_DIR/kafka.yaml"
rm "$CURRENT_DIR/rest.yaml"