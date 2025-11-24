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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WeftServerLocation defines the location of the Weft Server.
// +kubebuilder:validation:Enum=Internal;External
type WeftServerLocation string

const (
	// WeftServerLocationInternal means the server is running inside the cluster.
	WeftServerLocationInternal WeftServerLocation = "Internal"
	// WeftServerLocationExternal means the server is running outside the cluster.
	WeftServerLocationExternal WeftServerLocation = "External"
)

// WeftServerSpec defines the desired state of WeftServer
type WeftServerSpec struct {
	// ConnectionString is the connection string for the Weft Server.
	ConnectionString string `json:"connectionString,omitempty"`

	// Location specifies where the Weft Server is running.
	// Defaults to Internal.
	// +kubebuilder:default=Internal
	Location WeftServerLocation `json:"location,omitempty"`

	// BindInterface specifies the network interface to bind to.
	BindInterface string `json:"bindInterface,omitempty"`

	// UsageReportingURL specifies the URL for usage reporting.
	UsageReportingURL string `json:"usageReportingURL,omitempty"`

	// CloudflareTokenSecretRef specifies the secret containing the Cloudflare API token.
	CloudflareTokenSecretRef *corev1.SecretKeySelector `json:"cloudflareTokenSecretRef,omitempty"`
}

// TunnelStatus represents the status and metrics of a tunnel.
type TunnelStatus struct {
	Tx     uint64 `json:"tx,omitempty"`
	Rx     uint64 `json:"rx,omitempty"`
	SrcURL string `json:"srcURL,omitempty"`
	DstURL string `json:"dstURL,omitempty"`
}

// WeftServerStatus defines the observed state of WeftServer
type WeftServerStatus struct {
	// Tunnels is the list of active tunnels connected to this server.
	Tunnels []TunnelStatus `json:"tunnels,omitempty"`

	// Conditions represent the latest available observations of the WeftServer's current state.
	// +operator-sdk:channel-and-version-overrides:enable-channel=alpha,v1beta1=beta
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WeftServer is the Schema for the weftservers API
type WeftServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WeftServerSpec   `json:"spec,omitempty"`
	Status WeftServerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WeftServerList contains a list of WeftServer
type WeftServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WeftServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WeftServer{}, &WeftServerList{})
}
