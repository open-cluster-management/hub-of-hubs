#!/bin/bash

HUB_CLUSTER_NUM=${HUB_CLUSTER_NUM:-2}
MANAGED_CLUSTER_NUM=${MANAGED_CLUSTER_NUM:-1}

CURRENT_DIR=$(cd "$(dirname "$0")" || exit;pwd)
CONFIG_DIR=${CURRENT_DIR}/config
LOG=${CONFIG_DIR}/hoh_setup.log
PID=${CONFIG_DIR}/pid

source ${CURRENT_DIR}/common.sh

checkDir ${CONFIG_DIR}
checkKind

LEAF_HUB_NAME="hub"
HUB_OF_HUB_NAME="global-hub"

# kubeconfig
KUBECONFIG=${KUBECONFIG:-${CONFIG_DIR}/kubeconfig}

touch "$PID"
while read -r line; do
  if [[ $line != "" ]]; then
    kill -9 "${line}" >/dev/null 2>&1
  fi
done <"${PID}"

kind delete cluster --name ${HUB_OF_HUB_NAME}
for i in $(seq 1 "${HUB_CLUSTER_NUM}"); do
  kind delete cluster --name "hub${i}"
  for j in $(seq 1 "${MANAGED_CLUSTER_NUM}"); do
    kind delete cluster --name "hub${i}-cluster${j}"
  done
done

rm "${PID}" >/dev/null 2>&1
rm -rf "$CONFIG_DIR"
ps -ef |grep "setup" |grep -v grep |awk '{print $2}' | xargs kill -9 >/dev/null 2>&1