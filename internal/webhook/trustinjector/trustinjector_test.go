// trustinjector_test.go — tests for the trust webhook against a fake
// client: the handler patches pods and publishes a missing bundle (but not
// on dry runs), and setup keeps a stable CA across restarts while writing
// the serving certificate and the MutatingWebhookConfiguration.
package trustinjector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/pki"
	"aquaduct.dev/weft-operator/internal/trust"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := weftv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// fixture returns a fake client holding one reflector with a CA Secret, and
// a trust source reading a generated system roots file.
func fixture(t *testing.T) (client.Client, *trust.Source, *runtime.Scheme) {
	t.Helper()
	scheme := newScheme(t)
	caPEM, _, err := pki.NewCA("lab0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var roots strings.Builder
	for range 25 {
		c, _, _ := pki.NewCA("root", time.Now())
		roots.Write(c)
	}
	rootsFile := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(rootsFile, []byte(roots.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&weftv1alpha1.WeftLocalDNSReflector{
			ObjectMeta: metav1.ObjectMeta{Name: "lab0", UID: "uid-lab0"},
			Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{"gitea.lab0.us"}, Namespace: "weft-system"},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "weft-system", Name: "lab0-ca"},
			Data:       map[string][]byte{corev1.TLSCertKey: caPEM},
		},
	).Build()
	return c, &trust.Source{Reader: c, SystemRootsFile: rootsFile}, scheme
}

func request(t *testing.T, pod *corev1.Pod, dryRun bool) admission.Request {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID:       "req",
		Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Operation: admissionv1.Create,
		Namespace: "apps",
		Object:    runtime.RawExtension{Raw: raw},
		DryRun:    &dryRun,
	}}
}

func TestHandlerInjectsAndPublishes(t *testing.T) {
	c, src, scheme := fixture(t)
	h := &Handler{Client: c, Source: src, Decoder: admission.NewDecoder(scheme)}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "web-"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}},
	}

	resp := h.Handle(context.Background(), request(t, pod, true))
	t.Logf("dry run: allowed=%v patches=%d", resp.Allowed, len(resp.Patches))
	if !resp.Allowed || len(resp.Patches) == 0 {
		t.Fatalf("dry run should still be patched")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "apps", Name: trust.ConfigMapName}, &corev1.ConfigMap{}); err == nil {
		t.Fatalf("dry run must not create the bundle ConfigMap")
	}

	resp = h.Handle(context.Background(), request(t, pod, false))
	if !resp.Allowed || len(resp.Patches) == 0 {
		t.Fatalf("expected an allowed, patched response: allowed=%v patches=%d", resp.Allowed, len(resp.Patches))
	}
	paths := []string{}
	for _, p := range resp.Patches {
		paths = append(paths, p.Operation+" "+p.Path)
	}
	t.Logf("patches: %v", paths)
	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "apps", Name: trust.ConfigMapName}, cm); err != nil {
		t.Fatalf("bundle ConfigMap not published on demand: %v", err)
	}
	if pki.CountCerts([]byte(cm.Data[trust.BundleKey])) != 26 || len(cm.OwnerReferences) != 1 {
		t.Errorf("unexpected bundle: %d certs, owners %v", pki.CountCerts([]byte(cm.Data[trust.BundleKey])), cm.OwnerReferences)
	}

	optOut := pod.DeepCopy()
	optOut.Labels = map[string]string{trust.OptOutKey: "false"}
	if resp := h.Handle(context.Background(), request(t, optOut, false)); !resp.Allowed || len(resp.Patches) != 0 {
		t.Errorf("opted-out pod was patched")
	}
}

func TestHandlerWithoutReflectors(t *testing.T) {
	scheme := newScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	h := &Handler{Client: c, Source: &trust.Source{Reader: c, SystemRootsFile: "/nonexistent"}, Decoder: admission.NewDecoder(scheme)}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "x"}}}}
	resp := h.Handle(context.Background(), request(t, pod, false))
	if !resp.Allowed || len(resp.Patches) != 0 {
		t.Errorf("with no reflectors pods must pass untouched: %+v", resp.Result)
	}
}

