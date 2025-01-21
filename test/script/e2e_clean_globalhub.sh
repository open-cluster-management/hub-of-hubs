#!/bin/bash
set -euo pipefail

CURRENT_DIR=$(
  cd "$(dirname "$0")" || exit
  pwd
)
source "$CURRENT_DIR/util.sh"

export KUBECONFIG=${KUBECONFIG:-${CONFIG_DIR}/global-hub}

while getopts ":n:" opt; do
  case $opt in
  n)
    GH_NAMESPACE="$OPTARG"
    ;;
  \?)
    echo "Invalid option -$OPTARG" >&2
    exit 1
    ;;
  esac

  case $OPTARG in
  -*)
    echo "Option $opt needs a valid argument"
    exit 1
    ;;
  esac
done

GH_NAMESPACE=${GH_NAMESPACE:=multicluster-global-hub}
export GH_NAMESPACE
echo "namespace: "$GH_NAMESPACE

echo "Delete mgh"
wait_cmd "kubectl delete mgh --all -n $GH_NAMESPACE --ignore-not-found=true"

export TARGET_NAMESPACE=$GH_NAMESPACE
cd operator
make undeploy

cd $CURRENT_DIR

## wait kafka/kafkatopic/kafka user be deleted
echo "Check kafkatopics deleted"
if [[ ! -z $(kubectl get kafkatopic -n "$GH_NAMESPACE" --ignore-not-found=true) ]]; then
  echo "Failed to delete kafkatopics"
  exit 1
fi

echo "Check kafkauser deleted"
if [[ ! -z $(kubectl get kafkauser -n "$GH_NAMESPACE" --ignore-not-found=true) ]]; then
  echo "Failed to delete kafkausers"
  exit 1
fi

echo "Check kafka deleted"
if [[ ! -z $(kubectl get kafka -n "$GH_NAMESPACE" --ignore-not-found=true) ]]; then
  echo "Failed to delete kafka"
  exit 1
fi

kubectl delete clusterrolebinding multicluster-global-hub-operator-rolebinding --ignore-not-found=true
kubectl delete clusterrolebinding multicluster-global-hub-operator-aggregated-clusterrolebinding --ignore-not-found=true

## clean
wait_cmd "kubectl delete crd kafkas.kafka.strimzi.io --ignore-not-found=true"
wait_cmd "kubectl delete crd kafkanodepools.kafka.strimzi.io --ignore-not-found=true"
wait_cmd "kubectl delete crd kafkatopics.kafka.strimzi.io --ignore-not-found=true"
wait_cmd "kubectl delete crd kafkausers.kafka.strimzi.io --ignore-not-found=true"

