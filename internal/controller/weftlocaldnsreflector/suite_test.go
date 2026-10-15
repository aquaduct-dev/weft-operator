// suite_test.go — envtest bootstrap for the WeftLocalDNSReflector controller.
// The manager is configured like cmd/main.go (label-filtered ConfigMap cache,
// uncached API reader) so the tests exercise the same read paths production
// does. envtest has no kube-controller-manager: nothing schedules pods or
// garbage-collects, so tests fake Deployment status where they need it.
package weftlocaldnsreflector_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/controller/weftlocaldnsreflector"
	"aquaduct.dev/weft-operator/internal/pki"
	"aquaduct.dev/weft-operator/internal/trust"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WeftLocalDNSReflector Controller")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(os.Getenv("TEST_SRCDIR"), "_main", "api", "v1alpha1", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(weftv1alpha1.AddToScheme(scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	req, err := labels.NewRequirement(trust.ManagedLabel, selection.Equals, []string{trust.ManagedValue})
	Expect(err).NotTo(HaveOccurred())
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: {Label: labels.NewSelector().Add(*req)},
		}},
	})
	Expect(err).NotTo(HaveOccurred())

	// A stand-in for the operator image's system roots.
	var roots strings.Builder
	for range 25 {
		c, _, err := pki.NewCA("root", time.Now())
		Expect(err).NotTo(HaveOccurred())
		roots.Write(c)
	}
	rootsFile := filepath.Join(GinkgoT().TempDir(), "roots.pem")
	Expect(os.WriteFile(rootsFile, []byte(roots.String()), 0o644)).To(Succeed())

	Expect((&weftlocaldnsreflector.Reconciler{
		Client:              mgr.GetClient(),
		APIReader:           mgr.GetAPIReader(),
		Scheme:              mgr.GetScheme(),
		WeftImage:           "ghcr.io/aquaduct-dev/weft:test",
		OperatorImage:       "ghcr.io/aquaduct-dev/weft-operator:test",
		Trust:               &trust.Source{Reader: mgr.GetClient(), SystemRootsFile: rootsFile},
		ExcludedNamespaces:  []string{"kube-system", "weft-system"},
		TrustRecheck:        time.Second,
		TrustBlockedRecheck: time.Second,
	}).SetupWithManager(mgr)).To(Succeed())

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
