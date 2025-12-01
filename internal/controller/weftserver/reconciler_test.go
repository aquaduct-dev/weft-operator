package weftserver_test

import (
	"context"
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
	weftserver "aquaduct.dev/weft-operator/internal/controller/weftserver" // Import weftserver package for WeftServerReconciler and WeftClient
	weftclient "github.com/aquaduct-dev/weft/src/client"
)

// mockClient implements weftserver.WeftClient
type mockClient struct {
	tunnels map[string]weftclient.TunnelInfo
	err     error
}

func (m *mockClient) ListTunnels() (map[string]weftclient.TunnelInfo, error) {
	return m.tunnels, m.err
}

var _ = Describe("WeftServer Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a WeftServer", func() {
		It("Should create a Deployment and Service for Internal location", func(ctx context.Context) {
			By("Creating a WeftServer")
			ws := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-internal",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "http://localhost:8081",
				},
			}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			By("Reconciling")
			// Create reconciler with mock client factory
			r := &weftserver.WeftServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				ClientFactory: func(url, tunnelName string) (weftserver.WeftClient, error) {
					return &mockClient{
						tunnels: map[string]weftclient.TunnelInfo{
							"tunnel1": {
								Tx:     100,
								Rx:     200,
								SrcURL: "src",
								DstURL: "dst",
							},
						},
					}, nil
				},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-server-internal",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment")
			dep := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-internal-server", Namespace: "default"}, dep)
			}, timeout, interval).Should(Succeed())
			Expect(dep.Spec.Template.Spec.HostNetwork).To(BeTrue())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("ghcr.io/aquaduct-dev/weft:latest"))
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("weft"))
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("server"))
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("--bind-ip=0.0.0.0"))
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("--certs-cache-path=/var/lib/weft/certs"))
			Expect(dep.Spec.Template.Spec.Volumes).To(ContainElement(HaveField("Name", "certs")))
			Expect(dep.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(HaveField("MountPath", "/var/lib/weft/certs")))

			By("Checking Service")
			svc := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-internal-server", Namespace: "default"}, svc)
			}, timeout, interval).Should(Succeed())
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8081)))

			By("Checking PVC")
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-internal-certs", Namespace: "default"}, pvc)
			}, timeout, interval).Should(Succeed())
			Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))

			By("Checking Status")
			updatedWs := &weftv1alpha1.WeftServer{}
			Eventually(func() int {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-internal", Namespace: "default"}, updatedWs)
				if err != nil {
					return -1
				}
				return len(updatedWs.Status.Tunnels)
			}, timeout, interval).Should(Equal(1))
			Expect(updatedWs.Status.Tunnels[0].Tx).To(Equal(uint64(100)))
		})

		It("Should bind to specific IP if it matches Node IP", func(ctx context.Context) {
			By("Creating a Node")
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node-1"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			By("Creating a WeftServer linked to the Node")
			ws := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-node-match",
					Namespace: "default",
					Labels:    map[string]string{"weft.aquaduct.dev/node": "test-node-1"},
				},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "http://10.0.0.1:8081",
				},
			}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			By("Reconciling")
			r := &weftserver.WeftServerReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ClientFactory: func(url, tunnelName string) (weftserver.WeftClient, error) { return &mockClient{}, nil },
			}
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-server-node-match", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment uses specific IP")
			dep := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-node-match-server", Namespace: "default"}, dep)
			}, timeout, interval).Should(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("--bind-ip=10.0.0.1"))
		})

		It("Should bind to 0.0.0.0 if IP does not match Node IP", func(ctx context.Context) {
			By("Creating a Node")
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node-2"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())

			By("Creating a WeftServer linked to the Node with DIFFERENT IP")
			ws := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-node-mismatch",
					Namespace: "default",
					Labels:    map[string]string{"weft.aquaduct.dev/node": "test-node-2"},
				},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationInternal,
					ConnectionString: "http://1.2.3.4:8081", // Public IP differing from Node IP
				},
			}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			By("Reconciling")
			r := &weftserver.WeftServerReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				ClientFactory: func(url, tunnelName string) (weftserver.WeftClient, error) { return &mockClient{}, nil },
			}
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-server-node-mismatch", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Deployment uses 0.0.0.0")
			dep := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-node-mismatch-server", Namespace: "default"}, dep)
			}, timeout, interval).Should(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Command).To(ContainElement("--bind-ip=0.0.0.0"))
		})

		It("Should cleanup resources for External location", func(ctx context.Context) {
			By("Creating an External WeftServer")
			ws := &weftv1alpha1.WeftServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-external",
					Namespace: "default",
				},
				Spec: weftv1alpha1.WeftServerSpec{
					Location:         weftv1alpha1.WeftServerLocationExternal,
					ConnectionString: "http://external:8080",
				},
			}
			Expect(k8sClient.Create(ctx, ws)).To(Succeed())

			// Pre-create deployment to test deletion
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-external-server",
					Namespace: "default",
					Labels:    map[string]string{"app": "weft-server"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "weft-server"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "weft-server"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dep)).To(Succeed())

			By("Reconciling")
			r := &weftserver.WeftServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				ClientFactory: func(url, tunnelName string) (weftserver.WeftClient, error) {
					return &mockClient{}, nil
				},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-server-external",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Ensuring Deployment is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-server-external-server", Namespace: "default"}, dep)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})
