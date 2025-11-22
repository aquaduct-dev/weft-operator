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

package weftgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	ControllerName = "weft.aquaduct.dev/gateway-controller"
)

// WeftGatewayReconciler reconciles a Gateway object
type WeftGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftgateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *WeftGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var gateway gatewayv1.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gateway); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if this Gateway is managed by us
	var gwClass gatewayv1.GatewayClass
	if err := r.Get(ctx, types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, &gwClass); err != nil {
		log.Error(err, "Failed to get GatewayClass", "gatewayClass", gateway.Spec.GatewayClassName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if gwClass.Spec.ControllerName != ControllerName {
		return ctrl.Result{}, nil
	}

	// Get WeftGateway config if present
	var targetServers []string
	if gwClass.Spec.ParametersRef != nil &&
		gwClass.Spec.ParametersRef.Group == gatewayv1.Group(weftv1alpha1.GroupVersion.Group) &&
		gwClass.Spec.ParametersRef.Kind == "WeftGateway" {

		var weftGwConfig weftv1alpha1.WeftGateway
		// Assuming namespace is specified or same as GatewayClass (GatewayClass is cluster-scoped, but params can be namespaced)
		// Usually paramRef has Namespace field.
		ns := gwClass.Spec.ParametersRef.Namespace
		if ns == nil {
			// If namespace is not specified for Namespaced resource, it's invalid reference usually,
			// but for Cluster scoped it works. WeftGateway is namespaced.
			// Let's assume it's in "default" or we skip.
			// Actually, Gateway API spec says: "If the referent is a Namespaced resource, the namespace MUST be specified."
			// We'll assume it is provided.
			log.Info("ParametersRef Namespace is nil, skipping config lookup")
		} else {
			if err := r.Get(ctx, types.NamespacedName{Name: gwClass.Spec.ParametersRef.Name, Namespace: string(*ns)}, &weftGwConfig); err != nil {
				log.Error(err, "Failed to get WeftGateway parameters")
				// We can continue without config
			} else {
				targetServers = weftGwConfig.Spec.TargetServers
			}
		}
	}

	// Find HTTPRoutes attached to this Gateway
	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	expectedTunnels := make(map[string]bool)

	for _, route := range httpRoutes.Items {
		if !r.isRouteAttachedToGateway(&route, &gateway) {
			continue
		}

		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				// We only support Service backends
				kind := "Service"
				if backend.Kind != nil {
					kind = string(*backend.Kind)
				}
				if kind != "Service" {
					continue
				}

				// Construct DstURL
				// http://<service>.<namespace>.svc:<port>
				ns := route.Namespace
				if backend.Namespace != nil {
					ns = string(*backend.Namespace)
				}
				
				port := int32(80)
				if backend.Port != nil {
					port = int32(*backend.Port)
				}

				dstURL := fmt.Sprintf("http://%s.%s.svc:%d", backend.Name, ns, port)

				// Construct SrcURL
				// Matches are complicated. Simplifying:
				// If we have path match, append to gateway listener hostname.
				// Gateway Listeners:
				for _, listener := range gateway.Spec.Listeners {
					// Check if route attaches to this listener (simplified)
					
					// Assume Listener Hostname is the base
					if listener.Hostname == nil {
						continue
					}
					baseURL := fmt.Sprintf("http://%s", *listener.Hostname) // Assume HTTP for now

					for _, match := range rule.Matches {
						path := "/"
						if match.Path != nil && match.Path.Value != nil {
							path = *match.Path.Value
						}

						fullSrcURL, _ := url.JoinPath(baseURL, path)

						// Generate Tunnel Name
						hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, fullSrcURL)))
						hashStr := hex.EncodeToString(hash[:])[:8]
						tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
						
						expectedTunnels[tunnelName] = true

						tunnel := &weftv1alpha1.WeftTunnel{
							ObjectMeta: metav1.ObjectMeta{
								Name:      tunnelName,
								Namespace: gateway.Namespace,
							},
						}

						op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tunnel, func() error {
							tunnel.Spec.TargetServers = targetServers
							tunnel.Spec.SrcURL = fullSrcURL
							tunnel.Spec.DstURL = dstURL

							labels := map[string]string{
								"app":        "weft-gateway-tunnel",
								"gateway":    gateway.Name,
								"route":      route.Name,
								"created-by": "weft-operator",
							}
							tunnel.ObjectMeta.Labels = labels
							return controllerutil.SetControllerReference(&gateway, tunnel, r.Scheme)
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
			}
		}
	}

	// Prune obsolete tunnels
	var tunnelList weftv1alpha1.WeftTunnelList
	if err := r.List(ctx, &tunnelList, client.InNamespace(req.Namespace), client.MatchingLabels{"gateway": gateway.Name, "created-by": "weft-operator"}); err != nil {
		return ctrl.Result{}, err
	}

	for _, t := range tunnelList.Items {
		if !expectedTunnels[t.Name] {
			if metav1.IsControlledBy(&t, &gateway) {
				log.Info("Deleting obsolete WeftTunnel", "tunnel", t.Name)
				if err := r.Delete(ctx, &t); err != nil {
					log.Error(err, "Failed to delete obsolete WeftTunnel", "tunnel", t.Name)
					return ctrl.Result{}, err
				}
			}
		}
	}
	
	// Update Status (Simplified)
	return ctrl.Result{}, r.updateGatewayStatus(ctx, &gateway)
}

func (r *WeftGatewayReconciler) isRouteAttachedToGateway(route *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway) bool {
	for _, parent := range route.Spec.ParentRefs {
		if string(parent.Name) == gateway.Name {
			// Also check Namespace if present
			if parent.Namespace != nil && string(*parent.Namespace) != gateway.Namespace {
				continue
			}
			return true
		}
	}
	return false
}


func (r *WeftGatewayReconciler) updateGatewayStatus(ctx context.Context, gw *gatewayv1.Gateway) error {
	// Determine condition based on Tunnel status?
	// For now, just mark Accepted/Programmed
	
	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:    string(gatewayv1.GatewayConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.GatewayReasonAccepted),
		Message: "Gateway accepted by weft-operator",
		ObservedGeneration: gw.Generation,
	})
	
	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:    string(gatewayv1.GatewayConditionProgrammed),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.GatewayReasonProgrammed),
		Message: "Gateway programmed",
		ObservedGeneration: gw.Generation,
	})

	return r.Status().Update(ctx, gw)
}


// SetupWithManager sets up the controller with the Manager.
func (r *WeftGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Owns(&weftv1alpha1.WeftTunnel{}).
		Watches(
			&gatewayv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForRoute),
		).
		Complete(r)
}

func (r *WeftGatewayReconciler) findGatewaysForRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return nil
	}

	var requests []reconcile.Request
	for _, parent := range route.Spec.ParentRefs {
		// Check Kind/Group if necessary (defaults to Gateway/gateway.networking.k8s.io)
		
		ns := route.Namespace
		if parent.Namespace != nil {
			ns = string(*parent.Namespace)
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      string(parent.Name),
				Namespace: ns,
			},
		})
	}
	return requests
}
