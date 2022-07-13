#!/bin/bash

set -o errexit
set -o nounset

CURRENT_DIR=$(cd "$(dirname "$0")" || exit;pwd)
CONFIG_DIR=${CURRENT_DIR}/config
export LOG=${LOG:-$CONFIG_DIR/e2e_setup.log}
export TAG=${TAG:-"latest"}

source ${CURRENT_DIR}/common.sh

LEAF_HUB_NAME="hub1"
HUB_OF_HUB_NAME="hub-of-hubs"
CTX_HUB="microshift" #"kind-hub-of-hubs"
CTX_MANAGED="kind-hub1"

# check the prerequisites helm and envsubst
checkDir ${CONFIG_DIR}
checkKind
checkKubectl
checkClusteradm

# setup kubeconfig
export KUBECONFIG=${KUBECONFIG:-${CONFIG_DIR}/kubeconfig}
echo "export KUBECONFIG=$KUBECONFIG" > ${LOG}
sleep 1 &
hover $! "KUBECONFIG=${KUBECONFIG}"

# init hoh 
source ${CURRENT_DIR}/microshift/microshift_setup.sh "$HUB_OF_HUB_NAME" >> "$LOG" 2>&1 &
hover $! "1 Prepare top hub cluster $HUB_OF_HUB_NAME"

# isolate the hub kubeconfig
HUB_KUBECONFIG=${CONFIG_DIR}/kubeconfig-hub-${CTX_HUB} # kind get kubeconfig --name "$HUB_OF_HUB_NAME" --internal > "$HUB_KUBECONFIG"
kubectl config view --context=${CTX_HUB} --minify --flatten > ${HUB_KUBECONFIG}

# enable olm
enableOLM $CTX_HUB >> "$LOG" 2>&1 &
hover $! "  Enable OLM for $CTX_HUB"

# install some component in microshift in detached mode
bash ${CURRENT_DIR}/postgres/postgres_setup.sh $HUB_KUBECONFIG >> "$LOG" 2>&1 &
bash ${CURRENT_DIR}/kafka/kafka_setup.sh $HUB_KUBECONFIG >> "$LOG" 2>&1 &
initHub $CTX_HUB >> $LOG 2>&1 &

# init leafhub 
sleep 1 &
hover $! "2 Prepare leaf hub cluster $LEAF_HUB_NAME"
source ${CURRENT_DIR}/leafhub_setup.sh 

# joining lh to hoh
initHub $CTX_HUB >> $LOG 2>&1 &
hover $! "3 Initing HoH OCM $HUB_OF_HUB_NAME" 

# check connection
connectMicroshift "${LEAF_HUB_NAME}-control-plane" "${HUB_OF_HUB_NAME}" >> "$LOG" 2>&1 &
hover $! "  Check connection: $LEAF_HUB_NAME -> $HUB_OF_HUB_NAME" 

initManaged $CTX_HUB $CTX_MANAGED >> $LOG 2>&1 &
hover $! "  Joining $CTX_HUB - $CTX_MANAGED" 

initApp $CTX_HUB $CTX_MANAGED >> "$LOG" 2>&1 &
hover $! "  Enable application $CTX_HUB - $CTX_MANAGED" 

initPolicy $CTX_HUB $CTX_MANAGED $HUB_KUBECONFIG >> "$LOG" 2>&1 &
hover $! "  Enable Policy $CTX_HUB - $CTX_MANAGED" 

kubectl config use-context $CTX_HUB >> "$LOG"

# install kafka
bash ${CURRENT_DIR}/kafka/kafka_setup.sh $KUBECONFIG >> "$LOG" 2>&1 &
hover $! "4 Install kafka cluster" 

# install postgres
bash ${CURRENT_DIR}/postgres/postgres_setup.sh $KUBECONFIG >> "$LOG" 2>&1 &
hover $! "5 Install postgres cluster" 

# deploy hoh
source ${CURRENT_DIR}/hoh_setup.sh >> "$LOG" 2>&1 &
hover $! "6 Deploy hub-of-hubs with $TAG" 

export KUBECONFIG=$KUBECONFIG
printf "%s\033[0;32m%s\n\033[0m" "[Access the Clusters]: " "export KUBECONFIG=$KUBECONFIG"