// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package nonk8sapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stolostron/multicluster-global-hub/test/pkg/testpostgres"
)

var (
	ctx            context.Context
	cancel         context.CancelFunc
	testPostgres   *testpostgres.TestPostgres
	testAuthServer *httptest.Server
)

func TestNonK8sAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NonK8s API Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	var err error

	testPostgres, err = testpostgres.NewTestPostgres()
	Expect(err).NotTo(HaveOccurred())
	err = testpostgres.InitDatabase(testPostgres.URI)
	Expect(err).NotTo(HaveOccurred())

	testAuthServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind": "User",
			"apiVersion": "user.openshift.io/v1",
			"metadata": {
			  "name": "kube:admin",
			  "creationTimestamp": null
			},
			"groups": [
			  "system:authenticated",
			  "system:cluster-admins"
			]
		  }`))
	}))
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	testAuthServer.Close()
	err := testPostgres.Stop()
	Expect(err).NotTo(HaveOccurred())
	cancel()
})
