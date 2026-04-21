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

// DNSRecordSpec defines the desired state of a domain registration on
// aquaduct.dev. The reconciler mirrors this spec to the PUT /domain/{name}
// endpoint and cleans up via DELETE /domain/{name} when the record is
// deleted, even if the registration pre-existed (the operator is allowed
// to clobber but must always clean up).
type DNSRecordSpec struct {
	// DomainName is the fully-qualified domain to register (e.g.
	// "kate.oarm.io"). Immutable — rename by creating a new DNSRecord and
	// deleting the old one so the finalizer can release the original name.
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:MinLength=1
	//+kubebuilder:validation:XValidation:rule="self == oldSelf",message="domainName is immutable"
	DomainName string `json:"domainName"`

	// AquaductTaaSRef identifies the AquaductTaaS in the same namespace
	// whose access token + API endpoint are used to talk to aquaduct.dev.
	//+kubebuilder:validation:Required
	AquaductTaaSRef corev1.LocalObjectReference `json:"aquaductTaaSRef"`
}

// DNSRecordStatus defines the observed state of a DNSRecord.
type DNSRecordStatus struct {
	// ObservedGeneration is the .metadata.generation most recently
	// reconciled. Clients check this to know whether status fields apply
	// to the current spec.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions:
	//   Registered — aquaduct.dev confirms the record exists under our control
	//   Resolved   — GET /domain/lookup returned at least one A record
	//   Ready      — Registered && Resolved
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DomainID is the server-assigned identifier of the record. Populated
	// after the first successful GET or PUT. Cleared on re-registration
	// when the server reports the record no longer exists.
	DomainID string `json:"domainID,omitempty"`

	// ResolvedIPs is the latest A-record lookup result from
	// GET /domain/lookup. Empty if the lookup hasn't succeeded yet.
	ResolvedIPs []string `json:"resolvedIPs,omitempty"`

	// ClobberedPreexisting is true when, on the first reconcile that
	// observed server-side state, the record already existed. The
	// finalizer still deletes on teardown — tracking this flag lets
	// operators distinguish "we created this" from "we took this over".
	ClobberedPreexisting bool `json:"clobberedPreexisting,omitempty"`

	// LastSyncTime is when the reconciler last round-tripped with
	// aquaduct.dev (regardless of outcome). Useful for spotting silently
	// stuck reconciles.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DNSRecord is a tenant-scoped declaration that a domain should be
// registered at aquaduct.dev under this cluster's AquaductTaaS account.
type DNSRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSRecordSpec   `json:"spec,omitempty"`
	Status DNSRecordStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DNSRecordList contains a list of DNSRecord
type DNSRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSRecord `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DNSRecord{}, &DNSRecordList{})
}
