package weftgateway_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	weftgateway "aquaduct.dev/weft-operator/internal/controller/weftgateway"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// ptrTo returns a pointer to the given value.
func ptrTo[T any](v T) *T {
	return &v
}

var _ = Describe("WeftGateway Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a Gateway", func() {
		It("Should create WeftTunnels with correct URL semantics for HTTP", func(ctx context.Context) {
			By("Creating GatewayClass")
			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class-http",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}
			Expect(k8sClient.Create(ctx, gwClass)).To(Succeed())

			By("Creating Gateway with HTTP listener")
			gwName := "my-gateway-http"
			hostname := gatewayv1.Hostname("test.example.com")
			gateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      gwName,
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: gatewayv1.HTTPProtocolType,
							Hostname: &hostname,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, gateway)).To(Succeed())

			By("Creating HTTPRoute")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/api"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "api-route-http",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: gatewayv1.ObjectName(gwName),
							},
						},
					},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								{
									Path: &gatewayv1.HTTPPathMatch{
										Type:  &pathMatch,
										Value: &pathValue,
									},
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("my-service"),
											Port: &backendPort,
											Kind: &backendKind,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, route)).To(Succeed())

			r := &weftgateway.WeftGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel has correct URL semantics")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			// SrcURL should be the internal cluster service (always http)
			Expect(tunnel.Spec.SrcURL).To(Equal("http://my-service.default.svc:8080"))
			// DstURL should be the external hostname with http for HTTP listener
			Expect(tunnel.Spec.DstURL).To(Equal("http://test.example.com/api"))
		})

		It("Should use https in DstURL for HTTPS listeners", func(ctx context.Context) {
			// Use a fake client to bypass CRD validation for HTTPS listener TLS requirements
			fakeScheme := k8sClient.Scheme()
			
			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class-https-fake",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}
			
			gwName := "my-gateway-https-fake"
			hostname := gatewayv1.Hostname("secure.example.com")
			gateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      gwName,
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: gatewayv1.HTTPSProtocolType,
							Hostname: &hostname,
						},
					},
				},
			}

			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/secure"
			backendPort := gatewayv1.PortNumber(8443)
			backendKind := gatewayv1.Kind("Service")
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secure-route-fake",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: gatewayv1.ObjectName(gwName),
							},
						},
					},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								{
									Path: &gatewayv1.HTTPPathMatch{
										Type:  &pathMatch,
										Value: &pathValue,
									},
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("secure-service"),
											Port: &backendPort,
											Kind: &backendKind,
										},
									},
								},
							},
						},
					},
				},
			}
			
			// Use fakeclient which doesn't validate CRD schemas
			fakeClient := fakeclient.NewClientBuilder().
				WithScheme(fakeScheme).
				WithObjects(gwClass, gateway, route).
				WithStatusSubresource(gwClass, gateway).
				Build()

			r := &weftgateway.WeftGatewayReconciler{
				Client: fakeClient,
				Scheme: fakeScheme,
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses https in DstURL")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				fakeClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			// SrcURL should still be http (internal cluster service)
			Expect(tunnel.Spec.SrcURL).To(Equal("http://secure-service.default.svc:8443"))
			// DstURL should be https for HTTPS listener
			Expect(tunnel.Spec.DstURL).To(Equal("https://secure.example.com/secure"))
		})

		It("Should create WeftTunnels with tcp:// scheme for TCPRoute", func(ctx context.Context) {
			fakeScheme := k8sClient.Scheme()

			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class-tcp",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}

			gwName := "my-gateway-tcp"
			hostname := gatewayv1.Hostname("tcp.example.com")
			gateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      gwName,
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "tcp",
							Port:     5432,
							Protocol: gatewayv1.TCPProtocolType,
							Hostname: &hostname,
						},
					},
				},
			}

			backendPort := gatewayv1.PortNumber(5432)
			backendKind := gatewayv1.Kind("Service")
			tcpRoute := &gatewayv1alpha2.TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: gatewayv1.ObjectName(gwName),
							},
						},
					},
					Rules: []gatewayv1alpha2.TCPRouteRule{
						{
							BackendRefs: []gatewayv1.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("postgres-service"),
										Port: &backendPort,
										Kind: &backendKind,
									},
								},
							},
						},
					},
				},
			}

			fakeClient := fakeclient.NewClientBuilder().
				WithScheme(fakeScheme).
				WithObjects(gwClass, gateway, tcpRoute).
				WithStatusSubresource(gwClass, gateway).
				Build()

			r := &weftgateway.WeftGatewayReconciler{
				Client: fakeClient,
				Scheme: fakeScheme,
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses tcp:// scheme")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				fakeClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.SrcURL).To(Equal("tcp://postgres-service.default.svc:5432"))
			Expect(tunnel.Spec.DstURL).To(Equal("tcp://tcp.example.com:5432"))
		})

		It("Should create WeftTunnels with https:// scheme for TLSRoute (passthrough)", func(ctx context.Context) {
			fakeScheme := k8sClient.Scheme()

			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class-tls",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}

			gwName := "my-gateway-tls"
			hostname := gatewayv1.Hostname("passthrough.example.com")
			gateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      gwName,
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "tls",
							Port:     443,
							Protocol: gatewayv1.TLSProtocolType,
							Hostname: &hostname,
						},
					},
				},
			}

			backendPort := gatewayv1.PortNumber(8443)
			backendKind := gatewayv1.Kind("Service")
			tlsRoute := &gatewayv1alpha2.TLSRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-passthrough-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.TLSRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: gatewayv1.ObjectName(gwName),
							},
						},
					},
					Rules: []gatewayv1alpha2.TLSRouteRule{
						{
							BackendRefs: []gatewayv1.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("backend-tls-service"),
										Port: &backendPort,
										Kind: &backendKind,
									},
								},
							},
						},
					},
				},
			}

			fakeClient := fakeclient.NewClientBuilder().
				WithScheme(fakeScheme).
				WithObjects(gwClass, gateway, tlsRoute).
				WithStatusSubresource(gwClass, gateway).
				Build()

			r := &weftgateway.WeftGatewayReconciler{
				Client: fakeClient,
				Scheme: fakeScheme,
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses https:// for TLS passthrough")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				fakeClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			// TLS passthrough uses https:// for both src and dst
			Expect(tunnel.Spec.SrcURL).To(Equal("https://backend-tls-service.default.svc:8443"))
			Expect(tunnel.Spec.DstURL).To(Equal("https://passthrough.example.com"))
		})

		It("Should create WeftTunnels with udp:// scheme for UDPRoute", func(ctx context.Context) {
			fakeScheme := k8sClient.Scheme()

			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class-udp",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}

			gwName := "my-gateway-udp"
			gateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      gwName,
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "udp",
							Port:     53,
							Protocol: gatewayv1.UDPProtocolType,
						},
					},
				},
			}

			backendPort := gatewayv1.PortNumber(53)
			backendKind := gatewayv1.Kind("Service")
			udpRoute := &gatewayv1alpha2.UDPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dns-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.UDPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: gatewayv1.ObjectName(gwName),
							},
						},
					},
					Rules: []gatewayv1alpha2.UDPRouteRule{
						{
							BackendRefs: []gatewayv1.BackendRef{
								{
									BackendObjectReference: gatewayv1.BackendObjectReference{
										Name: gatewayv1.ObjectName("dns-service"),
										Port: &backendPort,
										Kind: &backendKind,
									},
								},
							},
						},
					},
				},
			}

			fakeClient := fakeclient.NewClientBuilder().
				WithScheme(fakeScheme).
				WithObjects(gwClass, gateway, udpRoute).
				WithStatusSubresource(gwClass, gateway).
				Build()

			r := &weftgateway.WeftGatewayReconciler{
				Client: fakeClient,
				Scheme: fakeScheme,
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses udp:// scheme")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				fakeClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.SrcURL).To(Equal("udp://dns-service.default.svc:53"))
			Expect(tunnel.Spec.DstURL).To(Equal("udp://:53"))
		})
	})
})