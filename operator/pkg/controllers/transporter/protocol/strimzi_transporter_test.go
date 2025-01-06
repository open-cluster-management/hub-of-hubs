package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stolostron/multicluster-global-hub/operator/api/operator/v1alpha4"
	operatorconstants "github.com/stolostron/multicluster-global-hub/operator/pkg/constants"
	"github.com/stolostron/multicluster-global-hub/pkg/utils"
)

func TestNewStrimziTransporter(t *testing.T) {
	mgh := &v1alpha4.MulticlusterGlobalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mgh",
			Namespace: utils.GetDefaultNamespace(),
			Annotations: map[string]string{
				operatorconstants.CommunityCatalogSourceNameKey:      "test",
				operatorconstants.CommunityCatalogSourceNamespaceKey: "default",
			},
		},
		Spec: v1alpha4.MulticlusterGlobalHubSpec{
			DataLayerSpec: v1alpha4.DataLayerSpec{
				Postgres: v1alpha4.PostgresSpec{
					Retention: "2y",
				},
			},
		},
	}

	trans := NewStrimziTransporter(
		nil,
		mgh,
		WithCommunity(true),
		WithNamespacedName(types.NamespacedName{
			Name:      KafkaClusterName,
			Namespace: mgh.Namespace,
		}),
	)

	if trans.subCatalogSourceName != "test" {
		t.Errorf("catalogSource name should be test, but %v", trans.subCatalogSourceName)
	}

	if trans.subCatalogSourceNamespace != "default" {
		t.Errorf("catalogSource name should be default, but %v", trans.subCatalogSourceNamespace)
	}
}

