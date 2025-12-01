package wefttunnel_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	wefttunnel "aquaduct.dev/weft-operator/internal/controller/wefttunnel"
)

var _ = Describe("WeftTunnel Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a WeftTunnel", func() {
		It("Should create Deployments for all servers when TargetServers is empty", func(ctx context.Context) {
			By("Creating WeftServers")
			server1 := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "server-1", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "weft://user:pass@0.0.0.0:9090",
				},
			}
			Expect(k8sClient.Create(ctx, server1)).To(Succeed())

			server2 := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "server-2", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationExternal,
					ConnectionString: "weft://external-host:8080",
				},
			}
			Expect(k8sClient.Create(ctx, server2)).To(Succeed())

			By("Creating a WeftTunnel")
			tunnel := &weftv1alpha1.WeftTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tunnel-all",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftTunnelSpec{
					TargetServers: []string{}, // Empty means all
					SrcURL:        "http://src1",
					DstURL:        "http://dst1",
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Reconciling")
			r := &wefttunnel.WeftTunnelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment for server-1")
			dep1 := &appsv1.Deployment{}
			depName1 := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, server1.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depName1, Namespace: "default"}, dep1)
			}, timeout, interval).Should(Succeed())
			
			// Check URL construction for Internal
			// Expected: weft://user:pass@server-1-server.default.svc:9090
			// Original: weft://user:pass@0.0.0.0:9090
			expectedURL1 := "weft://user:pass@0.0.0.0:9090"
			Expect(dep1.Spec.Template.Spec.Containers[0].Args).To(ContainElement(expectedURL1))
			Expect(dep1.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--tunnel-name=" + tunnel.Name))
			Expect(dep1.Spec.Template.Spec.Containers[0].Args).To(ContainElement("http://src1"))
			Expect(dep1.Spec.Template.Spec.Containers[0].Args).To(ContainElement("http://dst1"))
			Expect(dep1.Spec.Template.Spec.HostNetwork).To(BeTrue())

			By("Checking Deployment for server-2")
			dep2 := &appsv1.Deployment{}
			depName2 := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, server2.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depName2, Namespace: "default"}, dep2)
			}, timeout, interval).Should(Succeed())

			// Check URL construction for External (should be unchanged)
			Expect(dep2.Spec.Template.Spec.Containers[0].Args).To(ContainElement("weft://external-host:8080"))
		})

		It("Should create Deployments only for specified TargetServers", func(ctx context.Context) {
			By("Creating WeftServers")
			// Reuse existing servers from previous test or create new ones?
			// Better to use unique names to avoid collision if tests run in parallel (though Ginkgo usually runs serial unless configured otherwise)
			
			targetSrv := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "target-server", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "weft://u:p@0.0.0.0:80",
				},
			}
			Expect(k8sClient.Create(ctx, targetSrv)).To(Succeed())

			ignoredSrv := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "ignored-server", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "weft://u:p@0.0.0.0:80",
				},
			}
			Expect(k8sClient.Create(ctx, ignoredSrv)).To(Succeed())

			By("Creating a WeftTunnel with specific target")
			tunnel := &weftv1alpha1.WeftTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tunnel-specific",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftTunnelSpec{
					TargetServers: []string{"target-server"},
					SrcURL:        "http://src2",
					DstURL:        "http://dst2",
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Reconciling")
			r := &wefttunnel.WeftTunnelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment for target-server")
			dep := &appsv1.Deployment{}
			depName := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, targetSrv.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depName, Namespace: "default"}, dep)
			}, timeout, interval).Should(Succeed())

			By("Ensuring Deployment for ignored-server does NOT exist")
			ignoredDepName := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, ignoredSrv.Name)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ignoredDepName, Namespace: "default"}, &appsv1.Deployment{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("Should prune obsolete deployments when TargetServers changes", func(ctx context.Context) {
			By("Creating WeftServers")
			srvA := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "server-a", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{ConnectionString: "weft://a@0:0"},
			}
			Expect(k8sClient.Create(ctx, srvA)).To(Succeed())

			srvB := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "server-b", Namespace: "default"},
				Spec: weftv1alpha1.WeftServerSpec{ConnectionString: "weft://b@0:0"},
			}
			Expect(k8sClient.Create(ctx, srvB)).To(Succeed())

			By("Creating WeftTunnel targeting A")
			tunnel := &weftv1alpha1.WeftTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tunnel-prune",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftTunnelSpec{
					TargetServers: []string{"server-a"},
					SrcURL:        "http://src3",
					DstURL:        "http://dst3",
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			r := &wefttunnel.WeftTunnelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			
			By("Reconciling (A)")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			// Verify A exists
			depNameA := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, srvA.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depNameA, Namespace: "default"}, &appsv1.Deployment{})
			}, timeout, interval).Should(Succeed())

			By("Updating WeftTunnel to target B instead")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}, tunnel); err != nil {
					return err
				}
				tunnel.Spec.TargetServers = []string{"server-b"}
				return k8sClient.Update(ctx, tunnel)
			}, timeout, interval).Should(Succeed())

			By("Reconciling (B)")
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verify B exists")
			depNameB := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, srvB.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depNameB, Namespace: "default"}, &appsv1.Deployment{})
			}, timeout, interval).Should(Succeed())

			By("Verify A is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: depNameA, Namespace: "default"}, &appsv1.Deployment{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("Should create Deployments for servers in weft-system namespace", func(ctx context.Context) {
			// Ensure weft-system namespace exists
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "weft-system"},
			}
			// Ignore error if it already exists
			_ = k8sClient.Create(ctx, ns)

			By("Creating WeftServer in weft-system")
			sysSrv := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{Name: "sys-server", Namespace: "weft-system"},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "weft://sys:pass@0.0.0.0:9090",
				},
			}
			Expect(k8sClient.Create(ctx, sysSrv)).To(Succeed())

			By("Creating WeftTunnel in default namespace")
			tunnel := &weftv1alpha1.WeftTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tunnel-cross-ns",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftTunnelSpec{
					TargetServers: []string{}, // Empty means all (should include system)
					SrcURL:        "http://src4",
					DstURL:        "http://dst4",
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Reconciling")
			r := &wefttunnel.WeftTunnelReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment for sys-server")
			depName := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, sysSrv.Name)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: depName, Namespace: "default"}, &appsv1.Deployment{})
			}, timeout, interval).Should(Succeed())
		})
	})
})
