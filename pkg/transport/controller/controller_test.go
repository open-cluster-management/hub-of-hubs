package controller

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stolostron/multicluster-global-hub/pkg/transport"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

func TestSecretCtrlReconcile(t *testing.T) {
	// Set up a fake Kubernetes client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	callbackInvoked := false

	secretController := &TransportCtrl{
		secretNamespace: "default",
		secretName:      "test-secret",
		transportConfig: &transport.TransportConfig{
			TransportType:   string(transport.Chan),
			ConsumerGroupId: "test",
			KafkaCredential: &transport.KafkaConnCredential{
				SpecTopic:   "spec",
				StatusTopic: "status",
			},
		},
		callback: func(p transport.Producer, c transport.Consumer) error {
			callbackInvoked = true
			return nil
		},
		runtimeClient: fakeClient,
	}

	ctx := context.TODO()

	kafkaConn := &transport.KafkaConnCredential{
		BootstrapServer: "localhost:3031",
		StatusTopic:     "event",
		SpecTopic:       "spec",
		ClusterID:       "123",
		// the following fields are only for the manager, and the agent of byo/standalone kafka
		CACert:     base64.StdEncoding.EncodeToString([]byte("11")),
		ClientCert: base64.StdEncoding.EncodeToString([]byte("12")),
		ClientKey:  base64.StdEncoding.EncodeToString([]byte("13")),
	}

	kafkaConnYaml, err := kafkaConn.YamlMarshal(false)
	assert.NoError(t, err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-secret",
		},
		Data: map[string][]byte{
			"kafka.yaml": kafkaConnYaml,
		},
	}
	_ = fakeClient.Create(ctx, secret)

	// Reconcile
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-secret",
		},
	}
	result, err := secretController.Reconcile(ctx, req)
	assert.NoError(t, err)
	assert.False(t, result.Requeue)
	assert.NotNil(t, secretController.producer)
	assert.NotNil(t, secretController.consumer)
	assert.True(t, callbackInvoked)

	// Test when transport config changes
	result, err = secretController.Reconcile(ctx, req)
	assert.NoError(t, err)
	assert.False(t, result.Requeue)
	utils.PrettyPrint(secretController.transportConfig.RestfulCredentail)
}

func TestInventorySecretCtrlReconcile(t *testing.T) {
	// Set up a fake Kubernetes client
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	callbackInvoked := false

	secretController := &TransportCtrl{
		secretNamespace: "default",
		secretName:      "test-secret",
		transportConfig: &transport.TransportConfig{},
		callback: func(p transport.Producer, c transport.Consumer) error {
			callbackInvoked = true
			return nil
		},
		runtimeClient: fakeClient,
	}

	ctx := context.TODO()

	restfulConn := &transport.RestfulConnCredentail{
		Host: "localhost:123",
		// the following fields are only for the manager, and the agent of byo/standalone kafka
		CACert:     base64.StdEncoding.EncodeToString(rootPEM),
		ClientCert: base64.StdEncoding.EncodeToString(certPem),
		ClientKey:  base64.StdEncoding.EncodeToString(keyPem),
	}

	restfulConnYaml, err := restfulConn.YamlMarshal()
	assert.NoError(t, err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-secret",
		},
		Data: map[string][]byte{
			"rest.yaml": restfulConnYaml,
		},
	}
	_ = fakeClient.Create(ctx, secret)

	// Reconcile
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-secret",
		},
	}
	result, err := secretController.Reconcile(ctx, req)
	assert.NoError(t, err)
	assert.False(t, result.Requeue)
	assert.NotNil(t, secretController.producer)
	assert.Nil(t, secretController.consumer)
	assert.True(t, callbackInvoked)
	assert.Equal(t, string(transport.Rest), secretController.transportConfig.TransportType)

	// Test when transport config changes
	result, err = secretController.Reconcile(ctx, req)
	assert.NoError(t, err)
	assert.False(t, result.Requeue)
	utils.PrettyPrint(secretController.transportConfig.RestfulCredentail)
}

var rootPEM = []byte(`
-- GlobalSign Root R2, valid until Dec 15, 2021
-----BEGIN CERTIFICATE-----
MIIDujCCAqKgAwIBAgILBAAAAAABD4Ym5g0wDQYJKoZIhvcNAQEFBQAwTDEgMB4G
A1UECxMXR2xvYmFsU2lnbiBSb290IENBIC0gUjIxEzARBgNVBAoTCkdsb2JhbFNp
Z24xEzARBgNVBAMTCkdsb2JhbFNpZ24wHhcNMDYxMjE1MDgwMDAwWhcNMjExMjE1
MDgwMDAwWjBMMSAwHgYDVQQLExdHbG9iYWxTaWduIFJvb3QgQ0EgLSBSMjETMBEG
A1UEChMKR2xvYmFsU2lnbjETMBEGA1UEAxMKR2xvYmFsU2lnbjCCASIwDQYJKoZI
hvcNAQEBBQADggEPADCCAQoCggEBAKbPJA6+Lm8omUVCxKs+IVSbC9N/hHD6ErPL
v4dfxn+G07IwXNb9rfF73OX4YJYJkhD10FPe+3t+c4isUoh7SqbKSaZeqKeMWhG8
eoLrvozps6yWJQeXSpkqBy+0Hne/ig+1AnwblrjFuTosvNYSuetZfeLQBoZfXklq
tTleiDTsvHgMCJiEbKjNS7SgfQx5TfC4LcshytVsW33hoCmEofnTlEnLJGKRILzd
C9XZzPnqJworc5HGnRusyMvo4KD0L5CLTfuwNhv2GXqF4G3yYROIXJ/gkwpRl4pa
zq+r1feqCapgvdzZX99yqWATXgAByUr6P6TqBwMhAo6CygPCm48CAwEAAaOBnDCB
mTAOBgNVHQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUm+IH
V2ccHsBqBt5ZtJot39wZhi4wNgYDVR0fBC8wLTAroCmgJ4YlaHR0cDovL2NybC5n
bG9iYWxzaWduLm5ldC9yb290LXIyLmNybDAfBgNVHSMEGDAWgBSb4gdXZxwewGoG
3lm0mi3f3BmGLjANBgkqhkiG9w0BAQUFAAOCAQEAmYFThxxol4aR7OBKuEQLq4Gs
J0/WwbgcQ3izDJr86iw8bmEbTUsp9Z8FHSbBuOmDAGJFtqkIk7mpM0sYmsL4h4hO
291xNBrBVNpGP+DTKqttVCL1OmLNIG+6KYnX3ZHu01yiPqFbQfXf5WRDLenVOavS
ot+3i9DAgBkcRcAtjOj4LaR0VknFBbVPFd5uRHg5h6h+u/N5GJG79G+dwfCMNYxd
AfvDbbnvRG15RjF+Cv6pgsH/76tuIMRQyV+dTZsXjAzlAcmgQWpzU/qlULRuJQ/7
TBj0/VLZjmmx6BEP3ojY+x1J96relc8geMJgEtslQIxq/H5COEBkEveegeGTLg==
-----END CERTIFICATE-----`)

var certPem = []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`)

var keyPem = []byte("-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49\nAwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q\nEKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==\n-----END EC PRIVATE KEY-----") // notsecret
