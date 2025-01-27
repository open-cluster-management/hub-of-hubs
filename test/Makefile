
e2e-dep: 
	./test/script/e2e_dep.sh

e2e-setup: tidy vendor e2e-dep
	./test/script/e2e_setup.sh

kessel-e2e-setup:
	./test/script/kessel_e2e_setup.sh

kessel-e2e-run: tidy vendor
	./test/script/kessel_e2e_run.sh

e2e-cleanup:
	./test/script/e2e_cleanup.sh

e2e-test-all: tidy vendor
	sh -x ./test/script/e2e_run.sh -f "e2e-test-localpolicy,e2e-test-placement,e2e-test-app,e2e-test-policy,e2e-tests-backup,e2e-test-grafana,e2e-test-agent" -v $(VERBOSE)
	sh -x ./test/script/e2e_clean_globalhub.sh
	sh -x ./test/script/e2e_run.sh -n mgh -f "e2e-test-localpolicy,e2e-test-grafana"
	sh -x ./test/script/e2e_clean_globalhub.sh -n mgh
	sh -x ./test/script/e2e_run_byo.sh -v $(VERBOSE)

e2e-test-cluster e2e-test-placement e2e-test-app e2e-test-policy e2e-test-localpolicy e2e-test-grafana e2e-test-agent: tidy vendor
	./test/script/e2e_run.sh -f $@ -v $(VERBOSE)

e2e-test-prune: tidy vendor
	./test/script/e2e_run.sh -f "e2e-test-prune" -v $(VERBOSE)

e2e-prow-tests: 
	./test/script/e2e_prow.sh

e2e-log/operator:
	./test/script/e2e_log.sh

e2e-log/manager:
	@COMPONENT=multicluster-global-hub-manager ./test/script/e2e_log.sh

e2e-log/grafana:
	@COMPONENT=multicluster-global-hub-grafana ./test/script/e2e_log.sh

e2e-log/agent:
	@export CLUSTER_NAME=hub1 COMPONENT=multicluster-global-hub-agent NAMESPACE=multicluster-global-hub-agent && ./test/script/e2e_log.sh

integration-test: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./test/integration/...`

integration-test/agent: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./test/integration/agent/...`

integration-test/manager: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./test/integration/manager/...`

integration-test/operator: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./test/integration/operator/...`
