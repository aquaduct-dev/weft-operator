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
			Expect(tunnel.Spec.DstURL).To(Equal("http://my-service.default.svc:8080"))
			Expect(tunnel.Spec.SrcURL).To(Equal("http://test.example.com/api"))
		})
	})
})