// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package condition

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	operatorv1alpha3 "github.com/stolostron/multicluster-global-hub/operator/apis/v1alpha3"
)

var (
	cfg           *rest.Config
	runtimeClient client.Client
)

func TestMain(m *testing.M) {
	// start testenv
	testenv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "pkg", "testdata", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testenv.Start()
	if err != nil {
		panic(err)
	}

	if cfg == nil {
		panic(fmt.Errorf("empty kubeconfig!"))
	}

	scheme := runtime.NewScheme()
	err = operatorv1alpha3.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}

	runtimeClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}

	// run testings
	code := m.Run()

	// stop testenv
	err = testenv.Stop()
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}

func TestCondition(t *testing.T) {
	mgh := &operatorv1alpha3.MulticlusterGlobalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: operatorv1alpha3.MulticlusterGlobalHubSpec{
			DataLayer: &operatorv1alpha3.DataLayerConfig{
				Type:       operatorv1alpha3.LargeScale,
				LargeScale: &operatorv1alpha3.LargeScaleConfig{},
			},
		},
	}

	err := runtimeClient.Create(context.TODO(), mgh)
	if err != nil {
		t.Fatalf("failed to create mgh: %v", err)
	}

	cases := []struct {
		name          string
		conditionType string
		setFunc       SetConditionFunc
	}{
		{CONDITION_MESSAGE_DATABASE_INIT, CONDITION_TYPE_DATABASE_INIT, SetConditionDatabaseInit},
		{CONDITION_MESSAGE_TRANSPORT_INIT, CONDITION_TYPE_TRANSPORT_INIT, SetConditionTransportInit},
		{CONDITION_MESSAGE_MANAGER_AVAILABLE, CONDITION_TYPE_MANAGER_AVAILABLE, SetConditionManagerAvailable},
		{CONDITION_MESSAGE_GRAFANA_AVAILABLE, CONDITION_TYPE_GRAFANA_AVAILABLE, SetConditionGrafanaAvailable},
	}

	for _, tc := range cases {
		t.Logf("testing condition %s ", tc.name)
		tc.setFunc(context.TODO(), runtimeClient, mgh, CONDITION_STATUS_TRUE)
		if condition := GetConditionStatus(mgh, tc.conditionType); condition != CONDITION_STATUS_TRUE {
			t.Errorf("expected condition %s to be %s, got %s", tc.name, CONDITION_STATUS_TRUE, condition)
		}
		tc.setFunc(context.TODO(), runtimeClient, mgh, CONDITION_STATUS_FALSE)
		if condition := GetConditionStatus(mgh, tc.conditionType); condition != CONDITION_STATUS_FALSE {
			t.Errorf("expected condition %s to be %s, got %s", tc.name, CONDITION_STATUS_FALSE, condition)
		}
		tc.setFunc(context.TODO(), runtimeClient, mgh, CONDITION_STATUS_UNKNOWN)
		if condition := GetConditionStatus(mgh, tc.conditionType); condition != CONDITION_STATUS_UNKNOWN {
			t.Errorf("expected condition %s to be %s, got %s", tc.name, CONDITION_STATUS_UNKNOWN, condition)
		}
	}

	leafHubTestCases := []struct {
		status string
	}{
		{CONDITION_STATUS_TRUE},
		{CONDITION_STATUS_FALSE},
		{CONDITION_STATUS_UNKNOWN},
	}

	for _, tc := range leafHubTestCases {
		t.Logf("testing condition %s : %s ", CONDITION_TYPE_LEAFHUB_DEPLOY, tc.status)
		err := SetConditionLeafHubDeployed(context.TODO(), runtimeClient, mgh, "test", metav1.ConditionStatus(tc.status))
		if err != nil {
			t.Errorf("failed to set %s condition: %v", CONDITION_TYPE_LEAFHUB_DEPLOY, err)
		}
		if condition := GetConditionStatus(mgh, CONDITION_TYPE_LEAFHUB_DEPLOY); condition !=
			metav1.ConditionStatus(tc.status) {
			t.Errorf("expected condition %s to be %s, got %s",
				CONDITION_TYPE_LEAFHUB_DEPLOY, tc.status, condition)
		}
	}
}
