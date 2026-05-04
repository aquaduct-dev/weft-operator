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
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"aquaduct.dev/weft-operator/internal/resource"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	ControllerName = "weft.aquaduct.dev/gateway-controller"

	// dnsFinalizer keeps the Gateway around until DNSRecords this
	// reconciler stamped on its behalf have been removed. DNSRecords
	// live in the AquaductTaaS's namespace (typically weft-system),
	// not the Gateway's, so cross-namespace ownerReferences are
	// unavailable — finalizer-driven cleanup is the standard escape.
	dnsFinalizer = "weft.aquaduct.dev/cleanup-dnsrecords"

	// Labels stamped on every DNSRecord we create so we can list+prune
	// them without re-deriving from spec, and so operators can easily
	// trace a record back to the Gateway that asked for it.
	labelGatewayNamespace = "weft.aquaduct.dev/gateway-namespace"
	labelGatewayName      = "weft.aquaduct.dev/gateway-name"
	labelCreatedBy        = "created-by"
	createdByValue        = "weft-operator"
)

// WeftGatewayReconciler reconciles a Gateway object
type WeftGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tlsroutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=udproutes,verbs=get;list;watch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftgateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=aquaducttaases,verbs=get;list;watch

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
		// Return error with requeue to retry when GatewayClass becomes available
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	if gwClass.Spec.ControllerName != ControllerName {
		return ctrl.Result{}, nil
	}

	// Finalizer + deletion handling. Stamp before any side-effects so
	// the cleanup path can rely on it. DNSRecords are cluster-side
	// state that survives Gateway deletion if not explicitly removed —
	// the finalizer is what forces the cleanup to run before the
	// Gateway disappears.
	if !gateway.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&gateway, dnsFinalizer) {
			if err := r.cleanupDNSRecords(ctx, &gateway); err != nil {
				log.Error(err, "Failed to clean up DNSRecords for deleted Gateway")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&gateway, dnsFinalizer)
			if err := r.Update(ctx, &gateway); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	if !controllerutil.ContainsFinalizer(&gateway, dnsFinalizer) {
		controllerutil.AddFinalizer(&gateway, dnsFinalizer)
		if err := r.Update(ctx, &gateway); err != nil {
			return ctrl.Result{}, err
		}
		// Continue rather than return Requeue:true. r.Update mutates
		// gateway.ResourceVersion in place so the later Status().Update
		// won't conflict, and creating the tunnels + DNSRecords in the
		// same reconcile keeps single-call test semantics intact.
	}

	// Update GatewayClass status to indicate we've accepted it
	if err := r.updateGatewayClassStatus(ctx, &gwClass); err != nil {
		log.Error(err, "Failed to update GatewayClass status")
		// Don't fail the reconciliation for status update failure
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

	// Find HTTPRoutes attached to this Gateway. We list cluster-wide because
	// Gateway API permits routes in any namespace to reference a Gateway when
	// the Gateway's listener allowedRoutes.namespaces.from is "All" (or matches
	// via "Selector"); isRouteAttachedToGateway below cross-checks each route's
	// parentRefs against this Gateway, so listing cluster-wide is safe.
	// WeftTunnels we create are still placed in gateway.Namespace.
	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes); err != nil {
		return ctrl.Result{}, err
	}

	expectedTunnels := make(map[string]bool)

	for _, route := range httpRoutes.Items {
		if !r.isRouteAttachedToGateway(&route, &gateway) {
			continue
		}

		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				kind := "Service"
				if backend.Kind != nil {
					kind = string(*backend.Kind)
				}
				if kind != "Service" {
					continue
				}

				// Construct SrcURL (internal cluster service)
				// http://<service>.<namespace>.svc:<port>
				ns := route.Namespace
				if backend.Namespace != nil {
					ns = string(*backend.Namespace)
				}

				port := int32(80)
				if backend.Port != nil {
					port = int32(*backend.Port)
				}

				srcURL := fmt.Sprintf("http://%s.%s.svc:%d", backend.Name, ns, port) + httpFiltersToFragment(rule.Filters)

				// Construct DstURL (external hostname + path)
				// Matches are complicated. Simplifying:
				// If we have path match, append to gateway listener hostname.
				// Gateway Listeners:
				for _, listener := range gateway.Spec.Listeners {
					// Check if route attaches to this listener (simplified)

					// Assume Listener Hostname is the base for DstURL (external hostname)
					if listener.Hostname == nil {
						continue
					}
					// Determine scheme based on listener protocol
					scheme := "http"
					if listener.Protocol == gatewayv1.HTTPSProtocolType {
						scheme = "https"
					}
					baseURL := fmt.Sprintf("%s://%s", scheme, *listener.Hostname)

					for _, match := range rule.Matches {
						path := "/"
						if match.Path != nil && match.Path.Value != nil {
							path = *match.Path.Value
						}

						dstURL, _ := url.JoinPath(baseURL, path)

						// Generate Tunnel Name

						hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, dstURL)))
						hashStr := hex.EncodeToString(hash[:])[:8]
						tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
						expectedTunnels[tunnelName] = true
						tunnel := &weftv1alpha1.WeftTunnel{
							ObjectMeta: metav1.ObjectMeta{
								Name: tunnelName,

								Namespace: gateway.Namespace,
							},
						}
						op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tunnel, func() error {
							tunnel.Spec.TargetServers = targetServers
							tunnel.Spec.SrcURL = srcURL
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

	// Find TCPRoutes attached to this Gateway
	var tcpRoutes gatewayv1alpha2.TCPRouteList
	if err := r.List(ctx, &tcpRoutes, client.InNamespace(req.Namespace)); err != nil {
		log.Error(err, "Failed to list TCPRoutes")
		// Continue with other route types
	} else {
		for _, route := range tcpRoutes.Items {
			if !r.isTCPRouteAttachedToGateway(&route, &gateway) {
				continue
			}

			for _, rule := range route.Spec.Rules {
				for _, backend := range rule.BackendRefs {
					kind := "Service"
					if backend.Kind != nil {
						kind = string(*backend.Kind)
					}
					if kind != "Service" {
						continue
					}

					ns := route.Namespace
					if backend.Namespace != nil {
						ns = string(*backend.Namespace)
					}

					port := int32(0)
					if backend.Port != nil {
						port = int32(*backend.Port)
					}

					srcURL := fmt.Sprintf("tcp://%s.%s.svc:%d", backend.Name, ns, port)

					// For TCP routes, use TCP listener to determine destination
					for _, listener := range gateway.Spec.Listeners {
						if listener.Protocol != gatewayv1.TCPProtocolType {
							continue
						}
						if listener.Hostname == nil {
							// TCP routes typically use port-based routing, not hostname
							// Use the listener port as the destination
							dstURL := fmt.Sprintf("tcp://:%d", listener.Port)

							hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, dstURL)))
							hashStr := hex.EncodeToString(hash[:])[:8]
							tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
							expectedTunnels[tunnelName] = true

							if err := r.createOrUpdateTunnel(ctx, tunnelName, srcURL, dstURL, targetServers, &gateway, route.Name, log); err != nil {
								return ctrl.Result{}, err
							}
						} else {
							// Hostname-based TCP routing
							dstURL := fmt.Sprintf("tcp://%s:%d", *listener.Hostname, listener.Port)

							hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, dstURL)))
							hashStr := hex.EncodeToString(hash[:])[:8]
							tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
							expectedTunnels[tunnelName] = true

							if err := r.createOrUpdateTunnel(ctx, tunnelName, srcURL, dstURL, targetServers, &gateway, route.Name, log); err != nil {
								return ctrl.Result{}, err
							}
						}
					}
				}
			}
		}
	}

	// Find TLSRoutes attached to this Gateway (TLS passthrough)
	var tlsRoutes gatewayv1alpha2.TLSRouteList
	if err := r.List(ctx, &tlsRoutes, client.InNamespace(req.Namespace)); err != nil {
		log.Error(err, "Failed to list TLSRoutes")
		// Continue with other route types
	} else {
		for _, route := range tlsRoutes.Items {
			if !r.isTLSRouteAttachedToGateway(&route, &gateway) {
				continue
			}

			for _, rule := range route.Spec.Rules {
				for _, backend := range rule.BackendRefs {
					kind := "Service"
					if backend.Kind != nil {
						kind = string(*backend.Kind)
					}
					if kind != "Service" {
						continue
					}

					ns := route.Namespace
					if backend.Namespace != nil {
						ns = string(*backend.Namespace)
					}

					port := int32(443)
					if backend.Port != nil {
						port = int32(*backend.Port)
					}

					// TLS passthrough: both src and dst use https (re-encrypt)
					srcURL := fmt.Sprintf("https://%s.%s.svc:%d", backend.Name, ns, port)

					for _, listener := range gateway.Spec.Listeners {
						if listener.Protocol != gatewayv1.TLSProtocolType {
							continue
						}
						if listener.Hostname == nil {
							continue
						}

						dstURL := fmt.Sprintf("https://%s", *listener.Hostname)

						hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, dstURL)))
						hashStr := hex.EncodeToString(hash[:])[:8]
						tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
						expectedTunnels[tunnelName] = true

						if err := r.createOrUpdateTunnel(ctx, tunnelName, srcURL, dstURL, targetServers, &gateway, route.Name, log); err != nil {
							return ctrl.Result{}, err
						}
					}
				}
			}
		}
	}

	// Find UDPRoutes attached to this Gateway
	var udpRoutes gatewayv1alpha2.UDPRouteList
	if err := r.List(ctx, &udpRoutes, client.InNamespace(req.Namespace)); err != nil {
		log.Error(err, "Failed to list UDPRoutes")
		// Continue with other route types
	} else {
		for _, route := range udpRoutes.Items {
			if !r.isUDPRouteAttachedToGateway(&route, &gateway) {
				continue
			}

			for _, rule := range route.Spec.Rules {
				for _, backend := range rule.BackendRefs {
					kind := "Service"
					if backend.Kind != nil {
						kind = string(*backend.Kind)
					}
					if kind != "Service" {
						continue
					}

					ns := route.Namespace
					if backend.Namespace != nil {
						ns = string(*backend.Namespace)
					}

					port := int32(0)
					if backend.Port != nil {
						port = int32(*backend.Port)
					}

					srcURL := fmt.Sprintf("udp://%s.%s.svc:%d", backend.Name, ns, port)

					for _, listener := range gateway.Spec.Listeners {
						if listener.Protocol != gatewayv1.UDPProtocolType {
							continue
						}

						dstURL := fmt.Sprintf("udp://:%d", listener.Port)
						if listener.Hostname != nil {
							dstURL = fmt.Sprintf("udp://%s:%d", *listener.Hostname, listener.Port)
						}

						hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway.Name, route.Name, dstURL)))
						hashStr := hex.EncodeToString(hash[:])[:8]
						tunnelName := fmt.Sprintf("gw-%s-%s", gateway.Name, hashStr)
						expectedTunnels[tunnelName] = true

						if err := r.createOrUpdateTunnel(ctx, tunnelName, srcURL, dstURL, targetServers, &gateway, route.Name, log); err != nil {
							return ctrl.Result{}, err
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
		tunnelToDelete := t // Create a copy for the closure
		// Only consider tunnels owned by this gateway.
		if !metav1.IsControlledBy(&tunnelToDelete, &gateway) {
			continue
		}

		_, err := resource.Resource(resource.Options{
			Name: fmt.Sprintf("wefttunnel/%s", tunnelToDelete.Name),
			Log:  func(v ...any) { log.Info(fmt.Sprint(v...)) },
			Exists: func() bool {
				// We listed it, so it exists.
				// The outer check for IsControlledBy ensures we don't accidentally delete unowned tunnels.
				return true
			},
			ShouldExist: func() bool {
				return expectedTunnels[tunnelToDelete.Name] // Should only exist if in expectedTunnels
			},
			IsUpToDate: func() bool {
				// If it exists and should exist, we assume it's up to date for the purpose of this pruning loop.
				// Actual reconciliation happens in the creation loop above.
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

	// Stamp DNSRecord per listener.hostname so external DNS resolves
	// without anyone hand-applying CRs. Failure is logged but doesn't
	// fail the whole reconcile — tunnels are still useful even if DNS
	// is misconfigured (operators can hand-stamp records as a fallback).
	if err := r.reconcileDNSRecords(ctx, &gateway); err != nil {
		log.Error(err, "Failed to reconcile DNSRecords")
	}

	// Update Status (Simplified)
	return ctrl.Result{}, r.updateGatewayStatus(ctx, &gateway)
}

// reconcileDNSRecords creates one DNSRecord per unique listener
// hostname on the Gateway. Records live in the AquaductTaaS's
// namespace (DNSRecord.spec.aquaductTaaSRef is same-namespace only)
// and are tracked back to the Gateway by labels. Records this
// Gateway previously stamped that no longer correspond to a current
// listener hostname are pruned.
func (r *WeftGatewayReconciler) reconcileDNSRecords(ctx context.Context, gateway *gatewayv1.Gateway) error {
	log := log.FromContext(ctx)

	taas, err := r.findSingletonTaaS(ctx)
	if err != nil {
		return err
	}
	if taas == nil {
		// Zero or many TaaS objects: autodns is opt-in via "exactly
		// one TaaS in the cluster", so silently skip in either case.
		// The "many" case logs because it's almost always misconfig.
		return nil
	}

	specs := classifyListenerHostnames(gateway)
	expected := make(map[string]bool, len(specs))

	for _, hs := range specs {
		// L7 hostnames (HTTP/HTTPS/TLS) keep the "fan to all" default —
		// nil TargetBastionIDs lets every non-suspended bastion serve
		// the hostname, since L7 demuxes by Host header / SNI. L4
		// hostnames (any TCP/UDP listener) get pinned to a single
		// bastion: an L4 listener owns its bastion's port exclusively,
		// so multi-bastion fan-out would either waste port slots on
		// every other bastion or collide with sibling tunnels.
		var targetBastionIDs []string
		if hs.L4 {
			id := pickL4Bastion(taas.Status.Bastions, gateway, hs.Hostname)
			if id == "" {
				// No eligible bastion → don't stamp a record at all.
				// We can't write an "explicit empty set" because
				// omitempty on TargetBastionIDs collapses []vs nil
				// after a round-trip, which would silently degrade to
				// "fan to all" — exactly the wrong behavior for L4.
				// The AquaductTaaS watch re-enqueues this Gateway when
				// the bastion list changes, so the record materializes
				// as soon as a bastion is eligible.
				log.Info("Skipping DNSRecord for L4 hostname: no eligible bastion in TaaS",
					"hostname", hs.Hostname, "gateway", gateway.Name)
				continue
			}
			targetBastionIDs = []string{id}
		}

		recordName := dnsRecordName(gateway, hs.Hostname)
		expected[recordName] = true
		dr := &weftv1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      recordName,
				Namespace: taas.Namespace,
			},
		}
		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dr, func() error {
			dr.Spec.DomainName = hs.Hostname
			dr.Spec.AquaductTaaSRef.Name = taas.Name
			dr.Spec.TargetBastionIDs = targetBastionIDs
			if dr.Labels == nil {
				dr.Labels = map[string]string{}
			}
			dr.Labels[labelGatewayNamespace] = gateway.Namespace
			dr.Labels[labelGatewayName] = gateway.Name
			dr.Labels[labelCreatedBy] = createdByValue
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconcile DNSRecord %s/%s: %w", dr.Namespace, dr.Name, err)
		}
		if op != controllerutil.OperationResultNone {
			log.Info("DNSRecord reconciled", "dnsrecord", dr.Name, "hostname", hs.Hostname, "l4", hs.L4, "targetBastionIDs", targetBastionIDs, "operation", op)
		}
	}

	// Prune DNSRecords this Gateway previously owned that no longer
	// match a current listener hostname. Selector pins both gateway
	// labels so we can't accidentally delete records owned by another
	// Gateway in another namespace that happens to share a name.
	var owned weftv1alpha1.DNSRecordList
	if err := r.List(ctx, &owned,
		client.InNamespace(taas.Namespace),
		client.MatchingLabels{
			labelGatewayNamespace: gateway.Namespace,
			labelGatewayName:      gateway.Name,
			labelCreatedBy:        createdByValue,
		},
	); err != nil {
		return fmt.Errorf("list owned DNSRecords: %w", err)
	}
	for i := range owned.Items {
		dr := &owned.Items[i]
		if expected[dr.Name] {
			continue
		}
		log.Info("Deleting obsolete DNSRecord", "dnsrecord", dr.Name)
		if err := r.Delete(ctx, dr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete obsolete DNSRecord %s/%s: %w", dr.Namespace, dr.Name, err)
		}
	}
	return nil
}

// cleanupDNSRecords removes every DNSRecord this reconciler stamped
// for the Gateway. Called from the deletion path before the
// finalizer is removed; the DNSRecord controller's own finalizer
// then runs the aquaduct.dev DELETE /domain/{name} call. Success
// here just means we initiated the deletion — the DNSRecord may
// linger briefly while its own finalizer drains.
func (r *WeftGatewayReconciler) cleanupDNSRecords(ctx context.Context, gateway *gatewayv1.Gateway) error {
	taas, err := r.findSingletonTaaS(ctx)
	if err != nil {
		return err
	}
	if taas == nil {
		// No TaaS to scope by — nothing we could have created.
		return nil
	}

	var owned weftv1alpha1.DNSRecordList
	if err := r.List(ctx, &owned,
		client.InNamespace(taas.Namespace),
		client.MatchingLabels{
			labelGatewayNamespace: gateway.Namespace,
			labelGatewayName:      gateway.Name,
			labelCreatedBy:        createdByValue,
		},
	); err != nil {
		return fmt.Errorf("list DNSRecords for cleanup: %w", err)
	}
	for i := range owned.Items {
		dr := &owned.Items[i]
		if err := r.Delete(ctx, dr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete DNSRecord %s/%s: %w", dr.Namespace, dr.Name, err)
		}
	}
	return nil
}

// findSingletonTaaS returns the cluster's unique AquaductTaaS, or
// nil if zero or more than one exist. Autodns is intentionally
// opt-in via "exactly one TaaS" — multi-tenant clusters need to wire
// the TaaS choice through a parametersRef instead, which is a
// follow-up.
func (r *WeftGatewayReconciler) findSingletonTaaS(ctx context.Context) (*weftv1alpha1.AquaductTaaS, error) {
	log := log.FromContext(ctx)
	var list weftv1alpha1.AquaductTaaSList
	if err := r.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list AquaductTaaS: %w", err)
	}
	switch len(list.Items) {
	case 0:
		return nil, nil
	case 1:
		return &list.Items[0], nil
	default:
		log.Info("Multiple AquaductTaaS objects in cluster; autodns disabled until a single TaaS is selected (parametersRef wiring pending)",
			"count", len(list.Items))
		return nil, nil
	}
}

// hostnameSpec is the per-hostname classification used to decide
// bastion association for DNSRecord stamping. L4=true marks a
// hostname whose listener set includes any TCP or UDP protocol —
// those can't share a bastion's IP+port the way HTTP/HTTPS/TLS can
// (L7 demuxes by Host header, TLS by SNI), so they get pinned to
// one bastion instead of fanning to all.
type hostnameSpec struct {
	Hostname string
	L4       bool
}

// classifyListenerHostnames returns the deduplicated set of non-empty
// listener hostnames on the Gateway, in stable encounter order, with
// each hostname tagged L4 if any of its listeners uses a port-bound
// protocol. The "any listener" rule is intentionally pessimistic: a
// hostname with mixed HTTPS+TCP listeners must still be pinned to
// one bastion, because the TCP listener can't be served from a
// fanned-out set.
func classifyListenerHostnames(gateway *gatewayv1.Gateway) []hostnameSpec {
	indexByHost := make(map[string]int, len(gateway.Spec.Listeners))
	out := make([]hostnameSpec, 0, len(gateway.Spec.Listeners))
	for _, l := range gateway.Spec.Listeners {
		if l.Hostname == nil || *l.Hostname == "" {
			continue
		}
		h := string(*l.Hostname)
		idx, ok := indexByHost[h]
		if !ok {
			indexByHost[h] = len(out)
			out = append(out, hostnameSpec{Hostname: h})
			idx = len(out) - 1
		}
		if isL4Protocol(l.Protocol) {
			out[idx].L4 = true
		}
	}
	return out
}

func isL4Protocol(p gatewayv1.ProtocolType) bool {
	return p == gatewayv1.TCPProtocolType || p == gatewayv1.UDPProtocolType
}

// pickL4Bastion deterministically selects exactly one bastion ID for
// an L4 hostname. Stability across reconciles for the same (gateway,
// hostname) input matters: it keeps DNSRecord.spec churn-free when
// the bastion list is unchanged, and the hash distributes different
// hostnames across the fleet so one bastion isn't carrying every L4
// tunnel. Returns "" when no bastion is eligible (suspended or
// missing IP) — caller writes an empty TargetBastionIDs slice so
// the DNSRecord surfaces "NoTargets" until a bastion comes back.
func pickL4Bastion(bastions []weftv1alpha1.BastionInfo, gateway *gatewayv1.Gateway, hostname string) string {
	eligible := eligibleBastionIDs(bastions)
	if len(eligible) == 0 {
		return ""
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s", gateway.Namespace, gateway.Name, hostname)))
	idx := binary.BigEndian.Uint64(h[:8]) % uint64(len(eligible))
	return eligible[idx]
}

// eligibleBastionIDs returns the IDs of bastions that can actually
// receive L4 traffic, in deterministic (sorted) order so the hash
// pick in pickL4Bastion is reproducible across reconciles.
func eligibleBastionIDs(bastions []weftv1alpha1.BastionInfo) []string {
	out := make([]string, 0, len(bastions))
	for _, b := range bastions {
		if b.Suspended || b.IP == "" {
			continue
		}
		out = append(out, b.ID)
	}
	sort.Strings(out)
	return out
}

// dnsRecordName encodes the (gateway-namespace, gateway-name,
// hostname) triple into a stable, DNS-1123-safe object name.
// Including the namespace prevents collisions between same-named
// Gateways in different namespaces stamping records into the same
// TaaS namespace.
func dnsRecordName(gateway *gatewayv1.Gateway, hostname string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s", gateway.Namespace, gateway.Name, hostname)))
	return fmt.Sprintf("gw-%s-%s", gateway.Name, hex.EncodeToString(hash[:])[:10])
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

func (r *WeftGatewayReconciler) updateGatewayClassStatus(ctx context.Context, gwClass *gatewayv1.GatewayClass) error {
	// Only update if not already accepted
	for _, cond := range gwClass.Status.Conditions {
		if cond.Type == string(gatewayv1.GatewayClassConditionStatusAccepted) && cond.Status == metav1.ConditionTrue {
			return nil // Already accepted
		}
	}

	meta.SetStatusCondition(&gwClass.Status.Conditions, metav1.Condition{
		Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayClassReasonAccepted),
		Message:            "GatewayClass accepted by weft-operator",
		ObservedGeneration: gwClass.Generation,
	})

	return r.Status().Update(ctx, gwClass)
}

func (r *WeftGatewayReconciler) updateGatewayStatus(ctx context.Context, gw *gatewayv1.Gateway) error {
	// Determine condition based on Tunnel status?
	// For now, just mark Accepted/Programmed

	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonAccepted),
		Message:            "Gateway accepted by weft-operator",
		ObservedGeneration: gw.Generation,
	})

	meta.SetStatusCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonProgrammed),
		Message:            "Gateway programmed",
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
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForHTTPRoute),
		).
		Watches(
			&gatewayv1alpha2.TCPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForTCPRoute),
		).
		Watches(
			&gatewayv1alpha2.TLSRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForTLSRoute),
		).
		Watches(
			&gatewayv1alpha2.UDPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForUDPRoute),
		).
		Watches(
			&gatewayv1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForClass),
		).
		Watches(
			&weftv1alpha1.AquaductTaaS{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForAquaductTaaS),
		).
		Complete(r)
}

