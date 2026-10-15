// trust_test.go — unit tests for the trust bundle (refusing bad system
// roots, deterministic content) and the pod injection (mounts only, env left
// alone, respect for the container's own mounts, idempotence, opt-out).
package trust

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"aquaduct.dev/weft-operator/internal/pki"
)

// FakeRoots returns a PEM bundle with n self-signed CAs, standing in for a
// distro's ca-certificates.crt.
func fakeRoots(t *testing.T, n int) []byte {
	t.Helper()
	var b strings.Builder
	for range n {
		c, _, err := pki.NewCA("root", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		b.Write(c)
	}
	return []byte(b.String())
}

func testCA(t *testing.T, name string) CA {
	t.Helper()
	c, _, err := pki.NewCA(name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return CA{Reflector: name, PEM: c, Fingerprint: pki.Fingerprint(c)}
}

func TestBuild(t *testing.T) {
	roots := fakeRoots(t, minSystemRoots)
	if _, err := Build(fakeRoots(t, 3), []CA{testCA(t, "a")}); err == nil {
		t.Fatalf("a 3-certificate system bundle must be refused")
	}
	b, a := testCA(t, "b"), testCA(t, "a")
	bundle, err := Build(roots, []CA{b, a})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bundle: %d bytes, %d certs, fingerprints %v", len(bundle.All), pki.CountCerts(bundle.All), bundle.Fingerprints)
	if got := pki.CountCerts(bundle.All); got != minSystemRoots+2 {
		t.Errorf("All has %d certs, want %d", got, minSystemRoots+2)
	}
	if got := pki.CountCerts(bundle.CAs); got != 2 {
		t.Errorf("CAs has %d certs, want 2", got)
	}
	if !strings.HasPrefix(string(bundle.All), strings.TrimRight(string(roots), "\n")) {
		t.Errorf("system roots must come first, unmodified")
	}
	if strings.Index(string(bundle.CAs), "Reflector a") > strings.Index(string(bundle.CAs), "Reflector b") {
		t.Errorf("CAs not ordered by reflector name")
	}
	again, _ := Build(roots, []CA{a, b})
	if string(again.All) != string(bundle.All) {
		t.Errorf("bundle depends on input order")
	}
}

func testPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers: []corev1.Container{
				{Name: "plain"},
				{Name: "custom", Env: []corev1.EnvVar{{Name: "SSL_CERT_FILE", Value: "/mine.pem"}},
					VolumeMounts: []corev1.VolumeMount{{Name: "certs", MountPath: "/etc/ssl/certs/"}}},
			},
		},
	}
}

func mountPaths(c corev1.Container) map[string]string {
	out := map[string]string{}
	for _, m := range c.VolumeMounts {
		out[m.MountPath] = m.SubPath
	}
	return out
}

func envMap(c corev1.Container) map[string]string {
	out := map[string]string{}
	for _, e := range c.Env {
		out[e.Name] = e.Value
	}
	return out
}

func TestInjectPod(t *testing.T) {
	bundle, err := Build(fakeRoots(t, minSystemRoots), []CA{testCA(t, "lab0")})
	if err != nil {
		t.Fatal(err)
	}
	pod := testPod()
	if !InjectPod(pod, bundle) {
		t.Fatalf("expected a change")
	}
	t.Logf("volumes=%v annotations=%v", pod.Spec.Volumes, pod.Annotations)
	if len(pod.Spec.Volumes) != 1 || pod.Spec.Volumes[0].ConfigMap.Name != ConfigMapName {
		t.Errorf("bundle volume not added: %v", pod.Spec.Volumes)
	}
	if !TrustsCA(pod.Annotations, bundle.Fingerprints[0]) {
		t.Errorf("injected annotation missing the CA fingerprint")
	}

	for _, c := range []corev1.Container{pod.Spec.InitContainers[0], pod.Spec.Containers[0]} {
		mounts := mountPaths(c)
		t.Logf("%s mounts=%v env=%v", c.Name, mounts, c.Env)
		for _, p := range SystemBundlePaths {
			if mounts[p] != BundleKey {
				t.Errorf("%s: %s not overlaid with the bundle", c.Name, p)
			}
		}
		if len(c.Env) != 0 {
			t.Errorf("%s: env must be left untouched, got %v", c.Name, c.Env)
		}
	}

	custom := pod.Spec.Containers[1]
	mounts, env := mountPaths(custom), envMap(custom)
	t.Logf("custom mounts=%v env=%v", mounts, env)
	if _, ok := mounts["/etc/ssl/certs/ca-certificates.crt"]; ok {
		t.Errorf("must not overlay a path under the container's own /etc/ssl/certs mount")
	}
	if mounts["/etc/pki/tls/certs/ca-bundle.crt"] != BundleKey {
		t.Errorf("unrelated paths should still be overlaid")
	}
	if len(env) != 1 || env["SSL_CERT_FILE"] != "/mine.pem" {
		t.Errorf("container env changed: %v", env)
	}

	before := pod.DeepCopy()
	if InjectPod(pod, bundle) {
		t.Errorf("second injection changed the pod (not idempotent)")
	}
	if len(pod.Spec.Containers[0].VolumeMounts) != len(before.Spec.Containers[0].VolumeMounts) {
		t.Errorf("mounts duplicated on reinjection")
	}

	// A new reflector CA changes only the annotation.
	bigger, _ := Build(fakeRoots(t, minSystemRoots), []CA{testCA(t, "lab0"), testCA(t, "other")})
	if !InjectPod(pod, bigger) || !strings.Contains(pod.Annotations[InjectedAnnotation], ",") {
		t.Errorf("annotation not updated for an added CA: %q", pod.Annotations[InjectedAnnotation])
	}
}

func TestInjectSkips(t *testing.T) {
	bundle, _ := Build(fakeRoots(t, minSystemRoots), []CA{testCA(t, "lab0")})

	optOut := testPod()
	optOut.Annotations = map[string]string{OptOutKey: "false"}
	if InjectPod(optOut, bundle) {
		t.Errorf("opted-out pod was injected")
	}
	win := testPod()
	win.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}
	if InjectPod(win, bundle) {
		t.Errorf("windows pod was injected")
	}
	if InjectPod(testPod(), Bundle{}) {
		t.Errorf("empty bundle injected")
	}
	rootMount := testPod()
	rootMount.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "host", MountPath: "/"}}
	InjectPod(rootMount, bundle)
	for _, m := range rootMount.Spec.Containers[0].VolumeMounts {
		if m.SubPath != "" {
			t.Errorf("overlaid %s under a container that mounts /", m.MountPath)
		}
	}
}

func TestTrustsCA(t *testing.T) {
	ann := map[string]string{InjectedAnnotation: "aaa,bbb"}
	if !TrustsCA(ann, "bbb") || TrustsCA(ann, "ccc") || TrustsCA(nil, "aaa") {
		t.Errorf("TrustsCA wrong")
	}
}
