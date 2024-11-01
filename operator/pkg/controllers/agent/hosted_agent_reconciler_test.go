/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package agent

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/api/addon/v1alpha1"

	"github.com/stolostron/multicluster-global-hub/pkg/constants"
)

func TestHostedAddAddonConfig(t *testing.T) {
	tests := []struct {
		name      string
		cma       *v1alpha1.ClusterManagementAddOn
		expectCma *v1alpha1.ClusterManagementAddOn
		want      bool
	}{
		{
			name: "empty spec",
			cma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
			},
			expectCma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
				Spec: v1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: v1alpha1.InstallStrategy{
						Type: "Manual",
						Placements: []v1alpha1.PlacementStrategy{
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: constants.GHDefaultNamespace,
									Name:      "non-local-cluster",
								},
								Configs: []v1alpha1.AddOnConfig{
									{
										ConfigReferent: v1alpha1.ConfigReferent{
											Name:      "global-hub",
											Namespace: constants.GHDefaultNamespace,
										},
										ConfigGroupResource: v1alpha1.ConfigGroupResource{
											Group:    "addon.open-cluster-management.io",
											Resource: "addondeploymentconfigs",
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has config in spec",
			cma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
				Spec: v1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: v1alpha1.InstallStrategy{
						Type: "Manual",
						Placements: []v1alpha1.PlacementStrategy{
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: "ns",
									Name:      "pl",
								},
							},
						},
					},
				},
			},
			expectCma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
				Spec: v1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: v1alpha1.InstallStrategy{
						Type: "Manual",
						Placements: []v1alpha1.PlacementStrategy{
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: "ns",
									Name:      "pl",
								},
							},
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: constants.GHDefaultNamespace,
									Name:      "non-local-cluster",
								},
								Configs: []v1alpha1.AddOnConfig{
									{
										ConfigReferent: v1alpha1.ConfigReferent{
											Name:      "global-hub",
											Namespace: constants.GHDefaultNamespace,
										},
										ConfigGroupResource: v1alpha1.ConfigGroupResource{
											Group:    "addon.open-cluster-management.io",
											Resource: "addondeploymentconfigs",
										},
									},
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has needed config in spec",
			cma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
				Spec: v1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: v1alpha1.InstallStrategy{
						Type: "Manual",
						Placements: []v1alpha1.PlacementStrategy{
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: constants.GHDefaultNamespace,
									Name:      "non-local-cluster",
								},
								Configs: []v1alpha1.AddOnConfig{
									{
										ConfigReferent: v1alpha1.ConfigReferent{
											Name:      "global-hub",
											Namespace: constants.GHDefaultNamespace,
										},
										ConfigGroupResource: v1alpha1.ConfigGroupResource{
											Group:    "addon.open-cluster-management.io",
											Resource: "addondeploymentconfigs",
										},
									},
								},
							},
						},
					},
				},
			},
			expectCma: &v1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-manager",
					Namespace: "c1",
				},
				Spec: v1alpha1.ClusterManagementAddOnSpec{
					InstallStrategy: v1alpha1.InstallStrategy{
						Type: "Manual",
						Placements: []v1alpha1.PlacementStrategy{
							{
								PlacementRef: v1alpha1.PlacementRef{
									Namespace: constants.GHDefaultNamespace,
									Name:      "non-local-cluster",
								},
								Configs: []v1alpha1.AddOnConfig{
									{
										ConfigReferent: v1alpha1.ConfigReferent{
											Name:      "global-hub",
											Namespace: constants.GHDefaultNamespace,
										},
										ConfigGroupResource: v1alpha1.ConfigGroupResource{
											Group:    "addon.open-cluster-management.io",
											Resource: "addondeploymentconfigs",
										},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addAddonConfig(tt.cma)
			if got != tt.want {
				t.Errorf("addAddonConfig() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.expectCma.Spec.InstallStrategy.Placements, tt.cma.Spec.InstallStrategy.Placements) {
				t.Errorf("expectCma() = %v, want %v", tt.expectCma.Spec, tt.cma.Spec)
			}
		})
	}
}