// findGatewaysForAquaductTaaS re-enqueues every Gateway managed by
// this controller when the singleton AquaductTaaS changes. This is
// what keeps L4 bastion pinning fresh: if the TaaS publishes a new
// bastion list (added, removed, suspended), each Gateway re-runs
// pickL4Bastion against the new list and the DNSRecord spec follows.
// Without this, a suspended L4-pinned bastion would leave the
// hostname pointing at a dead IP until something else triggered a
// reconcile.
func (r *WeftGatewayReconciler) findGatewaysForAquaductTaaS(ctx context.Context, _ client.Object) []reconcile.Request {
	var gatewayList gatewayv1.GatewayList
	if err := r.List(ctx, &gatewayList); err != nil {
		return nil
	}
	classCache := make(map[string]bool)
	var requests []reconcile.Request
	for _, gw := range gatewayList.Items {
		className := string(gw.Spec.GatewayClassName)
		managed, cached := classCache[className]
		if !cached {
			var gwClass gatewayv1.GatewayClass
			if err := r.Get(ctx, types.NamespacedName{Name: className}, &gwClass); err != nil {
				classCache[className] = false
				continue
			}
			managed = gwClass.Spec.ControllerName == ControllerName
			classCache[className] = managed
		}
		if !managed {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace},
		})
	}
	return requests
}

