# Copyright (c) 2022 Red Hat, Inc.
# Copyright Contributors to the Open Cluster Management project

-include /opt/build-harness/Makefile.prow

REGISTRY ?= quay.io/stolostron
IMAGE_TAG ?= latest
TMP_BIN ?= /tmp/cr-tests-bin
GO_TEST ?= go test -v

.PHONY: vendor			##download all third party libraries and puts them inside vendor directory
vendor:
	@go mod vendor

.PHONY: tidy			
tidy:
	@go mod tidy

.PHONY: clean-vendor			##removes third party libraries from vendor directory
clean-vendor:
	-@rm -rf vendor

build-operator-image: vendor
	cd operator && make
	docker build -t ${REGISTRY}/multicluster-global-hub-operator:${IMAGE_TAG} . -f operator/Dockerfile

push-operator-image:
	docker push ${REGISTRY}/multicluster-global-hub-operator:${IMAGE_TAG}

deploy-operator: 
	cd operator && make deploy

undeploy-operator:
	cd operator && make undeploy

build-manager-image: vendor
	cd manager && make
	docker build -t ${REGISTRY}/multicluster-global-hub-manager:${IMAGE_TAG} . -f manager/Dockerfile

push-manager-image:
	docker push ${REGISTRY}/multicluster-global-hub-manager:${IMAGE_TAG}

build-agent-image: vendor
	cd agent && make
	docker build -t ${REGISTRY}/multicluster-global-hub-agent:${IMAGE_TAG} . -f agent/Dockerfile

push-agent-image:
	docker push ${REGISTRY}/multicluster-global-hub-agent:${IMAGE_TAG}

.PHONY: unit-tests
unit-tests: unit-tests-pkg unit-tests-operator unit-tests-manager unit-tests-agent

setup_envtest:
	GOBIN=${TMP_BIN} go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

unit-tests-operator: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./operator/... | grep -v test`

unit-tests-manager: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./manager/... | grep -v test`

unit-tests-agent: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./agent/... | grep -v test`

unit-tests-pkg: setup_envtest
	KUBEBUILDER_ASSETS="$(shell ${TMP_BIN}/setup-envtest use --use-env -p path)" ${GO_TEST} `go list ./pkg/... | grep -v test`

e2e-dep: 
	./test/setup/e2e_dep.sh

e2e-setup: tidy vendor e2e-dep
	./test/setup/e2e_setup.sh

e2e-cleanup:
	./test/setup/e2e_clean.sh

e2e-test-all: tidy vendor
	./cicd-scripts/run-local-e2e-test.sh -f "e2e-test-validation,e2e-test-localpolicy,e2e-test-placement,e2e-test-app,e2e-test-policy,e2e-tests-backup" -v $(VERBOSE)

e2e-test-validation e2e-test-cluster e2e-test-placement e2e-test-app e2e-test-policy e2e-test-localpolicy e2e-test-grafana: tidy vendor
	./cicd-scripts/run-local-e2e-test.sh -f $@ -v $(VERBOSE)

e2e-test-prune: tidy vendor
	./cicd-scripts/run-local-e2e-test.sh -f "e2e-test-prune" -v $(VERBOSE)

e2e-prow-tests: 
	./cicd-scripts/run-prow-e2e-test.sh

.PHONY: fmt				##formats the code
fmt:
	@go fmt ./agent/... ./manager/... ./operator/... ./pkg/... ./test/pkg/...
	git diff --exit-code
	! grep -ir "multicluster-global-hub/agent/\|multicluster-global-hub/operator/\|multicluster-global-hub/manager/" ./pkg
	! grep -ir "multicluster-global-hub/agent/\|multicluster-global-hub/manager/" ./operator
	! grep -ir "multicluster-global-hub/operator/\|multicluster-global-hub/manager/|" ./agent
	! grep -ir "multicluster-global-hub/operator/\|multicluster-global-hub/agent/|" ./manager

.PHONY: strict-fmt				##formats the code
strict-fmt:
	@gci write -s standard -s default -s "prefix(github.com/stolostron/multicluster-global-hub)" ./agent/ ./manager/ ./operator/ ./pkg/ ./test/pkg/
	gofumpt -w ./agent/ ./manager/ ./operator/ ./pkg/ ./test/pkg/
	git diff --exit-code

install-kafka: # install kafka on the ocp
	./operator/config/samples/transport/deploy_kafka.sh

uninstall-kafka: 
	./operator/config/samples/transport/undeploy_kafka.sh

install-postgres: # install postgres on the ocp
	./operator/config/samples/storage/deploy_postgres.sh

uninstall-postgres: 
	./operator/config/samples/storage/undeploy_postgres.sh