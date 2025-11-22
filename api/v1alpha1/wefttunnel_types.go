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

// WeftTunnelSpec defines the desired state of WeftTunnel
type WeftTunnelSpec struct {
	// TargetServers is a list of WeftServer names this tunnel should connect to.
	// If empty, it connects to all available WeftServers.
	TargetServers []string `json:"targetServers,omitempty"`

	// SrcURL is the source URL for the tunnel.
	SrcURL string `json:"srcURL,omitempty"`

	// DstURL is the destination URL for the tunnel.
	DstURL string `json:"dstURL,omitempty"`
}

// WeftTunnelStatus defines the observed state of WeftTunnel
type WeftTunnelStatus struct {
	// Conditions represents the latest available observations of the WeftTunnel's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WeftTunnel is the Schema for the wefttunnels API
type WeftTunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WeftTunnelSpec   `json:"spec,omitempty"`
	Status WeftTunnelStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WeftTunnelList contains a list of WeftTunnel
type WeftTunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WeftTunnel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WeftTunnel{}, &WeftTunnelList{})
}
