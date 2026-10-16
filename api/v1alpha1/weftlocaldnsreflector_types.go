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

// weftlocaldnsreflector_types.go — the WeftLocalDNSReflector API. A reflector
// keeps traffic for public weft hostnames inside the cluster: it runs an
// in-cluster `weft proxy` that terminates TLS with an operator-issued
// self-signed certificate, makes clients trust that certificate (a webhook
// injects the CA into every new pod; the node agent adds it to the host and
// containerd), points cluster DNS (CoreDNS) at the proxy, and points nodes
// at it so image pulls take the same short path.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WeftLocalDNSReflectorSpec defines the desired state of a WeftLocalDNSReflector.
// +kubebuilder:validation:XValidation:rule="(has(self.allHostnames) && self.allHostnames) != (has(self.hostnames) && size(self.hostnames) > 0)",message="set exactly one of hostnames or allHostnames"
type WeftLocalDNSReflectorSpec struct {
	// Hostnames are the public hostnames whose traffic should stay inside the
	// cluster. Each one is served using the backends of the WeftTunnels whose
	// dstURL has that host (which includes every tunnel a weft Gateway
	// generates), so routing is never configured twice. A hostname with no
	// matching WeftTunnel is left alone and reported in status. Mutually
	// exclusive with allHostnames.
	//+kubebuilder:validation:MaxItems=64
	//+kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)+$`
	//+kubebuilder:validation:items:MaxLength=253
	Hostnames []string `json:"hostnames,omitempty"`

	// AllHostnames reflects every http/https hostname served by a WeftTunnel
	// anywhere in the cluster, instead of an explicit list. A hostname that
	// another reflector lists explicitly is left to that reflector. The
	// operator's cluster-default reflector uses this mode.
	AllHostnames bool `json:"allHostnames,omitempty"`

	// Namespace holds the proxy Deployment, its Service, the node agent
	// DaemonSet and the CA/serving-certificate Secrets.
	//+kubebuilder:default=weft-system
	//+kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable"
	Namespace string `json:"namespace,omitempty"`

	// Replicas of the in-cluster proxy.
	//+kubebuilder:default=1
	//+kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// ClusterDNS controls the CoreDNS override that points pods at the proxy.
	ClusterDNS ClusterDNSSpec `json:"clusterDNS,omitempty"`

	// NodeTrust controls the per-node agent that lets containerd image pulls
	// use the proxy too.
	NodeTrust NodeTrustSpec `json:"nodeTrust,omitempty"`

	// IgnoreUntrustedPods publishes the DNS override even while running
	// pods exist that were created before the CA was injected (they will
	// fail TLS to the reflected hostnames until restarted). By default the
	// override waits until every such pod has been replaced.
	IgnoreUntrustedPods bool `json:"ignoreUntrustedPods,omitempty"`
}

// ClusterDNSSpec locates the CoreDNS "custom" ConfigMap whose *.server keys
// the cluster's Corefile imports (k3s ships exactly this hook).
type ClusterDNSSpec struct {
	// Disabled skips the CoreDNS override; pods keep resolving the public IP.
	Disabled bool `json:"disabled,omitempty"`

	// ConfigMapNamespace is where the CoreDNS custom ConfigMap lives.
	//+kubebuilder:default=kube-system
	ConfigMapNamespace string `json:"configMapNamespace,omitempty"`

	// ConfigMapName is the CoreDNS custom ConfigMap. The operator owns only
	// its own key in it and creates the ConfigMap if it is missing.
	//+kubebuilder:default=coredns-custom
	ConfigMapName string `json:"configMapName,omitempty"`
}

// NodeTrustSpec configures the node agent DaemonSet. On every node it adds
// the reflector CA to the host's CA bundle and to a containerd certs.d
// hosts.toml, and an /etc/hosts entry pointing the hostnames at the proxy
// Service. The hosts entry is only present while the proxy answers a
// verified TLS handshake, so an outage falls back to the public path instead
// of breaking pulls.
type NodeTrustSpec struct {
	// Disabled skips the node agent; image pulls keep using the public path.
	Disabled bool `json:"disabled,omitempty"`

	// ContainerdCertsDir is containerd's registry config_path on the host.
	//+kubebuilder:default=/var/lib/rancher/k3s/agent/etc/containerd/certs.d
	ContainerdCertsDir string `json:"containerdCertsDir,omitempty"`
}

