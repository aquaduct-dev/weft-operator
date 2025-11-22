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

// AquaductTaaSSpec defines the desired state of AquaductTaaS
type AquaductTaaSSpec struct {
	// AccessToken is the long-lived access token for aquaduct.dev.
	AccessToken string `json:"accessToken,omitempty"`
}

// AquaductTaaSStatus defines the observed state of AquaductTaaS
type AquaductTaaSStatus struct {
	// Conditions represents the latest available observations of the AquaductTaaS's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AquaductTaaS is the Schema for the aquaducttaas API
type AquaductTaaS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AquaductTaaSSpec   `json:"spec,omitempty"`
	Status AquaductTaaSStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AquaductTaaSList contains a list of AquaductTaaS
type AquaductTaaSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AquaductTaaS `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AquaductTaaS{}, &AquaductTaaSList{})
}
