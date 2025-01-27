package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/stolostron/multicluster-global-hub/operator/pkg/utils"
	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

const (
	mergedAlertName   = "multicluster-global-hub-alerting"
	defaultAlertName  = "multicluster-global-hub-default-alerting"
	alertConfigMapKey = "alerting.yaml"

	mergedGrafanaIniName  = "multicluster-global-hub-grafana-config"
	defaultGrafanaIniName = "multicluster-global-hub-default-grafana-config"
	grafanaIniKey         = "grafana.ini"

	grafanaDeploymentName = "multicluster-global-hub-grafana"
)

var _ = Describe("The alert configmap should be created", Ordered, Label("e2e-test-grafana"), func() {
	It("Merged alert configmap should be same as default configmap", func() {
		Eventually(func() error {
			defaultAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, defaultAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, mergedAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			equal, err := utils.IsAlertGPCcountEqual([]byte(defaultAlertConfigMap.Data[alertConfigMapKey]), []byte(mergedAlertConfigMap.Data[alertConfigMapKey]))
			if err != nil {
				return err
			}
			if !equal {
				return fmt.Errorf("mergedAlert %v is not equal with default %v",
					mergedAlertConfigMap.Data, defaultAlertConfigMap.Data[alertConfigMapKey])
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("Merged alert configmap should merged default and custom configmap", func() {
		customConfig := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testOptions.GlobalHub.Namespace,
				Name:      constants.CustomAlertName,
			},
			Data: map[string]string{
				alertConfigMapKey: `
apiVersion: 1
contactPoints:
- name: alerts-cu-webhook
  orgId: 1
  receivers:
  - disableResolveMessage: false
    type: email
    uid: 4e3bfe25-00cf-4173-b02b-16f077e539da
groups:
- folder: Policy
  name: Suspicious Cluster Compliance Status Change
  orgId: 1
- folder: Custom
  name: Suspicious policy change
  orgId: 1
- folder: Custom
  name: Suspicious Cluster Compliance Status Change
  orgId: 1
policies:
- orgId: 1
  receiver: alerts-cu-webhook`,
			},
		}

		_, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Create(ctx, customConfig, metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() error {
			defaultAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, defaultAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			customAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, constants.CustomAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, mergedAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			dg, dp, dc, err := utils.GetAlertGPCcount([]byte(defaultAlertConfigMap.Data[alertConfigMapKey]))
			Expect(err).ShouldNot(HaveOccurred())

			cg, cp, cc, err := utils.GetAlertGPCcount([]byte(customAlertConfigMap.Data[alertConfigMapKey]))
			Expect(err).ShouldNot(HaveOccurred())

			mg, mp, mc, err := utils.GetAlertGPCcount([]byte(mergedAlertConfigMap.Data[alertConfigMapKey]))
			Expect(err).ShouldNot(HaveOccurred())

			if mg == dg+cg && mp == dp+cp && mc == dc+cc {
				return nil
			}
			return fmt.Errorf("mergedAlert is not equal with default and custom. default: %v, custom: %v, merged: %v",
				defaultAlertConfigMap.Data[alertConfigMapKey], customAlertConfigMap.Data, mergedAlertConfigMap.Data)
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		err = testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Delete(ctx, constants.CustomAlertName, metav1.DeleteOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() error {
			defaultAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, defaultAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, mergedAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			equal, err := utils.IsAlertGPCcountEqual([]byte(defaultAlertConfigMap.Data[alertConfigMapKey]), []byte(mergedAlertConfigMap.Data[alertConfigMapKey]))
			if err != nil {
				return err
			}
			if equal {
				return nil
			}

			return fmt.Errorf("mergedAlert %v is not equal with default %v.",
				mergedAlertConfigMap.Data, defaultAlertConfigMap.Data)
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("Merged alert configmap should be same as default when custom alert is invalid", func() {
		customConfig := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testOptions.GlobalHub.Namespace,
				Name:      constants.CustomAlertName,
			},
			Data: map[string]string{
				alertConfigMapKey: `
- name: alerts-cu-webhook
	orgId: 1
  receivers:
  - disableResolveMessage: false
	type: email
	uid: 4e3bfe25-00cf-4173-b02b-16f077e539da`,
			},
		}

		_, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Create(ctx, customConfig, metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() error {
			defaultAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, defaultAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedAlertConfigMap, err := testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Get(ctx, mergedAlertName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			equal, err := utils.IsAlertGPCcountEqual([]byte(defaultAlertConfigMap.Data[alertConfigMapKey]), []byte(mergedAlertConfigMap.Data[alertConfigMapKey]))
			if err != nil {
				return err
			}

			if equal {
				return nil
			}

			return fmt.Errorf("mergedAlert %v is not equal with default %v",
				mergedAlertConfigMap.Data, defaultAlertConfigMap.Data[alertConfigMapKey])
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		err = testClients.KubeClient().CoreV1().ConfigMaps(testOptions.GlobalHub.Namespace).Delete(ctx, constants.CustomAlertName, metav1.DeleteOptions{})
		Expect(err).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("The grafana.ini should be created", Ordered, Label("e2e-test-grafana"), func() {
	It("Merged grafana.ini should be same as default configmap", func() {
		ctx := context.Background()
		Eventually(func() error {
			defaultGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, defaultGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, mergedGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			if sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]) != sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]) {
				return fmt.Errorf("Default grafana.ini is different with merged grafana.ini. Default grafana.ini : %v, merged grafana.ini: %v",
					sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]), sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]))
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("Merged grafana.ini should merged default and custom grafana.ini", func() {
		ctx := context.Background()
		customSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testOptions.GlobalHub.Namespace,
				Name:      constants.CustomGrafanaIniName,
			},
			Data: map[string][]byte{
				grafanaIniKey: []byte(`
    [smtp]
    user = true
    pass = true
    [auth]
    enabled = false
    [dataproxy]
    timeout = 300
    dial_timeout = 30
    keep_alive_seconds = 300
`),
			},
		}
		customSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Create(ctx, customSecret, metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() error {
			defaultGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, defaultGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, mergedGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			if (sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]) + sectionCount(customSecret.Data[grafanaIniKey]) - 2) != sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]) {
				return fmt.Errorf("Default and custom grafana.ini secret is different with merged grafana.ini secret. Default : %v, custom: %v, merged: %v",
					sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]), sectionCount(customSecret.Data[grafanaIniKey]), sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]))
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		err = testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Delete(ctx, constants.CustomGrafanaIniName, metav1.DeleteOptions{})
		Expect(err).ShouldNot(HaveOccurred())

		Eventually(func() error {
			defaultGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, defaultGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			mergedGrafanaIniSecret, err := testClients.KubeClient().CoreV1().Secrets(testOptions.GlobalHub.Namespace).Get(ctx, mergedGrafanaIniName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			if sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]) != sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]) {
				return fmt.Errorf("Default grafana.ini is different with merged grafana.ini. Default grafana.ini : %v, merged grafana.ini: %v",
					sectionCount(defaultGrafanaIniSecret.Data[grafanaIniKey]), sectionCount(mergedGrafanaIniSecret.Data[grafanaIniKey]))
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})
})

func sectionCount(a []byte) int {
	cfg, err := ini.Load(a)
	if err != nil {
		return -1
	}
	// By Default, There is a DEFAULT section, should not count it
	return len(cfg.Sections()) - 1
}

var _ = Describe("The grafana resources counts should be right", Ordered, Label("e2e-test-grafana"), func() {
	It("The grafana default alert rules count should be ok", func() {
		ctx := context.Background()
		Eventually(func() error {
			alertCount, err := getGrafanaResource(ctx, "v1/provisioning/alert-rules")
			if err != nil {
				klog.Errorf("Get grafana resource error:%v", err)
				return err
			}
			if alertCount != 5 {
				klog.Errorf("get alert count:%v", alertCount)
				return fmt.Errorf("Expect alert count is 5, bug got:%v", alertCount)
			}
			return nil
		}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
	})

	It("The grafana dashboard should be exist", func() {
		ctx := context.Background()
		dashboardUids := []string{
			"pAqtIGj4k",                            // adhoc investigation
			"868845a4d1334958bd62303c5ccb4c19",     // clustergroup compliance overview
			"0e0ddb7f16b946f99d96a483a4a3f95f",     // offending clusters
			"b67e0727891f4121ae2dde09671520ae",     // offending policies
			"fbf66c88-ba14-4553-90b7-55ea5870faab", // overview
			"9bb3bee6a17e47f9a231f6d77f2408fa",     // policy group compliance overview
			"5a3a577af7894943aa6e7ca8408502fb",     // what's changed cluster
			"5a3a577af7894943aa6e7ca8408502fa",     // what's changed policies
		}
		for _, uid := range dashboardUids {
			Eventually(func() error {
				_, err := getGrafanaResource(ctx, "dashboards/uid/"+uid)
				return err
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
		}
	})
})

// getGrafanaResource return the resource number and error
func getGrafanaResource(ctx context.Context, path string) (int, error) {
	configNamespace := testOptions.GlobalHub.Namespace

	labelSelector := fmt.Sprintf("name=%s", grafanaDeploymentName)
	var grafanaPod corev1.Pod
	Eventually(func() error {
		poList, err := testClients.KubeClient().CoreV1().Pods(configNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return err
		}

		klog.Infof("Get grafana path:%v", path)
		if len(poList.Items) == 0 {
			return fmt.Errorf("Can not get grafana pod")
		}
		if len(poList.Items[0].Status.PodIP) == 0 {
			return fmt.Errorf("Can not get grafana pod IP")
		}
		grafanaPod = poList.Items[0]
		return nil
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

	url := "http://" + grafanaPod.Status.PodIP + ":3001/api/" + path
	klog.Infof("Sending request: %v", url)

	responseBody, err := testClients.Kubectl(testOptions.GlobalHub.Name,
		"exec",
		"-it",
		grafanaPod.Name,
		"-n",
		configNamespace,
		"-c",
		"grafana",
		"--",
		"/usr/bin/curl",
		"-H",
		"Content-Type: application/json",
		"-H",
		"X-Forwarded-User: WHAT_YOU_ARE_DOING_IS_VOIDING_SUPPORT_0000000000000000000000000000000000000000000000000000000000000000",
		url)
	if err != nil {
		klog.Errorf("responseBody: %v, err: %v", string(responseBody), err)
		return 0, err
	}

	var mapObj map[string]interface{}
	var arrayObj []interface{}

	if len(responseBody) == 0 {
		return 0, nil
	}

	err = yaml.Unmarshal([]byte(responseBody), &mapObj)
	if err == nil {
		return 1, nil
	}

	err = yaml.Unmarshal([]byte(responseBody), &arrayObj)
	if err == nil {
		return len(arrayObj), nil
	}

	return 0, err
}
