/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package weftingress_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	weftingress "aquaduct.dev/weft-operator/internal/controller/weftingress"
)

var _ = Describe("WeftIngress Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling an Ingress", func() {
		It("Should create WeftTunnels for Ingress rules", func(ctx context.Context) {
			By("Creating IngressClass")
			ingressClass := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "weft",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: weftingress.ControllerName,
				},
			}
			Expect(k8sClient.Create(ctx, ingressClass)).To(Succeed())

			By("Creating Ingress")
			ingressName := "test-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: "test.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/api",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "my-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel creation")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://my-service.default.svc:8080"))
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("http://test.example.com/api"))
		})

		It("Should create WeftTunnels for multiple paths", func(ctx context.Context) {
			By("Creating Ingress with multiple paths")
			ingressName := "multi-path-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: "multi.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/api",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "api-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
										{
											Path:     "/web",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "web-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 3000,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel creation for both paths")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(2))
		})

		It("Should handle wildcard host URLs", func(ctx context.Context) {
			By("Creating Ingress with wildcard host")
			ingressName := "wildcard-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: "*.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "wildcard-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel URL format supports wildcards")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			// Fancy URL syntax: wildcard hosts should be preserved
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("http://*.example.com/"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://wildcard-service.default.svc:8080"))
		})

		It("Should handle empty host (default backend)", func(ctx context.Context) {
			By("Creating Ingress with no host (default backend)")
			ingressName := "default-backend-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							// Empty host - matches all requests
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/fallback",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "fallback-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel URL format handles empty host as wildcard")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			// Fancy URL syntax: empty host should become "*"
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("http://*/fallback"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://fallback-service.default.svc:80"))
		})

		It("Should handle complex path with special characters", func(ctx context.Context) {
			By("Creating Ingress with complex path")
			ingressName := "complex-path-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: "api.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/v1/users/:id/profile",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "user-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 3000,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel preserves path structure")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(1))

			tunnel := tunnelList.Items[0]
			Expect(tunnel.Spec.Routes).To(HaveLen(1))
			Expect(tunnel.Spec.Routes[0].SrcURL).To(Equal("http://api.example.com/v1/users/:id/profile"))
			Expect(tunnel.Spec.Routes[0].DstURL).To(Equal("http://user-service.default.svc:3000"))
		})

		It("Should use HTTPS scheme for TLS-enabled hosts (fancy URL formatting)", func(ctx context.Context) {
			By("Creating Ingress with TLS configuration")
			ingressName := "tls-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"secure.example.com"},
							SecretName: "tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "secure.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/secure-api",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "secure-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 443,
													},
												},
											},
										},
									},
								},
							},
						},
						{
							Host: "insecure.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/api",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "insecure-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel uses HTTPS for TLS hosts and HTTP for non-TLS hosts")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(2))

			// Find the tunnels and verify URLs
			var secureTunnel, insecureTunnel *weftv1alpha1.WeftTunnel
			for i := range tunnelList.Items {
				t := &tunnelList.Items[i]
				if len(t.Spec.Routes) > 0 {
					if t.Spec.Routes[0].SrcURL == "https://secure.example.com/secure-api" {
						secureTunnel = t
					} else if t.Spec.Routes[0].SrcURL == "http://insecure.example.com/api" {
						insecureTunnel = t
					}
				}
			}

			Expect(secureTunnel).NotTo(BeNil(), "Expected tunnel with HTTPS URL for TLS host")
			Expect(secureTunnel.Spec.Routes[0].SrcURL).To(Equal("https://secure.example.com/secure-api"))
			Expect(secureTunnel.Spec.Routes[0].DstURL).To(Equal("http://secure-service.default.svc:443"))

			Expect(insecureTunnel).NotTo(BeNil(), "Expected tunnel with HTTP URL for non-TLS host")
			Expect(insecureTunnel.Spec.Routes[0].SrcURL).To(Equal("http://insecure.example.com/api"))
			Expect(insecureTunnel.Spec.Routes[0].DstURL).To(Equal("http://insecure-service.default.svc:8080"))
		})

		It("Should generate URLs compatible with Weft's fancy URL syntax (path rewriting)", func(ctx context.Context) {
			By("Creating Ingress with path-based routing")
			ingressName := "fancy-url-ingress"
			ingressClassName := "weft"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: "api.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											// Path /api/v1 will be used by Weft for path rewriting
											Path:     "/api/v1",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "backend-v1",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
										{
											// Separate path /api/v2 routes to different backend
											Path:     "/api/v2",
											PathType: &pathType,
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "backend-v2",
													Port: networkingv1.ServiceBackendPort{
														Number: 9090,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			r := &weftingress.WeftIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: ingress.Name, Namespace: ingress.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel URLs support Weft's fancy URL path syntax")
			var tunnelList weftv1alpha1.WeftTunnelList
			Eventually(func() int {
				k8sClient.List(ctx, &tunnelList, client.MatchingLabels{"ingress": ingressName})
				return len(tunnelList.Items)
			}, timeout, interval).Should(Equal(2))

			// Find the tunnels and verify URLs
			// Weft expects: SrcURL = public URL with path, DstURL = backend service
			// Example: weft tunnel <server> http://backend:port https://domain/path
			// SrcURL (destination in weft terms) should include the full path for path rewriting
			var v1Tunnel, v2Tunnel *weftv1alpha1.WeftTunnel
			for i := range tunnelList.Items {
				t := &tunnelList.Items[i]
				if len(t.Spec.Routes) > 0 {
					if t.Spec.Routes[0].SrcURL == "http://api.example.com/api/v1" {
						v1Tunnel = t
					} else if t.Spec.Routes[0].SrcURL == "http://api.example.com/api/v2" {
						v2Tunnel = t
					}
				}
			}

			// SrcURL includes the full path, enabling Weft's path rewriting feature
			// When a request comes to https://api.example.com/api/v1/users,
			// Weft will forward to http://backend-v1:8080/api/v1/users
			Expect(v1Tunnel).NotTo(BeNil(), "Expected tunnel for /api/v1 path")
			Expect(v1Tunnel.Spec.Routes[0].SrcURL).To(Equal("http://api.example.com/api/v1"))
			Expect(v1Tunnel.Spec.Routes[0].DstURL).To(Equal("http://backend-v1.default.svc:8080"))

			Expect(v2Tunnel).NotTo(BeNil(), "Expected tunnel for /api/v2 path")
			Expect(v2Tunnel.Spec.Routes[0].SrcURL).To(Equal("http://api.example.com/api/v2"))
			Expect(v2Tunnel.Spec.Routes[0].DstURL).To(Equal("http://backend-v2.default.svc:9090"))
		})
	})
})
