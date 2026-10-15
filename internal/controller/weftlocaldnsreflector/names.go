// names.go — object names and labels for everything a WeftLocalDNSReflector
// creates. Kept in one place so the reconciler, the cache filter in
// cmd/main.go and the tests agree on them.
package weftlocaldnsreflector

import (
	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/trust"
)

const (
	// ReflectorLabel is stamped on every object a reflector creates.
	ReflectorLabel = "weft.aquaduct.dev/reflector"

	componentLabel = "app.kubernetes.io/component"
	// nameLabel=nameValue is on every ConfigMap the operator creates; the
	// manager cache only watches ConfigMaps carrying it (see cmd/main.go).
	nameLabel      = trust.ManagedLabel
	nameValue      = trust.ManagedValue
	createdByLabel = "created-by"
	createdByValue = "weft-operator"

	// finalizer removes the reflector's key from the shared CoreDNS
	// ConfigMap, which the reflector cannot own via an ownerReference.
	finalizer = "weft.aquaduct.dev/cleanup-coredns"

	// certFingerprintAnnotation on the proxy pod template rolls the proxy
	// when the serving certificate is reissued (weft reads it once at start).
	certFingerprintAnnotation = "weft.aquaduct.dev/cert-fingerprint"

	// tlsMountPath / caMountPath are where the proxy and node agent see
	// the serving Secret and the trust bundle ConfigMap.
	tlsMountPath = "/etc/weft-local/tls"
	caMountPath  = "/etc/weft-local/ca"
	// hostEtcPath is where the node agent sees the node's /etc.
	hostEtcPath = "/host/etc"
)

func caSecretName(r *weftv1alpha1.WeftLocalDNSReflector) string  { return r.Name + "-ca" }
func tlsSecretName(r *weftv1alpha1.WeftLocalDNSReflector) string { return r.Name + "-tls" }
func proxyName(r *weftv1alpha1.WeftLocalDNSReflector) string     { return r.Name + "-proxy" }
func nodeAgentName(r *weftv1alpha1.WeftLocalDNSReflector) string { return r.Name + "-node-agent" }

// coreDNSKey is this reflector's key in the CoreDNS custom ConfigMap. The
// ".server" suffix is what the k3s Corefile's `import` picks up.
func coreDNSKey(r *weftv1alpha1.WeftLocalDNSReflector) string {
	return "weft-local-dns-reflector-" + r.Name + ".server"
}

func managedLabels(r *weftv1alpha1.WeftLocalDNSReflector, component string) map[string]string {
	return map[string]string{
		ReflectorLabel: r.Name,
		componentLabel: component,
		nameLabel:      nameValue,
		createdByLabel: createdByValue,
	}
}

// selectorLabels is the immutable subset used in Deployment/DaemonSet
// selectors and the Service selector.
func selectorLabels(r *weftv1alpha1.WeftLocalDNSReflector, component string) map[string]string {
	return map[string]string{ReflectorLabel: r.Name, componentLabel: component}
}