func TestSetup(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	cfg := Config{Namespace: "weft-system", ServiceName: "weft-operator-webhook", CertDir: t.TempDir(), ExcludedNamespaces: []string{"kube-system", "weft-system"}}
	ctx := context.Background()

	if err := Setup(ctx, c, cfg); err != nil {
		t.Fatal(err)
	}
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := c.Get(ctx, types.NamespacedName{Name: ConfigurationName}, mwc); err != nil {
		t.Fatal(err)
	}
	wh := mwc.Webhooks[0]
	t.Logf("webhook %s -> %s/%s%s failure=%s", wh.Name, wh.ClientConfig.Service.Namespace, wh.ClientConfig.Service.Name, *wh.ClientConfig.Service.Path, *wh.FailurePolicy)
	if *wh.FailurePolicy != admissionregistrationv1.Ignore || *wh.ClientConfig.Service.Path != Path {
		t.Errorf("unexpected webhook config")
	}
	if strings.Join(wh.NamespaceSelector.MatchExpressions[0].Values, ",") != "kube-system,weft-system" {
		t.Errorf("namespace exclusion missing: %v", wh.NamespaceSelector)
	}

	// The serving cert on disk must verify for the Service DNS name against
	// the caBundle the API server is given.
	certPEM, _ := os.ReadFile(filepath.Join(cfg.CertDir, "tls.crt"))
	leaf, err := pki.ParseCert(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if reason := pki.RenewalReason(certPEM, wh.ClientConfig.CABundle, []string{cfg.DNSName()}, time.Now()); reason != "" {
		t.Errorf("serving cert does not match caBundle: %s", reason)
	}
	if _, err := os.Stat(filepath.Join(cfg.CertDir, "tls.key")); err != nil {
		t.Errorf("key not written: %v", err)
	}

	// A restart must reuse the same CA and certificate.
	firstCA := string(wh.ClientConfig.CABundle)
	if err := Setup(ctx, c, cfg); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(ctx, types.NamespacedName{Name: ConfigurationName}, mwc)
	again, _ := os.ReadFile(filepath.Join(cfg.CertDir, "tls.crt"))
	if string(mwc.Webhooks[0].ClientConfig.CABundle) != firstCA || string(again) != string(certPEM) {
		t.Errorf("restart rotated the webhook CA or certificate")
	}

	// Inside the renewal window a new leaf is issued from the same CA.
	if _, err := EnsureServingCert(ctx, c, cfg, leaf.NotAfter.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	renewed, _ := os.ReadFile(filepath.Join(cfg.CertDir, "tls.crt"))
	if string(renewed) == string(certPEM) {
		t.Errorf("certificate not renewed inside the renewal window")
	}
}

// A ConfigMap named like the bundle but not managed by the operator must
// never be mounted: pods there are admitted unmodified (dry run or not) and
// the ConfigMap is left alone. The manager cache filters ConfigMaps on the
// managed label, so the handler's reader is modelled without it and the
// handler has to find it through the API (client).
func TestHandlerLeavesForeignConfigMapAlone(t *testing.T) {
	c, src, scheme := fixture(t)
	foreignCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: trust.ConfigMapName},
		Data:       map[string]string{"owner": "someone-else"},
	}
	if err := c.Create(context.Background(), foreignCM); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}}}

	for name, reader := range map[string]client.Reader{
		"uncached reader":      c,
		"label-filtered cache": filteredReader{Reader: c},
	} {
		h := &Handler{Client: c, Source: &trust.Source{Reader: reader, SystemRootsFile: src.SystemRootsFile}, Decoder: admission.NewDecoder(scheme)}
		for _, dryRun := range []bool{true, false} {
			resp := h.Handle(context.Background(), request(t, pod, dryRun))
			if !resp.Allowed || len(resp.Patches) != 0 {
				t.Errorf("%s, dryRun=%v: pod next to a foreign ConfigMap was patched (%d patches)", name, dryRun, len(resp.Patches))
			}
		}
	}
	got := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(foreignCM), got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 || got.Data["owner"] != "someone-else" || len(got.OwnerReferences) != 0 || len(got.Labels) != 0 {
		t.Errorf("foreign ConfigMap modified: %+v", got)
	}
}

// The reconciler may publish the bundle moments before the cache sees it:
// the handler's create then conflicts, and the pod must still be injected.
func TestHandlerInjectsWhenCacheLagsOwnBundle(t *testing.T) {
	c, src, scheme := fixture(t)
	own := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: "apps", Name: trust.ConfigMapName,
		Labels: map[string]string{trust.ManagedLabel: trust.ManagedValue},
	}}
	if err := c.Create(context.Background(), own); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Client: c, Source: &trust.Source{Reader: filteredReader{Reader: c, lagging: true}, SystemRootsFile: src.SystemRootsFile}, Decoder: admission.NewDecoder(scheme)}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}}}
	if resp := h.Handle(context.Background(), request(t, pod, false)); !resp.Allowed || len(resp.Patches) == 0 {
		t.Errorf("pod not injected while the cache lagged the operator's own bundle")
	}
}

// filteredReader hides ConfigMaps the way the manager cache does: those
// without the managed label (or, with lagging, every ConfigMap).
type filteredReader struct {
	client.Reader
	lagging bool
}

func (f filteredReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := f.Reader.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if cm, ok := obj.(*corev1.ConfigMap); ok && (f.lagging || cm.Labels[trust.ManagedLabel] != trust.ManagedValue) {
		return apierrors.NewNotFound(corev1.Resource("configmaps"), key.Name)
	}
	return nil
}