// ReflectedRoute is one src→dst pair the proxy serves.
type ReflectedRoute struct {
	// Hostname is the public hostname this route answers for.
	Hostname string `json:"hostname"`
	// Source is the in-cluster backend URL, copied from the WeftTunnel.
	Source string `json:"source"`
	// Destination is the URL the proxy listens on (host, port and path).
	Destination string `json:"destination"`
	// Tunnel is the namespace/name of the WeftTunnel the route came from.
	Tunnel string `json:"tunnel"`
}

// WeftLocalDNSReflectorStatus defines the observed state of a WeftLocalDNSReflector.
type WeftLocalDNSReflectorStatus struct {
	// ObservedGeneration is the .metadata.generation most recently reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ServiceIP is the ClusterIP of the proxy Service that DNS points at.
	ServiceIP string `json:"serviceIP,omitempty"`

	// Routes are the src→dst pairs the proxy currently serves.
	Routes []ReflectedRoute `json:"routes,omitempty"`

	// UnroutedHostnames are spec hostnames with no matching WeftTunnel. They
	// get no DNS override and keep resolving publicly.
	UnroutedHostnames []string `json:"unroutedHostnames,omitempty"`

	// ReflectedHostnames are the hostnames this reflector currently claims:
	// its certificate covers them and it serves any that are routed.
	ReflectedHostnames []string `json:"reflectedHostnames,omitempty"`

	// ConflictingHostnames are hostnames this reflector would claim but
	// another reflector owns. An explicit list beats allHostnames, and among
	// explicit claims the oldest reflector wins, so two reflectors never
	// serve one hostname (CoreDNS rejects duplicate zones).
	ConflictingHostnames []string `json:"conflictingHostnames,omitempty"`

	// CABundleConfigMap is the trust bundle ConfigMap published in every
	// namespace and injected into new pods.
	CABundleConfigMap string `json:"caBundleConfigMap,omitempty"`

	// UntrustedPods counts running pods that do not trust this reflector's
	// CA (created before injection, or while the webhook was down). While
	// non-zero the DNS override and node agent are withheld, unless
	// spec.ignoreUntrustedPods is set. Restarting the pods clears it.
	UntrustedPods int32 `json:"untrustedPods,omitempty"`

	// UntrustedPodSample names up to 20 of those pods (namespace/name).
	UntrustedPodSample []string `json:"untrustedPodSample,omitempty"`

	// CertificateNotAfter is when the current serving certificate expires.
	// It is renewed automatically well before then.
	CertificateNotAfter *metav1.Time `json:"certificateNotAfter,omitempty"`

	// Conditions:
	//   CertificateReady — CA and serving certificate are issued and current
	//   ClientsTrustCA   — no running pod lacks the CA (or ignored by spec)
	//   ProxyAvailable   — the proxy Deployment has an available replica
	//   DNSPublished     — the CoreDNS override is written (or disabled)
	//   Ready            — all of the above
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName=wldr
//+kubebuilder:printcolumn:name="Service IP",type=string,JSONPath=`.status.serviceIP`
//+kubebuilder:printcolumn:name="All",type=boolean,JSONPath=`.spec.allHostnames`
//+kubebuilder:printcolumn:name="Untrusted Pods",type=integer,JSONPath=`.status.untrustedPods`
//+kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//+kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 40",message="name must be at most 40 characters so derived Service names stay valid DNS labels"

// WeftLocalDNSReflector keeps traffic for public weft hostnames inside the
// cluster instead of hairpinning out through the bastion and back through
// a weft tunnel.
type WeftLocalDNSReflector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WeftLocalDNSReflectorSpec   `json:"spec,omitempty"`
	Status WeftLocalDNSReflectorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WeftLocalDNSReflectorList contains a list of WeftLocalDNSReflector
type WeftLocalDNSReflectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WeftLocalDNSReflector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WeftLocalDNSReflector{}, &WeftLocalDNSReflectorList{})
}
