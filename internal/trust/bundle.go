// Package trust makes workloads trust the CAs of WeftLocalDNSReflectors
// without any change to the workloads themselves.
//
// bundle.go builds the shared trust bundle (the operator image's system
// roots plus every reflector CA) and publishes it as the ConfigMap
// weft-trust-bundle in a namespace. inject.go mounts that ConfigMap into
// pods at creation time (via the trust-injection webhook).
package trust

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/pki"
)

const (
	// ConfigMapName is the per-namespace trust bundle ConfigMap.
	ConfigMapName = "weft-trust-bundle"
	// BundleKey holds system roots plus reflector CAs: a drop-in
	// replacement for a distro's ca-certificates.crt.
	BundleKey = "ca-certificates.crt"
	// CAsKey holds only the reflector CAs (read by the node agent).
	CAsKey = "weft-ca.crt"

	// ManagedLabel/ManagedValue mark every ConfigMap the operator caches
	// (cmd/main.go filters the ConfigMap informer on it).
	ManagedLabel = "app.kubernetes.io/name"
	ManagedValue = "weft-local-dns-reflector"

	// minSystemRoots guards against publishing a bundle built from a
	// missing or truncated system roots file: mounted over the system
	// bundle, it would break every TLS connection in every injected pod.
	minSystemRoots = 20

	// DefaultSystemRootsFile is the system bundle in the (alpine) operator image.
	DefaultSystemRootsFile = "/etc/ssl/certs/ca-certificates.crt"
)

// CA is one reflector's CA certificate.
type CA struct {
	Reflector   string
	PEM         []byte
	Fingerprint string
}

// Bundle is the content of the trust bundle ConfigMap.
type Bundle struct {
	// All is system roots followed by the reflector CAs.
	All []byte
	// CAs is only the reflector CAs.
	CAs []byte
	// Fingerprints of the CAs, sorted; recorded on injected pods.
	Fingerprints []string
}

// Empty reports whether there is no reflector CA to trust.
func (b Bundle) Empty() bool { return len(b.Fingerprints) == 0 }

// Build assembles a bundle. It refuses system roots that do not look like a
// real CA bundle.
func Build(systemRoots []byte, cas []CA) (Bundle, error) {
	if n := pki.CountCerts(systemRoots); n < minSystemRoots {
		return Bundle{}, fmt.Errorf("system roots contain %d certificates (want at least %d); refusing to build a trust bundle that would replace them", n, minSystemRoots)
	}
	sorted := append([]CA(nil), cas...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Reflector < sorted[j].Reflector })

	var all, only bytes.Buffer
	all.Write(bytes.TrimRight(systemRoots, "\n"))
	all.WriteString("\n")
	var fps []string
	for _, ca := range sorted {
		header := fmt.Sprintf("# weft-operator WeftLocalDNSReflector %s\n", ca.Reflector)
		all.WriteString(header)
		all.Write(ca.PEM)
		only.WriteString(header)
		only.Write(ca.PEM)
		fps = append(fps, ca.Fingerprint)
	}
	sort.Strings(fps)
	return Bundle{All: all.Bytes(), CAs: only.Bytes(), Fingerprints: fps}, nil
}

// Source computes the current bundle from the cluster. The reconciler and
// the webhook share one so they always agree.
type Source struct {
	// Reader reads reflectors and their CA Secrets (the manager cache).
	Reader client.Reader
	// SystemRootsFile is read on every Build so an image update is picked up.
	SystemRootsFile string
}

// CAs returns the CA of every reflector that is not being deleted and whose
// CA Secret exists. The CA Secret is "<name>-ca" in the reflector namespace.
func (s *Source) CAs(ctx context.Context) ([]CA, error) {
	var list weftv1alpha1.WeftLocalDNSReflectorList
	if err := s.Reader.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list reflectors: %w", err)
	}
	var out []CA
	for _, refl := range list.Items {
		if !refl.DeletionTimestamp.IsZero() {
			continue
		}
		ns := refl.Spec.Namespace
		if ns == "" {
			ns = "weft-system"
		}
		secret := &corev1.Secret{}
		err := s.Reader.Get(ctx, types.NamespacedName{Namespace: ns, Name: refl.Name + "-ca"}, secret)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get CA of reflector %s: %w", refl.Name, err)
		}
		certPEM := secret.Data[corev1.TLSCertKey]
		if _, err := pki.ParseCert(certPEM); err != nil {
			return nil, fmt.Errorf("CA of reflector %s: %w", refl.Name, err)
		}
		out = append(out, CA{Reflector: refl.Name, PEM: certPEM, Fingerprint: pki.Fingerprint(certPEM)})
	}
	return out, nil
}

// Build returns the current bundle.
func (s *Source) Build(ctx context.Context) (Bundle, error) {
	cas, err := s.CAs(ctx)
	if err != nil {
		return Bundle{}, err
	}
	if len(cas) == 0 {
		return Bundle{}, nil
	}
	path := s.SystemRootsFile
	if path == "" {
		path = DefaultSystemRootsFile
	}
	roots, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("read system roots %s: %w", path, err)
	}
	return Build(roots, cas)
}

// EnsureConfigMap writes bundle into the namespace's trust bundle ConfigMap.
// owners are the reflectors whose CAs it carries; the ConfigMap is
// garbage-collected once none of them remain.
func EnsureConfigMap(ctx context.Context, c client.Client, namespace string, bundle Bundle, owners []metav1.OwnerReference) (controllerutil.OperationResult, error) {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: ConfigMapName}}
	return controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels[ManagedLabel] = ManagedValue
		cm.Labels["app.kubernetes.io/component"] = "trust-bundle"
		cm.Data = map[string]string{BundleKey: string(bundle.All), CAsKey: string(bundle.CAs)}
		cm.OwnerReferences = owners
		return nil
	})
}

// CreateConfigMapIfMissing publishes bundle in namespace only if the
// ConfigMap does not exist yet; updates are left to the reconciler so the
// two never fight over its content or owners. It reports whether it created.
func (s *Source) CreateConfigMapIfMissing(ctx context.Context, c client.Client, namespace string, bundle Bundle) (bool, error) {
	err := s.Reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ConfigMapName}, &corev1.ConfigMap{})
	if err == nil {
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	var list weftv1alpha1.WeftLocalDNSReflectorList
	if err := s.Reader.List(ctx, &list); err != nil {
		return false, err
	}
	cas, err := s.CAs(ctx)
	if err != nil {
		return false, err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            ConfigMapName,
			Labels:          map[string]string{ManagedLabel: ManagedValue, "app.kubernetes.io/component": "trust-bundle"},
			OwnerReferences: OwnerRefs(list.Items, cas),
		},
		Data: map[string]string{BundleKey: string(bundle.All), CAsKey: string(bundle.CAs)},
	}
	if err := c.Create(ctx, cm); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// OwnerRefs returns owner references to the reflectors in cas.
func OwnerRefs(list []weftv1alpha1.WeftLocalDNSReflector, cas []CA) []metav1.OwnerReference {
	have := map[string]bool{}
	for _, ca := range cas {
		have[ca.Reflector] = true
	}
	var refs []metav1.OwnerReference
	for _, r := range list {
		if !have[r.Name] {
			continue
		}
		refs = append(refs, metav1.OwnerReference{
			APIVersion: weftv1alpha1.GroupVersion.String(),
			Kind:       "WeftLocalDNSReflector",
			Name:       r.Name,
			UID:        r.UID,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// FingerprintList renders fingerprints as the injected-pod annotation value.
func FingerprintList(fps []string) string { return strings.Join(fps, ",") }
