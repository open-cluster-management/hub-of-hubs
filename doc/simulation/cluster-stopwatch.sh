#!/bin/bash

export KUBECONFIG=${KUBECONFIG:-$1}
echo "KUBECONFIG=$KUBECONFIG"

while [ true ]; do
    pgha="$(oc get pods -n multicluster-global-hub |grep hoh-pgha |awk '{print $1}' || true)"
    if [ "$pgha" != "" ]; then
        echo "database pod $pgha"
        break
    fi
    echo "waitting init database"
    sleep 1
done

function stopwatch() {
    n=0
    while [ true ]; do
        sleep 1
        echo -e " $n \c"
            (( n++ ))
    done
}
stopwatch &

while [ true ]; do
    sleep 0.4
    num=$(oc exec -i $pgha -c database -n multicluster-global-hub -- psql -A -t -U postgres -d hoh -c "select count(1) from status.managed_clusters" | grep -v row | grep -oP '\d*' || echo 0)
    if [ "$num" -gt 0 ]; then
        echo " [ MCs: $num ] "
    fi
done