func (r *WeftGatewayReconciler) findGatewaysForHTTPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		return nil
	}
	return r.findGatewaysFromParentRefs(route.Namespace, route.Spec.ParentRefs)
}

func (r *WeftGatewayReconciler) findGatewaysForTCPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gatewayv1alpha2.TCPRoute)
	if !ok {
		return nil
	}
	return r.findGatewaysFromParentRefs(route.Namespace, route.Spec.ParentRefs)
}

func (r *WeftGatewayReconciler) findGatewaysForTLSRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gatewayv1alpha2.TLSRoute)
	if !ok {
		return nil
	}
	return r.findGatewaysFromParentRefs(route.Namespace, route.Spec.ParentRefs)
}

func (r *WeftGatewayReconciler) findGatewaysForUDPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gatewayv1alpha2.UDPRoute)
	if !ok {
		return nil
	}
	return r.findGatewaysFromParentRefs(route.Namespace, route.Spec.ParentRefs)
}

func (r *WeftGatewayReconciler) findGatewaysFromParentRefs(routeNamespace string, parentRefs []gatewayv1.ParentReference) []reconcile.Request {
	var requests []reconcile.Request
	for _, parent := range parentRefs {
		ns := routeNamespace
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

// findGatewaysForClass returns reconcile requests for all Gateways that reference the given GatewayClass.
func (r *WeftGatewayReconciler) findGatewaysForClass(ctx context.Context, obj client.Object) []reconcile.Request {
	gwClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		return nil
	}

	// Only process if this is our controller
	if gwClass.Spec.ControllerName != ControllerName {
		return nil
	}

	// Find all Gateways that reference this GatewayClass
	var gatewayList gatewayv1.GatewayList
	if err := r.List(ctx, &gatewayList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, gw := range gatewayList.Items {
		if string(gw.Spec.GatewayClassName) == gwClass.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      gw.Name,
					Namespace: gw.Namespace,
				},
			})
		}
	}
	return requests
}

