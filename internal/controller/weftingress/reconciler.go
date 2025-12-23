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

// Package weftingress provides a controller that watches Kubernetes Ingress resources
// and translates them into WeftTunnel resources for tunneling traffic through Weft.
package weftingress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/resource"
)

const (
	// ControllerName is the name used to identify this controller in IngressClass.
	ControllerName = "weft.aquaduct.dev/ingress-controller"
)

// WeftIngressReconciler reconciles Kubernetes Ingress objects and creates WeftTunnels.
type WeftIngressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Ingress resources and creates corresponding WeftTunnels.
func (r *WeftIngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var ingress networkingv1.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ingress); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if this Ingress is managed by us
	if ingress.Spec.IngressClassName == nil {
		return ctrl.Result{}, nil
	}

	var ingressClass networkingv1.IngressClass
	if err := r.Get(ctx, types.NamespacedName{Name: *ingress.Spec.IngressClassName}, &ingressClass); err != nil {
		log.Error(err, "Failed to get IngressClass", "ingressClass", *ingress.Spec.IngressClassName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if ingressClass.Spec.Controller != ControllerName {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling Ingress", "name", ingress.Name, "namespace", ingress.Namespace)

	expectedTunnels := make(map[string]bool)

	// Build a set of TLS hosts for determining URL scheme
	tlsHosts := make(map[string]bool)
	for _, tls := range ingress.Spec.TLS {
		for _, host := range tls.Hosts {
			tlsHosts[host] = true
		}
	}

	// Process all rules in the Ingress
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		host := rule.Host
		if host == "" {
			host = "*" // Default host
		}

		// Determine URL scheme based on TLS configuration
		scheme := "http"
		if tlsHosts[host] {
			scheme = "https"
		}

		for _, path := range rule.HTTP.Paths {
			backend := path.Backend
			if backend.Service == nil {
				continue
			}

			// Construct DstURL: http://<service>.<namespace>.svc:<port>
			port := int32(80)
			if backend.Service.Port.Number != 0 {
				port = backend.Service.Port.Number
			}
			dstURL := fmt.Sprintf("http://%s.%s.svc:%d", backend.Service.Name, ingress.Namespace, port)

			// Construct SrcURL with appropriate scheme based on TLS
			pathValue := "/"
			if path.Path != "" {
				pathValue = path.Path
			}
			srcURL := fmt.Sprintf("%s://%s%s", scheme, host, pathValue)

			// Generate tunnel name using hash for uniqueness
			hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", ingress.Name, host, pathValue)))
			hashStr := hex.EncodeToString(hash[:])[:8]
			tunnelName := fmt.Sprintf("ing-%s-%s", ingress.Name, hashStr)
			expectedTunnels[tunnelName] = true

			tunnel := &weftv1alpha1.WeftTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tunnelName,
					Namespace: ingress.Namespace,
				},
			}

			op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tunnel, func() error {
				// Use Routes field (SrcURL/DstURL are deprecated)
				tunnel.Spec.Routes = []weftv1alpha1.TunnelRoute{
					{SrcURL: srcURL, DstURL: dstURL},
				}
				labels := map[string]string{
					"app":        "weft-ingress-tunnel",
					"ingress":    ingress.Name,
					"created-by": "weft-operator",
				}
				tunnel.ObjectMeta.Labels = labels
				return controllerutil.SetControllerReference(&ingress, tunnel, r.Scheme)
			})
			if err != nil {
				log.Error(err, "Failed to reconcile WeftTunnel", "tunnel", tunnelName)
				return ctrl.Result{}, err
			}

			if op != controllerutil.OperationResultNone {
				log.Info("WeftTunnel reconciled", "tunnel", tunnelName, "operation", op)
			}
		}
	}

	// Prune obsolete tunnels
	var tunnelList weftv1alpha1.WeftTunnelList
	if err := r.List(ctx, &tunnelList, client.InNamespace(req.Namespace), client.MatchingLabels{"ingress": ingress.Name, "created-by": "weft-operator"}); err != nil {
		return ctrl.Result{}, err
	}

	for _, t := range tunnelList.Items {
		tunnelToDelete := t
		if !metav1.IsControlledBy(&tunnelToDelete, &ingress) {
			continue
		}

		_, err := resource.Resource(resource.Options{
			Name: fmt.Sprintf("wefttunnel/%s", tunnelToDelete.Name),
			Log:  func(v ...any) { log.Info(fmt.Sprint(v...)) },
			Exists: func() bool {
				return true
			},
			ShouldExist: func() bool {
				return expectedTunnels[tunnelToDelete.Name]
			},
			IsUpToDate: func() bool {
				return true
			},
			Delete: func() error {
				log.Info("Deleting obsolete WeftTunnel", "tunnel", tunnelToDelete.Name)
				return r.Delete(ctx, &tunnelToDelete)
			},
		})
		if err != nil {
			log.Error(err, "Failed to delete obsolete WeftTunnel", "tunnel", tunnelToDelete.Name)
			return ctrl.Result{}, err
		}
	}

	// Update Ingress status
	return ctrl.Result{}, r.updateIngressStatus(ctx, &ingress)
}

func (r *WeftIngressReconciler) updateIngressStatus(ctx context.Context, ingress *networkingv1.Ingress) error {
	// Ingress status uses LoadBalancer, not Conditions.
	// For now, we don't set LoadBalancer status since we don't have external IPs.
	// The WeftServer handles the actual traffic routing.
	// In the future, we could populate this with the WeftServer's external IP.
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WeftIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&weftv1alpha1.WeftTunnel{}).
		Complete(r)
}
