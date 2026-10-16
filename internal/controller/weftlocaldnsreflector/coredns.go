// coredns.go — the cluster DNS half of a WeftLocalDNSReflector. The operator
// writes one *.server key into the CoreDNS custom ConfigMap (k3s's Corefile
// already does `import /etc/coredns/custom/*.server`, and the reload plugin
// picks up changes to imported files), declaring one server block per
// hostname that answers with the proxy Service IP. Other names under the
// hostname fall through to the normal resolver.
//
// A malformed block keeps CoreDNS from starting, which would be a
// cluster-wide DNS outage, so everything rendered here is re-validated in Go
// even though the CRD schema already constrains it.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/pki"
)

// hostnameRE mirrors the CRD's items pattern: a lowercase DNS name with at
// least two labels, no wildcards, nothing CoreDNS could read as syntax.
var hostnameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)+$`)

// renderCoreDNS returns the server blocks pointing hostnames at serviceIP.
func renderCoreDNS(reflectorName, serviceIP string, hostnames []string) (string, error) {
	ip := net.ParseIP(serviceIP)
	if ip == nil {
		return "", fmt.Errorf("service IP %q is not an IP address", serviceIP)
	}
	if len(hostnames) == 0 {
		return "", fmt.Errorf("no hostnames to render")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by weft-operator WeftLocalDNSReflector %q. Do not edit.\n", reflectorName)
	for _, h := range pki.SortedLower(hostnames) {
		if len(h) > 253 || !hostnameRE.MatchString(h) || net.ParseIP(h) != nil {
			return "", fmt.Errorf("hostname %q is not a valid DNS name", h)
		}
		fmt.Fprintf(&b, "%s:53 {\n", h)
		b.WriteString("    errors\n")
		b.WriteString("    cache 30\n")
		b.WriteString("    hosts {\n")
		fmt.Fprintf(&b, "        %s %s\n", ip.String(), h)
		b.WriteString("        ttl 30\n")
		b.WriteString("        fallthrough\n")
		b.WriteString("    }\n")
		b.WriteString("    forward . /etc/resolv.conf\n")
		b.WriteString("}\n")
	}
	return b.String(), nil
}

// syncCoreDNS writes content under this reflector's key in the CoreDNS custom
// ConfigMap, or removes the key when content is "". Reads go through the
// uncached API reader: the manager cache only holds reflector-labelled
// ConfigMaps and this one is shared. It returns whether the key is present.
func (r *Reconciler) syncCoreDNS(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector, content string) (bool, error) {
	logger := log.FromContext(ctx)
	key := types.NamespacedName{Namespace: refl.Spec.ClusterDNS.ConfigMapNamespace, Name: refl.Spec.ClusterDNS.ConfigMapName}
	dataKey := coreDNSKey(refl)

	cm := &corev1.ConfigMap{}
	err := r.APIReader.Get(ctx, key, cm)
	if apierrors.IsNotFound(err) {
		if content == "" {
			return false, nil
		}
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name, Labels: map[string]string{createdByLabel: createdByValue}},
			Data:       map[string]string{dataKey: content},
		}
		logger.Info("Creating CoreDNS custom ConfigMap", "configmap", key.String(), "key", dataKey)
		if err := r.Create(ctx, cm); err != nil {
			return false, fmt.Errorf("create %s: %w", key, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get %s: %w", key, err)
	}

	current, present := cm.Data[dataKey]
	switch {
	case content == "" && !present:
		return false, nil
	case content != "" && present && current == content:
		return true, nil
	}
	if content == "" {
		delete(cm.Data, dataKey)
		logger.Info("Removing CoreDNS override", "configmap", key.String(), "key", dataKey)
	} else {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[dataKey] = content
		logger.Info("Writing CoreDNS override", "configmap", key.String(), "key", dataKey)
	}
	// Update carries the resourceVersion we read, so a concurrent writer
	// to another key makes this fail with a conflict and requeue rather
	// than clobbering their change.
	if err := r.Update(ctx, cm); err != nil {
		return present, fmt.Errorf("update %s: %w", key, err)
	}
	return content != "", nil
}