func TestNewKafkaCluster(t *testing.T) {
	tests := []struct {
		name                 string
		mgh                  *v1alpha4.MulticlusterGlobalHub
		expectedKafkaCluster string
	}{
		{
			name: "availabilityConfig is Basic",
			mgh: &v1alpha4.MulticlusterGlobalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mgh",
					Namespace: utils.GetDefaultNamespace(),
					Annotations: map[string]string{
						operatorconstants.CommunityCatalogSourceNameKey:      "test",
						operatorconstants.CommunityCatalogSourceNamespaceKey: "default",
					},
				},
				Spec: v1alpha4.MulticlusterGlobalHubSpec{
					AvailabilityConfig: v1alpha4.HABasic,
					DataLayerSpec: v1alpha4.DataLayerSpec{
						Postgres: v1alpha4.PostgresSpec{
							Retention: "2y",
						},
					},
				},
			},
			expectedKafkaCluster: `{
    "metadata": {
        "name": "kafka",
        "namespace": "multicluster-global-hub",
        "creationTimestamp": null,
        "labels": {
            "global-hub.open-cluster-management.io/managed-by": "global-hub"
        },
        "annotations": {
            "strimzi.io/kraft": "enabled",
            "strimzi.io/node-pools": "enabled"
        }
    },
    "spec": {
        "entityOperator": {
            "topicOperator": {},
            "userOperator": {}
        },
        "kafka": {
            "authorization": {
                "type": "simple"
            },
            "config": {
                "default.replication.factor": 1,
                "min.insync.replicas": 1,
                "offsets.topic.replication.factor": 1,
                "transaction.state.log.min.isr": 1,
                "transaction.state.log.replication.factor": 1
            },
            "listeners": [
                {
                    "authentication": {
                        "type": "tls"
                    },
                    "name": "tls",
                    "port": 9093,
                    "tls": true,
                    "type": "route"
                }
            ],
            "resources": {
                "requests": {
                    "cpu": "25m",
                    "memory": "128Mi"
                }
            },
            "version": "3.8.0"
        }
    }
}`,
		},
		{
			name: "availabilityConfig is High",
			mgh: &v1alpha4.MulticlusterGlobalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mgh",
					Namespace: utils.GetDefaultNamespace(),
					Annotations: map[string]string{
						operatorconstants.CommunityCatalogSourceNameKey:      "test",
						operatorconstants.CommunityCatalogSourceNamespaceKey: "default",
					},
				},
				Spec: v1alpha4.MulticlusterGlobalHubSpec{
					DataLayerSpec: v1alpha4.DataLayerSpec{
						Postgres: v1alpha4.PostgresSpec{
							Retention: "2y",
						},
					},
				},
			},
			expectedKafkaCluster: `{
    "metadata": {
        "name": "kafka",
        "namespace": "multicluster-global-hub",
        "creationTimestamp": null,
        "labels": {
            "global-hub.open-cluster-management.io/managed-by": "global-hub"
        },
        "annotations": {
            "strimzi.io/kraft": "enabled",
            "strimzi.io/node-pools": "enabled"
        }
    },
    "spec": {
        "entityOperator": {
            "topicOperator": {},
            "userOperator": {}
        },
        "kafka": {
            "authorization": {
                "type": "simple"
            },
            "config": {
                "default.replication.factor": 3,
                "min.insync.replicas": 2,
                "offsets.topic.replication.factor": 3,
                "transaction.state.log.min.isr": 2,
                "transaction.state.log.replication.factor": 3
            },
            "listeners": [
                {
                    "authentication": {
                        "type": "tls"
                    },
                    "name": "tls",
                    "port": 9093,
                    "tls": true,
                    "type": "route"
                }
            ],
            "resources": {
                "requests": {
                    "cpu": "25m",
                    "memory": "128Mi"
                }
            },
            "version": "3.8.0"
        }
    }
}`,
		},

		{
			name: "availabilityConfig is Basic and expose via nodeport",
			mgh: &v1alpha4.MulticlusterGlobalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mgh",
					Namespace: utils.GetDefaultNamespace(),
					Annotations: map[string]string{
						operatorconstants.CommunityCatalogSourceNameKey:      "test",
						operatorconstants.CommunityCatalogSourceNamespaceKey: "default",
						operatorconstants.KafkaUseNodeport:                   "",
						operatorconstants.KinDClusterIPKey:                   "10.0.0.1",
					},
				},
				Spec: v1alpha4.MulticlusterGlobalHubSpec{
					DataLayerSpec: v1alpha4.DataLayerSpec{
						Postgres: v1alpha4.PostgresSpec{
							Retention: "2y",
						},
					},
				},
			},
			expectedKafkaCluster: `{
    "metadata": {
        "name": "kafka",
        "namespace": "multicluster-global-hub",
        "creationTimestamp": null,
        "labels": {
            "global-hub.open-cluster-management.io/managed-by": "global-hub"
        },
        "annotations": {
            "strimzi.io/kraft": "enabled",
            "strimzi.io/node-pools": "enabled"
        }
    },
    "spec": {
        "entityOperator": {
            "topicOperator": {},
            "userOperator": {}
        },
        "kafka": {
            "authorization": {
                "type": "simple"
            },
            "config": {
                "default.replication.factor": 3,
                "min.insync.replicas": 2,
                "offsets.topic.replication.factor": 3,
                "transaction.state.log.min.isr": 2,
                "transaction.state.log.replication.factor": 3
            },
            "listeners": [
                {
                    "authentication": {
                        "type": "tls"
                    },
                    "configuration": {
                        "bootstrap": {
                            "nodePort": 30093
                        },
                        "brokers": [
                            {
                                "advertisedHost": "10.0.0.1",
                                "broker": 0
                            }
                        ]
                    },
                    "name": "tls",
                    "port": 9093,
                    "tls": true,
                    "type": "nodeport"
                }
            ],
            "resources": {
                "requests": {
                    "cpu": "25m",
                    "memory": "128Mi"
                }
            },
            "version": "3.8.0"
        }
    }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transporter := NewStrimziTransporter(nil, tt.mgh)
			cluster := transporter.newKafkaCluster(tt.mgh)
			clusterBytes, _ := json.Marshal(cluster)
			// Replace spaces
			s := strings.ReplaceAll(tt.expectedKafkaCluster, " ", "")
			// Replace newlines
			s = strings.ReplaceAll(s, "\n", "")
			if string(clusterBytes) != s {
				t.Errorf("want %v, but got %v", s, string(clusterBytes))
			}
		})
	}
}
