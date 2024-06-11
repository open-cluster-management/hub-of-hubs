#!/bin/bash

set -euo pipefail

CURRENT_DIR=$(
  cd "$(dirname "$0")" || exit
  pwd
)
# shellcheck source=/dev/null
source "$CURRENT_DIR/common.sh"

export CURRENT_DIR
export GH_NAME="global-hub"
export MH_NUM=${MH_NUM:-2}
export MC_NUM=${MC_NUM:-1}
export KinD=true

# setup kubeconfig
export KUBE_DIR=${CURRENT_DIR}/kubeconfig
check_dir "$KUBE_DIR"
export KUBECONFIG=${KUBECONFIG:-${KUBE_DIR}/clusters}
start=$(date +%s)

# Init clusters
echo -e "$BLUE creating clusters $NC"
start_time=$(date +%s)

kind_cluster "$GH_NAME" 2>&1 
for i in $(seq 1 "${MH_NUM}"); do
  kind_cluster "hub$i" 2>&1 
done

echo -e "${YELLOW} creating hubs:${NC} $(($(date +%s) - start_time)) seconds"

# GH
# service-ca
echo -e "$BLUE setting global hub service-ca and middlewares $NC"
enable_service_ca $GH_NAME "$CURRENT_DIR/resource" 2>&1 || true
# async middlewares
export GH_KUBECONFIG=$KUBE_DIR/$GH_NAME
bash "$CURRENT_DIR/resource/postgres/postgres_setup.sh" "$GH_KUBECONFIG" 2>&1 &
echo "$!" >"$KUBE_DIR/PID"
bash "$CURRENT_DIR"/resource/kafka/kafka_setup.sh "$GH_KUBECONFIG" 2>&1 &
echo "$!" >>"$KUBE_DIR/PID"

# async ocm, policy and app
echo -e "$BLUE installing ocm, policy, and app in global hub and managed hubs $NC"
start_time=$(date +%s)

# gobal-hub: hub1, hub2
(
  init_hub $GH_NAME 2>&1
  for i in $(seq 1 "${MH_NUM}"); do
    bash "$CURRENT_DIR"/ocm_setup.sh "$GH_NAME" "hub$i" HUB_INIT=false 2>&1 &
  done
  wait
) &

# hub1: cluster1 | hub2: cluster1
for i in $(seq 1 "${MH_NUM}"); do
  (
    init_hub "hub$i" 2>&1
    for j in $(seq 1 "${MC_NUM}"); do
      bash "$CURRENT_DIR"/ocm_setup.sh "hub$i" "hub$i-cluster$j" HUB_INIT=false 2>&1 &
    done
    wait
  ) &
done

wait
echo -e "${YELLOW} installing ocm, app and policy:${NC} $(($(date +%s) - start_time)) seconds"

# validation
echo -e "$BLUE validating ocm, app and policy $NC"
start_time=$(date +%s)

for i in $(seq 1 "${MH_NUM}"); do
  wait_ocm $GH_NAME "hub$i"
  wait_policy $GH_NAME "hub$i"
  wait_application $GH_NAME "hub$i"
  for j in $(seq 1 "${MC_NUM}"); do
    wait_ocm "hub$i" "hub$i-cluster$j"
    wait_policy "hub$i" "hub$i-cluster$j"
    wait_application "hub$i" "hub$i-cluster$j"
  done
done

# postgres
kubectl wait --for=condition=ready pod -l postgres-operator.crunchydata.com/instance-set=pgha1 -n hoh-postgres --context $GH_NAME --timeout=100s
# hoh
kubectl wait --for=condition=ready pod -l statefulset.kubernetes.io/pod-name=kafka-kafka-0 -n multicluster-global-hub --context $GH_NAME --timeout=100s

echo -e "${YELLOW} validating ocm, app and policy:${NC} $(($(date +%s) - start_time)) seconds"

# kubeconfig
for i in $(seq 1 "${MH_NUM}"); do
  echo -e "$CYAN [Access the ManagedHub]: export KUBECONFIG=$KUBE_DIR/hub$i $NC"
  for j in $(seq 1 "${MC_NUM}"); do
    echo -e "$CYAN [Access the ManagedCluster]: export KUBECONFIG=$KUBE_DIR/hub$i-cluster$j $NC"
  done
done
echo -e "${BOLD_GREEN}[Access the Clusters]: export KUBECONFIG=$KUBECONFIG $NC"
echo -e "${BOLD_GREEN}[ END ] ${NC} $(($(date +%s) - start)) seconds"