// isTCPRouteAttachedToGateway checks if a TCPRoute references the given Gateway
func (r *WeftGatewayReconciler) isTCPRouteAttachedToGateway(route *gatewayv1alpha2.TCPRoute, gateway *gatewayv1.Gateway) bool {
	for _, parent := range route.Spec.ParentRefs {
		if string(parent.Name) == gateway.Name {
			if parent.Namespace != nil && string(*parent.Namespace) != gateway.Namespace {
				continue
			}
			return true
		}
	}
	return false
}

// isTLSRouteAttachedToGateway checks if a TLSRoute references the given Gateway
func (r *WeftGatewayReconciler) isTLSRouteAttachedToGateway(route *gatewayv1alpha2.TLSRoute, gateway *gatewayv1.Gateway) bool {
	for _, parent := range route.Spec.ParentRefs {
		if string(parent.Name) == gateway.Name {
			if parent.Namespace != nil && string(*parent.Namespace) != gateway.Namespace {
				continue
			}
			return true
		}
	}
	return false
}

// isUDPRouteAttachedToGateway checks if a UDPRoute references the given Gateway
func (r *WeftGatewayReconciler) isUDPRouteAttachedToGateway(route *gatewayv1alpha2.UDPRoute, gateway *gatewayv1.Gateway) bool {
	for _, parent := range route.Spec.ParentRefs {
		if string(parent.Name) == gateway.Name {
			if parent.Namespace != nil && string(*parent.Namespace) != gateway.Namespace {
				continue
			}
			return true
		}
	}
	return false
}

