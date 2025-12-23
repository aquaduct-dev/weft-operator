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

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	weftgateway "aquaduct.dev/weft-operator/internal/controller/weftgateway"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("WeftGateway Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a Gateway", func() {
		It("Should create WeftTunnels for HTTPRoutes", func(ctx context.Context) {
			By("Creating GatewayClass")
			gwClass := &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft-gateway-class",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}
			Expect(k8sClient.Create(ctx, gwClass)).To(Succeed())

			By("Creating Gateway")
			gwName := "my-gateway"
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
					Name:      "api-route",
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

			By("Verifying WeftTunnel creation")
			// We expect a tunnel to be created
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://my-service.default.svc:8080"))
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("http://test.example.com/api"))
		})
	})

	// =========================================================================
	// Advanced HTTPRoute Matching Tests (Fancy URL Support)
	// =========================================================================
	Context("When reconciling HTTPRoutes with advanced matching (Fancy URL Support)", func() {
		var (
			gwClass *gatewayv1.GatewayClass
			gateway *gatewayv1.Gateway
			gwName  string
		)

		BeforeEach(func(ctx context.Context) {
			gwName = "fancy-url-gateway"

			By("Creating GatewayClass for advanced matching tests")
			gwClass = &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fancy-url-class",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: weftgateway.ControllerName,
				},
			}
			Expect(k8sClient.Create(ctx, gwClass)).To(Succeed())

			By("Creating Gateway")
			hostname := gatewayv1.Hostname("fancy.example.com")
			gateway = &gatewayv1.Gateway{
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
		})

		AfterEach(func(ctx context.Context) {
			// Cleanup routes, gateway, and class
			var routes gatewayv1.HTTPRouteList
			k8sClient.List(ctx, &routes, client.InNamespace("default"))
			for _, route := range routes.Items {
				k8sClient.Delete(ctx, &route)
			}

			// Cleanup tunnels
			var tunnels weftv1alpha1.WeftTunnelList
			k8sClient.List(ctx, &tunnels, client.InNamespace("default"))
			for _, tunnel := range tunnels.Items {
				k8sClient.Delete(ctx, &tunnel)
			}

			k8sClient.Delete(ctx, gateway)
			k8sClient.Delete(ctx, gwClass)
		})

		It("Should translate HTTPRoute header matches to fancy URL fragment syntax", func(ctx context.Context) {
			By("Creating HTTPRoute with header match")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/api"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			headerMatchExact := gatewayv1.HeaderMatchExact
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "header-match-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName(gwName)},
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
									Headers: []gatewayv1.HTTPHeaderMatch{
										{
											Type:  &headerMatchExact,
											Name:  "Authorization",
											Value: "Bearer.*",
										},
										{
											Type:  &headerMatchExact,
											Name:  "X-API-Key",
											Value: "secret-key",
										},
									},
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("auth-service"),
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

			By("Verifying WeftTunnel uses fancy URL fragment syntax for headers")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			// Weft fancy URL syntax: headers go in the fragment as #HeaderName=value&OtherHeader=value
			srcURL := tunnel.Spec.Routes[0].SrcURL
			Expect(srcURL).To(ContainSubstring("http://fancy.example.com/api"))
			// Headers should be in fragment
			Expect(srcURL).To(ContainSubstring("#"))
			Expect(srcURL).To(ContainSubstring("Authorization=Bearer.*"))
			Expect(srcURL).To(ContainSubstring("X-API-Key=secret-key"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://auth-service.default.svc:8080"))
		})

		It("Should translate HTTPRoute query parameter matches to fancy URL query syntax", func(ctx context.Context) {
			By("Creating HTTPRoute with query parameter match")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/search"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			queryMatchExact := gatewayv1.QueryParamMatchExact
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "query-match-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName(gwName)},
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
									QueryParams: []gatewayv1.HTTPQueryParamMatch{
										{
											Type:  &queryMatchExact,
											Name:  "token",
											Value: "valid-token-regex",
										},
										{
											Type:  &queryMatchExact,
											Name:  "version",
											Value: "v2",
										},
									},
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("search-service"),
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

			By("Verifying WeftTunnel uses fancy URL query syntax for query params")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			// Weft fancy URL syntax: query params go in the query string as ?param=value&other=value
			srcURL := tunnel.Spec.Routes[0].SrcURL
			Expect(srcURL).To(ContainSubstring("http://fancy.example.com/search"))
			Expect(srcURL).To(ContainSubstring("?"))
			Expect(srcURL).To(ContainSubstring("token=valid-token-regex"))
			Expect(srcURL).To(ContainSubstring("version=v2"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://search-service.default.svc:8080"))
		})

		It("Should translate HTTPRoute method matches to fancy URL fragment syntax", func(ctx context.Context) {
			By("Creating HTTPRoute with method match")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/submit"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			methodPost := gatewayv1.HTTPMethodPost
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "method-match-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName(gwName)},
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
									Method: &methodPost,
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("submit-service"),
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

			By("Verifying WeftTunnel uses fancy URL fragment syntax for method")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			// Weft fancy URL syntax: method goes in the fragment as #POST
			srcURL := tunnel.Spec.Routes[0].SrcURL
			Expect(srcURL).To(ContainSubstring("http://fancy.example.com/submit"))
			Expect(srcURL).To(ContainSubstring("#"))
			Expect(srcURL).To(ContainSubstring("POST"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://submit-service.default.svc:8080"))
		})

		It("Should use HTTPS scheme for listeners with HTTPS protocol", func(ctx context.Context) {
			By("Creating Gateway with HTTPS listener")
			httpsHostname := gatewayv1.Hostname("secure.example.com")
			tlsMode := gatewayv1.TLSModeTerminate
			httpsGateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "https-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: gatewayv1.HTTPSProtocolType,
							Hostname: &httpsHostname,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsMode,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, httpsGateway)).To(Succeed())
			defer k8sClient.Delete(ctx, httpsGateway)

			By("Creating HTTPRoute attached to HTTPS gateway")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/secure-api"
			backendPort := gatewayv1.PortNumber(8443)
			backendKind := gatewayv1.Kind("Service")
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "https-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName("https-gateway")},
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
											Name: gatewayv1.ObjectName("secure-backend"),
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
			defer k8sClient.Delete(ctx, route)

			r := &weftgateway.WeftGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: httpsGateway.Name, Namespace: httpsGateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses HTTPS scheme for HTTPS listener")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": "https-gateway"})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			// HTTPS listener should result in https:// scheme in SrcURL
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("https://secure.example.com/secure-api"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://secure-backend.default.svc:8443"))
		})

		It("Should combine multiple matchers in fancy URL format", func(ctx context.Context) {
			By("Creating HTTPRoute with path, header, query, and method matches")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/combined"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			headerMatchExact := gatewayv1.HeaderMatchExact
			queryMatchExact := gatewayv1.QueryParamMatchExact
			methodPut := gatewayv1.HTTPMethodPut
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-match-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName(gwName)},
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
									Headers: []gatewayv1.HTTPHeaderMatch{
										{
											Type:  &headerMatchExact,
											Name:  "Content-Type",
											Value: "application/json",
										},
									},
									QueryParams: []gatewayv1.HTTPQueryParamMatch{
										{
											Type:  &queryMatchExact,
											Name:  "format",
											Value: "json",
										},
									},
									Method: &methodPut,
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: gatewayv1.ObjectName("combined-service"),
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

			By("Verifying WeftTunnel combines all matchers in fancy URL format")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": gwName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			srcURL := tunnel.Spec.Routes[0].SrcURL
			// Should contain path
			Expect(srcURL).To(ContainSubstring("http://fancy.example.com/combined"))
			// Should contain query param
			Expect(srcURL).To(ContainSubstring("format=json"))
			// Should contain header and method in fragment
			Expect(srcURL).To(ContainSubstring("Content-Type=application/json"))
			Expect(srcURL).To(ContainSubstring("PUT"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://combined-service.default.svc:8080"))
		})

		It("Should handle Gateway with multiple listeners (HTTP and HTTPS)", func(ctx context.Context) {
			By("Creating Gateway with both HTTP and HTTPS listeners")
			httpHostname := gatewayv1.Hostname("multi.example.com")
			httpsHostname := gatewayv1.Hostname("secure-multi.example.com")
			tlsMode := gatewayv1.TLSModeTerminate
			multiGateway := &gatewayv1.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-listener-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gwClass.Name),
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: gatewayv1.HTTPProtocolType,
							Hostname: &httpHostname,
						},
						{
							Name:     "https",
							Port:     443,
							Protocol: gatewayv1.HTTPSProtocolType,
							Hostname: &httpsHostname,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsMode,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, multiGateway)).To(Succeed())
			defer k8sClient.Delete(ctx, multiGateway)

			By("Creating HTTPRoute attached to multi-listener gateway")
			pathMatch := gatewayv1.PathMatchPathPrefix
			pathValue := "/multi-api"
			backendPort := gatewayv1.PortNumber(8080)
			backendKind := gatewayv1.Kind("Service")
			route := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-listener-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{Name: gatewayv1.ObjectName("multi-listener-gateway")},
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
											Name: gatewayv1.ObjectName("multi-backend"),
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
			defer k8sClient.Delete(ctx, route)

			r := &weftgateway.WeftGatewayReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: multiGateway.Name, Namespace: multiGateway.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel created for each listener with appropriate scheme")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"gateway": "multi-listener-gateway"})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(2))

			// Find HTTP and HTTPS tunnels
			var httpTunnel, httpsTunnel *weftv1alpha1.WeftTunnel
			for i := range tunnelList.Items {
				t := &tunnelList.Items[i]
				if len(t.Spec.Routes) > 0 {
					if t.Spec.Routes[0].SrcURL == "http://multi.example.com/multi-api" {
						httpTunnel = t
					} else if t.Spec.Routes[0].SrcURL == "https://secure-multi.example.com/multi-api" {
						httpsTunnel = t
					}
				}
			}

			Expect(httpTunnel).NotTo(BeNil(), "Expected HTTP tunnel for http listener")
			Expect(httpTunnel.Spec.Routes[0].SrcURL).To(Equal("http://multi.example.com/multi-api"))

			Expect(httpsTunnel).NotTo(BeNil(), "Expected HTTPS tunnel for https listener")
			Expect(httpsTunnel.Spec.Routes[0].SrcURL).To(Equal("https://secure-multi.example.com/multi-api"))
		})
	})
})
