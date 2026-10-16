// reconciler_test.go — envtest coverage for the WeftLocalDNSReflector
// lifecycle: certificates and CA fan-out, the proxy Deployment/Service and
// node agent, the CoreDNS override being withheld until the proxy is
// available, reacting to tunnels and hostnames changing, and the finalizer
// withdrawing the override on delete.
package weftlocaldnsreflector_test

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/controller/weftlocaldnsreflector"
	"aquaduct.dev/weft-operator/internal/pki"
	"aquaduct.dev/weft-operator/internal/trust"
)

const (
	timeout  = 20 * time.Second
	interval = 200 * time.Millisecond
)

func ensureNamespace(name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := k8sClient.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func coreDNSData() map[string]string {
	cm := &corev1.ConfigMap{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-custom"}, cm); err != nil {
		return nil
	}
	return cm.Data
}

func condition(name, kind string) *metav1.Condition {
	refl := &weftv1alpha1.WeftLocalDNSReflector{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl); err != nil {
		return nil
	}
	return meta.FindStatusCondition(refl.Status.Conditions, kind)
}

var _ = Describe("WeftLocalDNSReflector", func() {
	const name = "lab0"
	coreDNSKey := "weft-local-dns-reflector-" + name + ".server"

	It("reflects tunnelled hostnames in-cluster and cleans up after itself", func() {
		for _, ns := range []string{"weft-system", "gitea", "kube-system"} {
			ensureNamespace(ns)
		}
		// A foreign key in the shared CoreDNS ConfigMap must survive.
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns-custom"},
			Data:       map[string]string{"someone-else.override": "log\n"},
		})).To(Succeed())

		tunnel := &weftv1alpha1.WeftTunnel{
			ObjectMeta: metav1.ObjectMeta{Namespace: "gitea", Name: "gw-gitea"},
			Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: "http://gitea-http.gitea.svc:3000", DstURL: "https://gitea.lab0.us/"},
		}
		Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

		refl := &weftv1alpha1.WeftLocalDNSReflector{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{"gitea.lab0.us", "unrouted.lab0.us"}},
		}
		Expect(k8sClient.Create(ctx, refl)).To(Succeed())

		By("applying CRD defaults")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl)).To(Succeed())
		Expect(refl.Spec.Namespace).To(Equal("weft-system"))
		Expect(refl.Spec.ClusterDNS.ConfigMapName).To(Equal("coredns-custom"))
		Expect(refl.Spec.NodeTrust.ContainerdCertsDir).To(Equal("/var/lib/rancher/k3s/agent/etc/containerd/certs.d"))

		By("issuing a serving certificate that chains to the CA and covers every spec hostname")
		caSecret, tlsSecret := &corev1.Secret{}, &corev1.Secret{}
		Eventually(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-ca"}, caSecret); err != nil {
				return err
			}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-tls"}, tlsSecret)
		}, timeout, interval).Should(Succeed())
		leaf := parse(tlsSecret.Data["tls.crt"])
		GinkgoWriter.Printf("serving cert SANs=%v notAfter=%s\n", leaf.DNSNames, leaf.NotAfter)
		Expect(leaf.DNSNames).To(ConsistOf("gitea.lab0.us", "unrouted.lab0.us"))
		roots := x509.NewCertPool()
		Expect(roots.AppendCertsFromPEM(caSecret.Data["tls.crt"])).To(BeTrue())
		_, err := leaf.Verify(x509.VerifyOptions{DNSName: "gitea.lab0.us", Roots: roots})
		Expect(err).NotTo(HaveOccurred())

		By("publishing the trust bundle (system roots + CA, never the key) in every namespace")
		caPEM := string(caSecret.Data["tls.crt"])
		caFingerprint := pki.Fingerprint(caSecret.Data["tls.crt"])
		for _, ns := range []string{"default", "gitea", "weft-system", "kube-system"} {
			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: trust.ConfigMapName}, cm)
			}, timeout, interval).Should(Succeed(), "trust bundle in %s", ns)
			Expect(cm.Data[trust.BundleKey]).To(ContainSubstring(caPEM))
			Expect(pki.CountCerts([]byte(cm.Data[trust.BundleKey]))).To(Equal(26))
			Expect(cm.Data[trust.CAsKey]).To(ContainSubstring(caPEM))
			Expect(cm.Data[trust.BundleKey] + cm.Data[trust.CAsKey]).NotTo(ContainSubstring("PRIVATE KEY"))
			Expect(cm.OwnerReferences).To(HaveLen(1))
		}

		By("running weft proxy with static TLS for the routed hostname only")
		dep := &appsv1.Deployment{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-proxy"}, dep)
		}, timeout, interval).Should(Succeed())
		args := dep.Spec.Template.Spec.Containers[0].Args
		GinkgoWriter.Printf("proxy args: %v\n", args)
		Expect(args).To(Equal([]string{
			"proxy", "--tls-cert=/etc/weft-local/tls/tls.crt", "--tls-key=/etc/weft-local/tls/tls.key",
			"http://gitea-http.gitea.svc:3000", "https://gitea.lab0.us:8443/",
		}))
		Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
		Expect(dep.Spec.Template.Annotations).To(HaveKey("weft.aquaduct.dev/cert-fingerprint"))
		firstFingerprint := dep.Spec.Template.Annotations["weft.aquaduct.dev/cert-fingerprint"]

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-proxy"}, svc)).To(Succeed())
		Expect(svc.Spec.ClusterIP).NotTo(BeEmpty())
		serviceIP := svc.Spec.ClusterIP

		By("deploying the node agent pointed at the Service IP")
		ds := &appsv1.DaemonSet{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-node-agent"}, ds)
		}, timeout, interval).Should(Succeed())
		agentArgs := ds.Spec.Template.Spec.Containers[0].Args
		GinkgoWriter.Printf("node agent args: %v\n", agentArgs)
		Expect(agentArgs).To(ContainElements("node-agent", "--service-ip="+serviceIP, "--hostnames=gitea.lab0.us"))
		Expect(ds.Spec.Template.Spec.HostNetwork).To(BeTrue())
		Expect(ds.Spec.Template.Spec.Containers[0].Image).To(Equal("ghcr.io/aquaduct-dev/weft-operator:test"))

		By("withholding the CoreDNS override while the proxy is unavailable")
		Eventually(func() string {
			c := condition(name, "DNSPublished")
			if c == nil {
				return ""
			}
			return c.Reason
		}, timeout, interval).Should(Equal("Withdrawn"))
		Consistently(coreDNSData, 2*time.Second, interval).ShouldNot(HaveKey(coreDNSKey))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl)).To(Succeed())
		Expect(refl.Status.UnroutedHostnames).To(Equal([]string{"unrouted.lab0.us"}))
		Expect(refl.Status.ServiceIP).To(Equal(serviceIP))
		Expect(refl.Status.Routes).To(HaveLen(1))

		By("holding DNS and the node agent back while a running pod does not trust the CA")
		ensureNamespace("apps")
		legacy := runningPod("apps", "legacy", nil)
		runningPod("weft-system", "excluded", nil)
		runningPod("apps", "opted-out", map[string]string{trust.OptOutKey: "false"})
		markAvailable(name)
		Eventually(func() string {
			if c := condition(name, "DNSPublished"); c != nil {
				return c.Reason
			}
			return ""
		}, timeout, interval).Should(Equal("WaitingForTrust"))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl)).To(Succeed())
		GinkgoWriter.Printf("untrusted=%d sample=%v\n", refl.Status.UntrustedPods, refl.Status.UntrustedPodSample)
		Expect(refl.Status.UntrustedPods).To(Equal(int32(1)))
		Expect(refl.Status.UntrustedPodSample).To(Equal([]string{"apps/legacy"}))
		Expect(coreDNSData()).NotTo(HaveKey(coreDNSKey))
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-node-agent"}, &appsv1.DaemonSet{}))
		}, timeout, interval).Should(BeTrue())

		By("publishing the override once every pod trusts the CA (as if restarted through the webhook)")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "apps", Name: "legacy"}, legacy)).To(Succeed())
		legacy.Annotations = map[string]string{trust.InjectedAnnotation: "someotherca," + caFingerprint}
		Expect(k8sClient.Update(ctx, legacy)).To(Succeed())
		Eventually(func() string { return coreDNSData()[coreDNSKey] }, timeout, interval).Should(And(
			ContainSubstring("gitea.lab0.us:53 {"),
			ContainSubstring(serviceIP+" gitea.lab0.us"),
			Not(ContainSubstring("unrouted.lab0.us")),
		))
		GinkgoWriter.Printf("coredns block:\n%s\n", coreDNSData()[coreDNSKey])
		Expect(coreDNSData()).To(HaveKey("someone-else.override"))
		Eventually(func() metav1.ConditionStatus {
			if c := condition(name, "Ready"); c != nil {
				return c.Status
			}
			return ""
		}, timeout, interval).Should(Equal(metav1.ConditionTrue))
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-node-agent"}, &appsv1.DaemonSet{})
		}, timeout, interval).Should(Succeed())

		By("reissuing the certificate and rolling the proxy when hostnames change")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl)).To(Succeed())
		refl.Spec.Hostnames = []string{"gitea.lab0.us"}
		Expect(k8sClient.Update(ctx, refl)).To(Succeed())
		Eventually(func() []string {
			s := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-tls"}, s); err != nil {
				return nil
			}
			return parse(s.Data["tls.crt"]).DNSNames
		}, timeout, interval).Should(Equal([]string{"gitea.lab0.us"}))
		Eventually(func() string {
			d := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-proxy"}, d)
			return d.Spec.Template.Annotations["weft.aquaduct.dev/cert-fingerprint"]
		}, timeout, interval).ShouldNot(Equal(firstFingerprint))

		By("publishing the trust bundle into namespaces created later")
		ensureNamespace("late-arrival")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: "late-arrival", Name: trust.ConfigMapName}, &corev1.ConfigMap{})
		}, timeout, interval).Should(Succeed())

		By("withdrawing DNS, scaling the proxy to zero and removing the node agent when the tunnel goes away")
		Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())
		Eventually(coreDNSData, timeout, interval).ShouldNot(HaveKey(coreDNSKey))
		Eventually(func() int32 {
			d := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-proxy"}, d)
			if d.Spec.Replicas == nil {
				return -1
			}
			return *d.Spec.Replicas
		}, timeout, interval).Should(Equal(int32(0)))
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-node-agent"}, &appsv1.DaemonSet{})
			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())

		By("re-publishing when the tunnel comes back, then withdrawing on delete")
		tunnel.ResourceVersion = ""
		Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())
		Eventually(func() string { return coreDNSData()[coreDNSKey] }, timeout, interval).Should(ContainSubstring("gitea.lab0.us"))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, refl)).To(Succeed())
		Expect(k8sClient.Delete(ctx, refl)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &weftv1alpha1.WeftLocalDNSReflector{}))
		}, timeout, interval).Should(BeTrue())
		Expect(coreDNSData()).NotTo(HaveKey(coreDNSKey))
		Expect(coreDNSData()).To(HaveKey("someone-else.override"))
	})

	It("produces injected pods the API server accepts", func() {
		ensureNamespace("weft-system")
		ensureNamespace("inject")
		caCert, _, err := pki.NewCA("inject-test", time.Now())
		Expect(err).NotTo(HaveOccurred())
		var roots []byte
		for range 25 {
			c, _, _ := pki.NewCA("root", time.Now())
			roots = append(roots, c...)
		}
		bundle, err := trust.Build(roots, []trust.CA{{Reflector: "x", PEM: caCert, Fingerprint: pki.Fingerprint(caCert)}})
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "inject", Name: "injected"},
			Spec: corev1.PodSpec{
				AutomountServiceAccountToken: ptrBool(false),
				InitContainers:               []corev1.Container{{Name: "init", Image: "busybox"}},
				Containers: []corev1.Container{
					{Name: "a", Image: "busybox"},
					{Name: "b", Image: "busybox",
						Env:          []corev1.EnvVar{{Name: "SSL_CERT_FILE", Value: "/x"}},
						VolumeMounts: []corev1.VolumeMount{{Name: "own", MountPath: "/etc/ssl"}}},
				},
				Volumes: []corev1.Volume{{Name: "own", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
			},
		}
		Expect(trust.InjectPod(pod, bundle)).To(BeTrue())
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		got := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "inject", Name: "injected"}, got)).To(Succeed())
		GinkgoWriter.Printf("accepted mounts for a: %v\n", got.Spec.Containers[0].VolumeMounts)
		Expect(got.Spec.Containers[0].VolumeMounts).To(HaveLen(len(trust.SystemBundlePaths)))
		Expect(got.Spec.Containers[0].Env).To(BeEmpty())
	})

	It("requires exactly one of hostnames or allHostnames", func() {
		for name, spec := range map[string]weftv1alpha1.WeftLocalDNSReflectorSpec{
			"neither": {},
			"both":    {AllHostnames: true, Hostnames: []string{"a.example.test"}},
		} {
			err := k8sClient.Create(ctx, &weftv1alpha1.WeftLocalDNSReflector{ObjectMeta: metav1.ObjectMeta{Name: "oneof-" + name}, Spec: spec})
			GinkgoWriter.Printf("%s -> %v\n", name, err)
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "%s should be rejected", name)
		}
	})

	It("keeps a cluster-default allHostnames reflector that yields to explicit claims", func() {
		ensureNamespace("weft-system")
		ensureNamespace("auto")
		Expect(k8sClient.Create(ctx, &weftv1alpha1.WeftTunnel{
			ObjectMeta: metav1.ObjectMeta{Namespace: "auto", Name: "gw-auto"},
			Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: "http://web.auto.svc:80", DstURL: "https://auto.example.test/"},
		})).To(Succeed())

		def := &weftlocaldnsreflector.DefaultReflector{Client: k8sClient, Enabled: true, Name: "cluster-default"}
		Expect(def.Sync(ctx)).To(Succeed())
		got := &weftv1alpha1.WeftLocalDNSReflector{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster-default"}, got)).To(Succeed())
		Expect(got.Spec.AllHostnames).To(BeTrue())
		Expect(got.Labels).To(HaveKeyWithValue(weftlocaldnsreflector.DefaultReflectorLabel, "true"))

		reflected := func(name string) func() []string {
			return func() []string {
				r := &weftv1alpha1.WeftLocalDNSReflector{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, r); err != nil {
					return nil
				}
				return r.Status.ReflectedHostnames
			}
		}
		By("claiming every tunnelled hostname")
		Eventually(reflected("cluster-default"), timeout, interval).Should(ContainElement("auto.example.test"))

		By("yielding a hostname another reflector lists explicitly, and reclaiming it afterwards")
		explicit := &weftv1alpha1.WeftLocalDNSReflector{
			ObjectMeta: metav1.ObjectMeta{Name: "explicit"},
			Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{"auto.example.test"}},
		}
		Expect(k8sClient.Create(ctx, explicit)).To(Succeed())
		Eventually(reflected("explicit"), timeout, interval).Should(Equal([]string{"auto.example.test"}))
		Eventually(reflected("cluster-default"), timeout, interval).ShouldNot(ContainElement("auto.example.test"))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster-default"}, got)).To(Succeed())
		Expect(got.Status.ConflictingHostnames).To(ContainElement("auto.example.test"))
		Expect(k8sClient.Delete(ctx, explicit)).To(Succeed())
		Eventually(reflected("cluster-default"), timeout, interval).Should(ContainElement("auto.example.test"))

		By("following configured hostnames")
		def.Hostnames = []string{"only.example.test"}
		Expect(def.Sync(ctx)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster-default"}, got)).To(Succeed())
		Expect(got.Spec.AllHostnames).To(BeFalse())
		Expect(got.Spec.Hostnames).To(Equal([]string{"only.example.test"}))

		By("deleting it when disabled")
		def.Enabled = false
		Expect(def.Sync(ctx)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster-default"}, &weftv1alpha1.WeftLocalDNSReflector{}))
		}, timeout, interval).Should(BeTrue())

		By("never touching a hand-made reflector with the default name")
		hand := &weftv1alpha1.WeftLocalDNSReflector{
			ObjectMeta: metav1.ObjectMeta{Name: "hand-made"},
			Spec:       weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{"hand.example.test"}},
		}
		Expect(k8sClient.Create(ctx, hand)).To(Succeed())
		for _, enabled := range []bool{false, true} {
			Expect((&weftlocaldnsreflector.DefaultReflector{Client: k8sClient, Enabled: enabled, Name: "hand-made"}).Sync(ctx)).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "hand-made"}, got)).To(Succeed())
			Expect(got.DeletionTimestamp).To(BeNil())
			Expect(got.Spec.AllHostnames).To(BeFalse())
			Expect(got.Spec.Hostnames).To(Equal([]string{"hand.example.test"}))
		}
		Expect(k8sClient.Delete(ctx, hand)).To(Succeed())
	})

	It("rejects hostnames CoreDNS could misread", func() {
		for _, bad := range []string{"*.lab0.us", "localhost", "gitea.lab0.us {", "UPPER.lab0.us"} {
			refl := &weftv1alpha1.WeftLocalDNSReflector{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-" + strings.ToLower(strings.Trim(strings.Map(func(r rune) rune {
					if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
						return r
					}
					return '-'
				}, bad), "-"))},
				Spec: weftv1alpha1.WeftLocalDNSReflectorSpec{Hostnames: []string{bad}},
			}
			err := k8sClient.Create(ctx, refl)
			GinkgoWriter.Printf("hostname %q -> %v\n", bad, err)
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "hostname %q should be rejected", bad)
		}
	})
})

// runningPod creates a pod and marks it Running (envtest has no kubelet).
func runningPod(ns, name string, labels map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: ptrBool(false),
			Containers:                   []corev1.Container{{Name: "c", Image: "busybox"}},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Phase = corev1.PodRunning
	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	return pod
}

func ptrBool(b bool) *bool { return &b }

// markAvailable fakes what the deployment controller would report once the
// proxy pod is up; envtest runs no controllers of its own.
func markAvailable(name string) {
	Eventually(func() error {
		d := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "weft-system", Name: name + "-proxy"}, d); err != nil {
			return err
		}
		d.Status.Replicas = 1
		d.Status.ReadyReplicas = 1
		d.Status.AvailableReplicas = 1
		return k8sClient.Status().Update(ctx, d)
	}, timeout, interval).Should(Succeed())
}

func parse(certPEM []byte) *x509.Certificate {
	block, _ := pem.Decode(certPEM)
	Expect(block).NotTo(BeNil())
	cert, err := x509.ParseCertificate(block.Bytes)
	Expect(err).NotTo(HaveOccurred())
	return cert
}
