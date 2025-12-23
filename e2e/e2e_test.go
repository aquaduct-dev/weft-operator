// Package e2e contains comprehensive integration tests for the weft-operator Helm chart.
// These tests verify that the operator can be installed in a real Kubernetes
// cluster and correctly reconcile all CRD types including WeftServer, WeftTunnel,
// WeftGateway, and Gateway API resources.
package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	dynamicClient dynamic.Interface
	clientset     *kubernetes.Clientset
	ctx           context.Context
	cancel        context.CancelFunc
)

// GVRs for all CRD types
var (
	weftTunnelGVR = schema.GroupVersionResource{
		Group:    "weft.aquaduct.dev",
		Version:  "v1alpha1",
		Resource: "wefttunnels",
	}
	weftServerGVR = schema.GroupVersionResource{
		Group:    "weft.aquaduct.dev",
		Version:  "v1alpha1",
		Resource: "weftservers",
	}
	weftGatewayGVR = schema.GroupVersionResource{
		Group:    "weft.aquaduct.dev",
		Version:  "v1alpha1",
		Resource: "weftgateways",
	}
	gatewayClassGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gatewayclasses",
	}
	gatewayGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "gateways",
	}
	httpRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
)

const (
	testNamespace = "default"
	timeout       = 60 * time.Second
	interval      = 2 * time.Second
)

// TestE2E runs the e2e test suite.
// These tests expect an already-running cluster with the operator installed.
// Use scripts/run-e2e.sh for full cluster lifecycle management.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)

	// Get kubeconfig from environment or default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "Failed to load kubeconfig")

	dynamicClient, err = dynamic.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred(), "Failed to create dynamic client")

	clientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred(), "Failed to create clientset")
})

var _ = AfterSuite(func() {
	cancel()
})

