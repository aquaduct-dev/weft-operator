/*
Copyright 2025.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WeftGatewaySpec defines the desired state of WeftGateway
type WeftGatewaySpec struct {
	// TargetServers is a list of WeftServer names that tunnels should use.
	// If empty, tunnels will connect to all available WeftServers.
	TargetServers []string `json:"targetServers,omitempty"`
}

// WeftGatewayStatus defines the observed state of WeftGateway
type WeftGatewayStatus struct {
	// Conditions represents the latest available observations of the WeftGateway's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WeftGateway is the Schema for the weftgateways API.
// It is used as a parametersRef for GatewayClass to configure Weft-specific settings.
type WeftGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WeftGatewaySpec   `json:"spec,omitempty"`
	Status WeftGatewayStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WeftGatewayList contains a list of WeftGateway
type WeftGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WeftGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WeftGateway{}, &WeftGatewayList{})
}