// createOrUpdateTunnel creates or updates a WeftTunnel resource
func (r *WeftGatewayReconciler) createOrUpdateTunnel(
	ctx context.Context,
	tunnelName, srcURL, dstURL string,
	targetServers []string,
	gateway *gatewayv1.Gateway,
	routeName string,
	log interface{ Info(msg string, keysAndValues ...any); Error(err error, msg string, keysAndValues ...any) },
) error {
	tunnel := &weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tunnelName,
			Namespace: gateway.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tunnel, func() error {
		tunnel.Spec.TargetServers = targetServers
		tunnel.Spec.SrcURL = srcURL
		tunnel.Spec.DstURL = dstURL
		labels := map[string]string{
			"app":        "weft-gateway-tunnel",
			"gateway":    gateway.Name,
			"route":      routeName,
			"created-by": "weft-operator",
		}

		tunnel.ObjectMeta.Labels = labels

		return controllerutil.SetControllerReference(gateway, tunnel, r.Scheme)
	})
	if err != nil {
		log.Error(err, "Failed to reconcile WeftTunnel", "tunnel", tunnelName)
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.Info("WeftTunnel reconciled", "tunnel", tunnelName, "operation", op)
	}
	return nil
}

// httpFiltersToFragment translates Gateway-API RequestHeaderModifier filters
// into the URL-fragment syntax weft tunnels use to rewrite headers on
// forwarded requests (see weft README "Header Modifiers"):
//
//	  set:    name -> name=value
//	  add:    name -> name=+value     (only set if header is absent)
//	  remove: name -> name=!del
//
// Other filter types (RequestRedirect, URLRewrite, etc.) are not yet
// translated; weft has no equivalent for most of them. Returns "" when
// there are no header-modifier filters to apply.
func httpFiltersToFragment(filters []gatewayv1.HTTPRouteFilter) string {
	var parts []string
	for _, f := range filters {
		if f.Type != gatewayv1.HTTPRouteFilterRequestHeaderModifier || f.RequestHeaderModifier == nil {
			continue
		}
		rhm := f.RequestHeaderModifier
		for _, h := range rhm.Set {
			parts = append(parts, fmt.Sprintf("%s=%s", h.Name, url.QueryEscape(h.Value)))
		}
		for _, h := range rhm.Add {
			parts = append(parts, fmt.Sprintf("%s=+%s", h.Name, url.QueryEscape(h.Value)))
		}
		for _, name := range rhm.Remove {
			parts = append(parts, fmt.Sprintf("%s=!del", name))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "/#" + strings.Join(parts, "&")
}