var _ = Describe("Weft Operator E2E", func() {
	// =========================================================================
	// CRD Availability Tests
	// =========================================================================
	Describe("CRD Availability", func() {
		It("should have WeftTunnel CRD available", func() {
			_, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "WeftTunnel CRD should be available")
		})

		It("should have WeftServer CRD available", func() {
			_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "WeftServer CRD should be available")
		})

		It("should have WeftGateway CRD available", func() {
			_, err := dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "WeftGateway CRD should be available")
		})

		It("should have Gateway API CRDs available", func() {
			_, err := dynamicClient.Resource(gatewayClassGVR).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "GatewayClass CRD should be available")

			_, err = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "Gateway CRD should be available")

			_, err = dynamicClient.Resource(httpRouteGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{Limit: 1})
			Expect(err).NotTo(HaveOccurred(), "HTTPRoute CRD should be available")
		})
	})

	// =========================================================================
	// WeftServer Tests
	// =========================================================================
	Describe("WeftServer Reconciliation", func() {
		Context("External WeftServer", func() {
			var serverName string

			BeforeEach(func() {
				serverName = "e2e-external-server"
			})

			AfterEach(func() {
				_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})
			})

			It("should create External WeftServer without creating Deployment", func() {
				server := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "weft.aquaduct.dev/v1alpha1",
						"kind":       "WeftServer",
						"metadata": map[string]interface{}{
							"name":      serverName,
							"namespace": testNamespace,
						},
						"spec": map[string]interface{}{
							"connectionString": "weft://testsecret@10.0.0.1:8080",
							"location":         "External",
						},
					},
				}

				By("Creating an External WeftServer resource")
				created, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to create WeftServer")
				Expect(created.GetName()).To(Equal(serverName))

				By("Verifying no Deployment is created for External server")
				time.Sleep(3 * time.Second) // Brief wait for potential reconciliation
				depName := serverName + "-server"
				_, err = clientset.AppsV1().Deployments(testNamespace).Get(ctx, depName, metav1.GetOptions{})
				Expect(err).To(HaveOccurred(), "Deployment should NOT be created for External WeftServer")

				GinkgoWriter.Printf("External WeftServer %s verified - no deployment created\n", serverName)
			})
		})

		Context("Internal WeftServer", func() {
			var serverName string

			BeforeEach(func() {
				serverName = "e2e-internal-server"
			})

			AfterEach(func() {
				_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})
				// Wait for cleanup
				time.Sleep(2 * time.Second)
			})

			It("should create Internal WeftServer with Deployment, Service, and PVC", func() {
				server := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "weft.aquaduct.dev/v1alpha1",
						"kind":       "WeftServer",
						"metadata": map[string]interface{}{
							"name":      serverName,
							"namespace": testNamespace,
						},
						"spec": map[string]interface{}{
							"connectionString": "weft://testsecret@0.0.0.0:9092",
							"location":         "Internal",
						},
					},
				}

				By("Creating an Internal WeftServer resource")
				created, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "Failed to create WeftServer")
				Expect(created.GetName()).To(Equal(serverName))

				depName := serverName + "-server"
				pvcName := serverName + "-certs"

				By("Verifying Deployment is created")
				Eventually(func() error {
					_, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, depName, metav1.GetOptions{})
					return err
				}, timeout, interval).Should(Succeed(), "Deployment should be created for Internal WeftServer")

				By("Verifying Deployment has correct configuration")
				dep, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, depName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(dep.Spec.Template.Spec.HostNetwork).To(BeTrue(), "Deployment should use host network")
				Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
				Expect(dep.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("ghcr.io/aquaduct-dev/weft"))

				By("Verifying Service is created")
				Eventually(func() error {
					_, err := clientset.CoreV1().Services(testNamespace).Get(ctx, depName, metav1.GetOptions{})
					return err
				}, timeout, interval).Should(Succeed(), "Service should be created for Internal WeftServer")

				By("Verifying PVC is created")
				Eventually(func() error {
					_, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(ctx, pvcName, metav1.GetOptions{})
					return err
				}, timeout, interval).Should(Succeed(), "PVC should be created for Internal WeftServer")

				GinkgoWriter.Printf("Internal WeftServer %s verified with Deployment, Service, PVC\n", serverName)
			})
		})
	})

	// =========================================================================
	// WeftTunnel Tests
	// =========================================================================
	Describe("WeftTunnel Reconciliation", func() {
		var tunnelName, serverName string

		BeforeEach(func() {
			tunnelName = "e2e-test-tunnel"
			serverName = "e2e-tunnel-server"

			// Create an External WeftServer for tunnels to target
			server := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftServer",
					"metadata": map[string]interface{}{
						"name":      serverName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"connectionString": "weft://tunneltest@192.168.1.1:8080",
						"location":         "External",
					},
				},
			}
			_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create WeftServer for tunnel test")
		})

		AfterEach(func() {
			_ = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Delete(ctx, tunnelName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})
			time.Sleep(2 * time.Second) // Wait for cleanup
		})

		It("should create WeftTunnel and spawn Deployment for each target WeftServer", func() {
			tunnel := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftTunnel",
					"metadata": map[string]interface{}{
						"name":      tunnelName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"srcURL":        "http://test-svc.default.svc.cluster.local:8080",
						"dstURL":        "https://test.example.com",
						"targetServers": []interface{}{serverName},
					},
				},
			}

			By("Creating a WeftTunnel resource")
			created, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Create(ctx, tunnel, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create WeftTunnel")
			Expect(created.GetName()).To(Equal(tunnelName))

			// Expected deployment name format: tunnel-<tunnelName>-to-<serverName>
			expectedDepName := fmt.Sprintf("tunnel-%s-to-%s", tunnelName, serverName)

			By("Verifying tunnel Deployment is created")
			Eventually(func() error {
				_, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
				return err
			}, timeout, interval).Should(Succeed(), "Tunnel Deployment should be created")

			By("Verifying tunnel Deployment has correct labels")
			dep, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(dep.Labels["app"]).To(Equal("weft-tunnel"))
			Expect(dep.Labels["tunnel"]).To(Equal(tunnelName))
			Expect(dep.Labels["target"]).To(Equal(serverName))
			Expect(dep.Labels["created-by"]).To(Equal("weft-operator"))

			By("Verifying tunnel Deployment container args")
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			args := dep.Spec.Template.Spec.Containers[0].Args
			Expect(args).To(ContainElement("tunnel"))
			Expect(args).To(ContainElement(fmt.Sprintf("--tunnel-name=%s", tunnelName)))

			GinkgoWriter.Printf("WeftTunnel %s verified with Deployment %s\n", tunnelName, expectedDepName)
		})

		It("should update WeftTunnel status conditions", func() {
			tunnel := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftTunnel",
					"metadata": map[string]interface{}{
						"name":      tunnelName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"srcURL":        "http://status-test.default.svc.cluster.local:80",
						"dstURL":        "https://status.example.com",
						"targetServers": []interface{}{serverName},
					},
				},
			}

			By("Creating a WeftTunnel resource")
			_, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Create(ctx, tunnel, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel status conditions are updated")
			Eventually(func() bool {
				obj, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Get(ctx, tunnelName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				status, found, _ := unstructured.NestedMap(obj.Object, "status")
				if !found {
					return false
				}
				conditions, found, _ := unstructured.NestedSlice(status, "conditions")
				return found && len(conditions) > 0
			}, timeout, interval).Should(BeTrue(), "WeftTunnel should have status conditions")

			GinkgoWriter.Printf("WeftTunnel %s status conditions verified\n", tunnelName)
		})
	})

	// =========================================================================
	// WeftTunnel Cleanup Tests
	// =========================================================================
	Describe("WeftTunnel Cleanup", func() {
		It("should delete tunnel Deployment when WeftTunnel is deleted", func() {
			tunnelName := "e2e-cleanup-tunnel"
			serverName := "e2e-cleanup-server"

			By("Creating WeftServer and WeftTunnel")
			server := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftServer",
					"metadata": map[string]interface{}{
						"name":      serverName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"connectionString": "weft://cleanuptest@10.0.0.2:8080",
						"location":         "External",
					},
				},
			}
			_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			tunnel := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftTunnel",
					"metadata": map[string]interface{}{
						"name":      tunnelName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"srcURL":        "http://cleanup.default.svc.cluster.local:80",
						"dstURL":        "https://cleanup.example.com",
						"targetServers": []interface{}{serverName},
					},
				},
			}
			_, err = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Create(ctx, tunnel, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			expectedDepName := fmt.Sprintf("tunnel-%s-to-%s", tunnelName, serverName)

			By("Waiting for tunnel Deployment to be created")
			Eventually(func() error {
				_, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
				return err
			}, timeout, interval).Should(Succeed())

			By("Deleting WeftTunnel")
			err = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Delete(ctx, tunnelName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying tunnel Deployment is deleted via owner reference")
			Eventually(func() bool {
				_, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
				return err != nil // Should fail (not found)
			}, timeout, interval).Should(BeTrue(), "Tunnel Deployment should be deleted when WeftTunnel is deleted")

			// Cleanup
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})

			GinkgoWriter.Printf("WeftTunnel cleanup verified - Deployment deleted with owner\n")
		})
	})

	// =========================================================================
	// Gateway API Integration Tests
	// =========================================================================
	Describe("Gateway API Integration", func() {
		var gatewayClassName, gatewayName, weftGatewayName string

		BeforeEach(func() {
			gatewayClassName = "e2e-weft-gateway-class"
			gatewayName = "e2e-gateway"
			weftGatewayName = "e2e-weft-gateway"
		})

		AfterEach(func() {
			// Cleanup in reverse order
			_ = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Delete(ctx, gatewayName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).Delete(ctx, weftGatewayName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(gatewayClassGVR).Delete(ctx, gatewayClassName, metav1.DeleteOptions{})
			time.Sleep(2 * time.Second)
		})

		It("should create WeftGateway and configure GatewayClass", func() {
			weftGateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftGateway",
					"metadata": map[string]interface{}{
						"name":      weftGatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"targetServers": []interface{}{},
					},
				},
			}

			By("Creating a WeftGateway resource")
			created, err := dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).Create(ctx, weftGateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create WeftGateway")
			Expect(created.GetName()).To(Equal(weftGatewayName))

			By("Creating GatewayClass referencing WeftGateway")
			gatewayClass := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "GatewayClass",
					"metadata": map[string]interface{}{
						"name": gatewayClassName,
					},
					"spec": map[string]interface{}{
						"controllerName": "weft.aquaduct.dev/gateway-controller",
						"parametersRef": map[string]interface{}{
							"group":     "weft.aquaduct.dev",
							"kind":      "WeftGateway",
							"name":      weftGatewayName,
							"namespace": testNamespace,
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayClassGVR).Create(ctx, gatewayClass, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create GatewayClass")

			By("Creating Gateway using the GatewayClass")
			gateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "Gateway",
					"metadata": map[string]interface{}{
						"name":      gatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"gatewayClassName": gatewayClassName,
						"listeners": []interface{}{
							map[string]interface{}{
								"name":     "http",
								"protocol": "HTTP",
								"port":     int64(80),
								"hostname": "gateway.example.com",
							},
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Create(ctx, gateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create Gateway")

			By("Verifying Gateway exists")
			Eventually(func() error {
				_, err := dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Get(ctx, gatewayName, metav1.GetOptions{})
				return err
			}, timeout, interval).Should(Succeed())

			GinkgoWriter.Printf("Gateway API integration verified: WeftGateway=%s, GatewayClass=%s, Gateway=%s\n",
				weftGatewayName, gatewayClassName, gatewayName)
		})
	})

	// =========================================================================
	// Gateway Fancy URL Conformance Tests
	// =========================================================================
	Describe("Gateway Fancy URL Conformance", func() {
		var gatewayClassName, gatewayName, weftGatewayName, serverName string

		BeforeEach(func() {
			gatewayClassName = "e2e-fancy-url-class"
			gatewayName = "e2e-fancy-gateway"
			weftGatewayName = "e2e-fancy-weft-gateway"
			serverName = "e2e-fancy-server"

			// Create WeftServer with External location
			server := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftServer",
					"metadata": map[string]interface{}{
						"name":      serverName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"connectionString": "weft://fancytest@10.0.0.100:8080",
						"location":         "External",
					},
				},
			}
			_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to create WeftServer for fancy URL test")
		})

		AfterEach(func() {
			// Cleanup in reverse order
			_ = dynamicClient.Resource(httpRouteGVR).Namespace(testNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
			_ = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
			_ = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Delete(ctx, gatewayName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).Delete(ctx, weftGatewayName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(gatewayClassGVR).Delete(ctx, gatewayClassName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})
			time.Sleep(2 * time.Second)
		})

		It("should create WeftTunnel with path-based fancy URL for HTTPRoute with path matcher", func() {
			By("Creating WeftGateway with target server")
			weftGateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftGateway",
					"metadata": map[string]interface{}{
						"name":      weftGatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"targetServers": []interface{}{serverName},
					},
				},
			}
			_, err := dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).Create(ctx, weftGateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating GatewayClass referencing WeftGateway")
			gatewayClass := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "GatewayClass",
					"metadata": map[string]interface{}{
						"name": gatewayClassName,
					},
					"spec": map[string]interface{}{
						"controllerName": "weft.aquaduct.dev/gateway-controller",
						"parametersRef": map[string]interface{}{
							"group":     "weft.aquaduct.dev",
							"kind":      "WeftGateway",
							"name":      weftGatewayName,
							"namespace": testNamespace,
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayClassGVR).Create(ctx, gatewayClass, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating Gateway with HTTP listener")
			gateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "Gateway",
					"metadata": map[string]interface{}{
						"name":      gatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"gatewayClassName": gatewayClassName,
						"listeners": []interface{}{
							map[string]interface{}{
								"name":     "http",
								"protocol": "HTTP",
								"port":     int64(80),
								"hostname": "fancy-api.example.com",
							},
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Create(ctx, gateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating HTTPRoute with path matcher")
			httpRoute := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "HTTPRoute",
					"metadata": map[string]interface{}{
						"name":      "fancy-path-route",
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"parentRefs": []interface{}{
							map[string]interface{}{
								"name": gatewayName,
							},
						},
						"rules": []interface{}{
							map[string]interface{}{
								"matches": []interface{}{
									map[string]interface{}{
										"path": map[string]interface{}{
											"type":  "PathPrefix",
											"value": "/api/v1",
										},
									},
								},
								"backendRefs": []interface{}{
									map[string]interface{}{
										"name": "api-backend",
										"port": int64(8080),
									},
								},
							},
						},
					},
				},
			}
			_, err = dynamicClient.Resource(httpRouteGVR).Namespace(testNamespace).Create(ctx, httpRoute, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel is created with path in SrcURL")
			Eventually(func() bool {
				tunnels, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("gateway=%s", gatewayName),
				})
				if err != nil || len(tunnels.Items) == 0 {
					return false
				}

				for _, tunnel := range tunnels.Items {
					routes, found, _ := unstructured.NestedSlice(tunnel.Object, "spec", "routes")
					if !found || len(routes) == 0 {
						continue
					}
					route := routes[0].(map[string]interface{})
					srcURL, _, _ := unstructured.NestedString(route, "srcURL")
					// Should include path in fancy URL
					if srcURL == "http://fancy-api.example.com/api/v1" {
						GinkgoWriter.Printf("Found WeftTunnel with fancy URL: %s\n", srcURL)
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "WeftTunnel should have SrcURL with path")

			GinkgoWriter.Printf("Gateway Fancy URL conformance verified for path matching\n")
		})

		It("should create WeftTunnel with header matching in fancy URL fragment", func() {
			By("Creating WeftGateway with target server")
			weftGateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftGateway",
					"metadata": map[string]interface{}{
						"name":      weftGatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"targetServers": []interface{}{serverName},
					},
				},
			}
			_, err := dynamicClient.Resource(weftGatewayGVR).Namespace(testNamespace).Create(ctx, weftGateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating GatewayClass referencing WeftGateway")
			gatewayClass := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "GatewayClass",
					"metadata": map[string]interface{}{
						"name": gatewayClassName,
					},
					"spec": map[string]interface{}{
						"controllerName": "weft.aquaduct.dev/gateway-controller",
						"parametersRef": map[string]interface{}{
							"group":     "weft.aquaduct.dev",
							"kind":      "WeftGateway",
							"name":      weftGatewayName,
							"namespace": testNamespace,
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayClassGVR).Create(ctx, gatewayClass, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating Gateway")
			gateway := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "Gateway",
					"metadata": map[string]interface{}{
						"name":      gatewayName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"gatewayClassName": gatewayClassName,
						"listeners": []interface{}{
							map[string]interface{}{
								"name":     "http",
								"protocol": "HTTP",
								"port":     int64(80),
								"hostname": "header-api.example.com",
							},
						},
					},
				},
			}
			_, err = dynamicClient.Resource(gatewayGVR).Namespace(testNamespace).Create(ctx, gateway, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Creating HTTPRoute with header matcher")
			httpRoute := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "gateway.networking.k8s.io/v1",
					"kind":       "HTTPRoute",
					"metadata": map[string]interface{}{
						"name":      "header-match-route",
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"parentRefs": []interface{}{
							map[string]interface{}{
								"name": gatewayName,
							},
						},
						"rules": []interface{}{
							map[string]interface{}{
								"matches": []interface{}{
									map[string]interface{}{
										"path": map[string]interface{}{
											"type":  "PathPrefix",
											"value": "/secure",
										},
										"headers": []interface{}{
											map[string]interface{}{
												"type":  "Exact",
												"name":  "Authorization",
												"value": "Bearer.*",
											},
										},
									},
								},
								"backendRefs": []interface{}{
									map[string]interface{}{
										"name": "secure-backend",
										"port": int64(8443),
									},
								},
							},
						},
					},
				},
			}
			_, err = dynamicClient.Resource(httpRouteGVR).Namespace(testNamespace).Create(ctx, httpRoute, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying WeftTunnel is created with header in fragment")
			Eventually(func() bool {
				tunnels, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("gateway=%s", gatewayName),
				})
				if err != nil || len(tunnels.Items) == 0 {
					return false
				}

				for _, tunnel := range tunnels.Items {
					routes, found, _ := unstructured.NestedSlice(tunnel.Object, "spec", "routes")
					if !found || len(routes) == 0 {
						continue
					}
					route := routes[0].(map[string]interface{})
					srcURL, _, _ := unstructured.NestedString(route, "srcURL")
					// Should include header in fragment (fancy URL syntax)
					if srcURL != "" && (strings.Contains(srcURL, "#") && strings.Contains(srcURL, "Authorization=")) {
						GinkgoWriter.Printf("Found WeftTunnel with header fancy URL: %s\n", srcURL)
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "WeftTunnel should have SrcURL with header in fragment")

			GinkgoWriter.Printf("Gateway Fancy URL conformance verified for header matching\n")
		})
	})

	// =========================================================================
	// Multi-Server Tunnel Tests
	// =========================================================================
	Describe("Multi-Server Tunnel", func() {
		It("should create deployments for all WeftServers when no targetServers specified", func() {
			server1Name := "e2e-multi-server-1"
			server2Name := "e2e-multi-server-2"
			tunnelName := "e2e-multi-tunnel"

			By("Creating two WeftServers")
			for _, srvName := range []string{server1Name, server2Name} {
				server := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "weft.aquaduct.dev/v1alpha1",
						"kind":       "WeftServer",
						"metadata": map[string]interface{}{
							"name":      srvName,
							"namespace": testNamespace,
						},
						"spec": map[string]interface{}{
							"connectionString": fmt.Sprintf("weft://multi@%s:8080", srvName),
							"location":         "External",
						},
					},
				}
				_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating WeftTunnel without targetServers (should target all)")
			tunnel := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftTunnel",
					"metadata": map[string]interface{}{
						"name":      tunnelName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"srcURL": "http://multi-svc.default.svc.cluster.local:80",
						"dstURL": "https://multi.example.com",
						// No targetServers - should connect to all
					},
				},
			}
			_, err := dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Create(ctx, tunnel, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Deployments are created for both servers")
			for _, srvName := range []string{server1Name, server2Name} {
				expectedDepName := fmt.Sprintf("tunnel-%s-to-%s", tunnelName, srvName)
				Eventually(func() error {
					_, err := clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
					return err
				}, timeout, interval).Should(Succeed(), "Deployment for %s should be created", srvName)
			}

			// Cleanup
			_ = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Delete(ctx, tunnelName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, server1Name, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, server2Name, metav1.DeleteOptions{})

			GinkgoWriter.Printf("Multi-server tunnel verified - deployments created for all servers\n")
		})
	})

	// =========================================================================
	// Deployment Configuration Tests
	// =========================================================================
	Describe("Deployment Configuration", func() {
		It("should configure tunnel deployment with correct weft CLI args", func() {
			serverName := "e2e-args-server"
			tunnelName := "e2e-args-tunnel"
			srcURL := "http://args-test.default.svc.cluster.local:3000"
			dstURL := "https://args-test.example.com"

			server := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftServer",
					"metadata": map[string]interface{}{
						"name":      serverName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"connectionString": "weft://argstest@10.0.0.50:9092",
						"location":         "External",
					},
				},
			}
			_, err := dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Create(ctx, server, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			tunnel := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "weft.aquaduct.dev/v1alpha1",
					"kind":       "WeftTunnel",
					"metadata": map[string]interface{}{
						"name":      tunnelName,
						"namespace": testNamespace,
					},
					"spec": map[string]interface{}{
						"srcURL":        srcURL,
						"dstURL":        dstURL,
						"targetServers": []interface{}{serverName},
					},
				},
			}
			_, err = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Create(ctx, tunnel, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			expectedDepName := fmt.Sprintf("tunnel-%s-to-%s", tunnelName, serverName)

			By("Waiting for Deployment")
			var dep *appsv1.Deployment
			Eventually(func() error {
				var getErr error
				dep, getErr = clientset.AppsV1().Deployments(testNamespace).Get(ctx, expectedDepName, metav1.GetOptions{})
				return getErr
			}, timeout, interval).Should(Succeed())

			By("Verifying container args match weft CLI format")
			args := dep.Spec.Template.Spec.Containers[0].Args
			Expect(args[0]).To(Equal("tunnel"), "First arg should be 'tunnel' command")
			Expect(args).To(ContainElement(fmt.Sprintf("--tunnel-name=%s", tunnelName)))
			Expect(args).To(ContainElement("weft://argstest@10.0.0.50:9092")) // Connection string
			Expect(args).To(ContainElement(srcURL))
			Expect(args).To(ContainElement(dstURL))

			// Cleanup
			_ = dynamicClient.Resource(weftTunnelGVR).Namespace(testNamespace).Delete(ctx, tunnelName, metav1.DeleteOptions{})
			_ = dynamicClient.Resource(weftServerGVR).Namespace(testNamespace).Delete(ctx, serverName, metav1.DeleteOptions{})

			GinkgoWriter.Printf("Deployment args verified: tunnel --tunnel-name=%s <conn> %s %s\n", tunnelName, srcURL, dstURL)
		})
	})
})
