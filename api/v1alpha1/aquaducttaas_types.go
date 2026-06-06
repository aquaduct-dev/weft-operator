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

// AquaductTaaSSpec defines the desired state of AquaductTaaS.
//
// An AquaductTaaS object represents a connection between the cluster and the
// user's hosted aquaduct.dev account. The reconciler reads the access token
// from the referenced Secret, calls api.aquaduct.dev on the user's behalf, and
// materializes any cloud-hosted bastions as WeftServer objects with
// Location=External so the rest of the operator can treat them uniformly.
type AquaductTaaSSpec struct {
	// AccessTokenSecretRef references a Secret holding the long-lived authz
	// access token (created under authz "My Identity → Long-Lived Tokens",
	// scoped to aquaduct.use). The operator exchanges it at
	// {AUTHZ_ENDPOINT}/api/auth/access-token for a short-lived JWT and uses
	// that JWT against the aquaduct.dev API. AUTHZ_ENDPOINT is operator-level
	// config (env on the Deployment), defaulting to https://authz.aquaduct.dev.
	AccessTokenSecretRef *corev1.SecretKeySelector `json:"accessTokenSecretRef,omitempty"`

	// APIEndpoint optionally overrides the aquaduct.dev API endpoint.
	// Defaults to https://api.aquaduct.dev when empty.
	APIEndpoint string `json:"apiEndpoint,omitempty"`
}

// BastionInfo is a snapshot of one of the caller's bastions as
// reported by aquaduct.dev. The AquaductTaaS reconciler publishes the
// full list on its status so other reconcilers (notably DNSRecord) can
// compute IP/bastion-association decisions without re-listing the API.
type BastionInfo struct {
	// ID is the opaque bastion identifier. Stable across the bastion's
	// lifetime; matches what the user references in
	// DNSRecord.spec.targetBastionIDs and what the server expects in
	// the bastion_ids field of /domain/{name}.
	ID string `json:"id"`

	// Name is the human-friendly name. Mirrors the WeftServer object
	// name for the External-located mirror.
	Name string `json:"name,omitempty"`

	// IP is the bastion's externally-routable IPv4 address. Used as the
	// expected A-record value when this bastion is part of a domain's
	// fan-out set.
	IP string `json:"ip,omitempty"`

	// Suspended indicates the bastion isn't currently routing traffic.
	// Suspended bastions are excluded from "fan out to all" defaults
	// but stay in the list so explicit targetBastionIDs references
	// don't break.
	Suspended bool `json:"suspended,omitempty"`
}

// AquaductTaaSStatus defines the observed state of AquaductTaaS
type AquaductTaaSStatus struct {
	// Conditions represents the latest available observations of the AquaductTaaS's current state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncTime is the last time the reconciler successfully synced with aquaduct.dev.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// SyncedServers lists the names of WeftServer objects currently mirrored from aquaduct.dev.
	SyncedServers []string `json:"syncedServers,omitempty"`

	// Bastions is the full list of bastions the caller's token has
	// access to, as of the most recent successful sync. Distinct from
	// SyncedServers in that it includes ID + IP + suspended state, and
	// is the canonical source of truth for downstream reconcilers
	// computing fan-out / expected-IP decisions.
	Bastions []BastionInfo `json:"bastions,omitempty"`
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
