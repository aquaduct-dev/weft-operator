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

package wefttunnel

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
)

// WeftTunnelReconciler reconciles a WeftTunnel object
type WeftTunnelReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels/finalizers,verbs=update
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *WeftTunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var weftTunnel weftv1alpha1.WeftTunnel
	if err := r.Get(ctx, req.NamespacedName, &weftTunnel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Ensure GVK is set for SetOwnerReference
	if weftTunnel.GetObjectKind().GroupVersionKind().Empty() {
		weftTunnel.SetGroupVersionKind(weftv1alpha1.GroupVersion.WithKind("WeftTunnel"))
	}

	var reconcileErr error
	defer func() {
		if err := r.updateStatus(ctx, &weftTunnel, reconcileErr); err != nil {
			log.Error(err, "Failed to update WeftTunnel status")
		}
	}()

	// Identify target WeftServers
	var targetServers []weftv1alpha1.WeftServer
	if len(weftTunnel.Spec.TargetServers) == 0 {
		// Connect to all servers in the namespace
		var serverList weftv1alpha1.WeftServerList
		if err := r.List(ctx, &serverList, client.InNamespace(req.Namespace)); err != nil {
			log.Error(err, "Failed to list WeftServers")
			reconcileErr = err
			return ctrl.Result{}, err
		}
		targetServers = append(targetServers, serverList.Items...)

		// Also connect to servers in weft-system
		if req.Namespace != "weft-system" {
			var sysServerList weftv1alpha1.WeftServerList
			if err := r.List(ctx, &sysServerList, client.InNamespace("weft-system")); err != nil {
				log.Error(err, "Failed to list system WeftServers")
				reconcileErr = err
				return ctrl.Result{}, err
			}
			targetServers = append(targetServers, sysServerList.Items...)
		}
	} else {
		// Connect to specified servers
		for _, name := range weftTunnel.Spec.TargetServers {
			var srv weftv1alpha1.WeftServer
			if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: req.Namespace}, &srv); err != nil {
				if errors.IsNotFound(err) {
					log.Info("Target WeftServer not found, skipping", "name", name)
					continue
				}
				log.Error(err, "Failed to get target WeftServer", "name", name)
				reconcileErr = err
				return ctrl.Result{}, err
			}
			targetServers = append(targetServers, srv)
		}
	}

	// Track expected deployments to prune others later
	expectedDeployments := make(map[string]bool)

	for _, srv := range targetServers {
		targetURL, err := r.constructTargetURL(&srv)
		if err != nil {
			log.Error(err, "Failed to construct target URL", "server", srv.Name)
			continue
		}

		// Deployment Name: tunnel-<TunnelName>-to-<ServerName>
		depName := fmt.Sprintf("tunnel-%s-to-%s", weftTunnel.Name, srv.Name)
		expectedDeployments[depName] = true

		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      depName,
				Namespace: weftTunnel.Namespace,
			},
		}

		labels := map[string]string{
			"app":        "weft-tunnel",
			"tunnel":     weftTunnel.Name,
			"target":     srv.Name,
			"created-by": "weft-operator",
		}

		// Command: weft tunnel --tunnel-name=<name> <targetURL> <srcURL> <dstURL>
		cmdArgs := []string{
			"tunnel",
			fmt.Sprintf("--tunnel-name=%s", weftTunnel.Name),
			targetURL,
			weftTunnel.Spec.SrcURL,
			weftTunnel.Spec.DstURL,
		}

		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
			dep.ObjectMeta.Labels = labels
			dep.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: labels,
			}
			dep.Spec.Replicas = int32Ptr(1)
			dep.Spec.Template.ObjectMeta.Labels = labels
			dep.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
			dep.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:            "tunnel",
					Image:           "ghcr.io/aquaduct-dev/weft:latest", // TODO: Versioning
					Args:            cmdArgs,
					ImagePullPolicy: corev1.PullAlways,
				},
			}
			err := controllerutil.SetOwnerReference(&weftTunnel, dep, r.Scheme)
			if err != nil {
				log.Error(err, "SetOwnerReference failed")
				return err
			}
			return nil
		})
		if err != nil {
			log.Error(err, "Failed to reconcile Tunnel Deployment", "deployment", depName)
			reconcileErr = err
			return ctrl.Result{}, err
		}

		if op != controllerutil.OperationResultNone {
			log.Info("Tunnel Deployment reconciled", "deployment", depName, "operation", op)
		}
	}

	// Prune deployments that are no longer needed
	// List all deployments owned by this WeftTunnel
	var depList appsv1.DeploymentList
	if err := r.List(ctx, &depList, client.InNamespace(req.Namespace), client.MatchingLabels{"tunnel": weftTunnel.Name, "created-by": "weft-operator"}); err != nil {
		log.Error(err, "Failed to list existing deployments")
		reconcileErr = err
		return ctrl.Result{}, err
	}

	for _, d := range depList.Items {
		if !expectedDeployments[d.Name] {
			log.Info("Deleting obsolete tunnel deployment", "deployment", d.Name)
			if err := r.Delete(ctx, &d); err != nil {
				log.Error(err, "Failed to delete obsolete deployment", "deployment", d.Name)
				reconcileErr = err
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *WeftTunnelReconciler) updateStatus(ctx context.Context, weftTunnel *weftv1alpha1.WeftTunnel, reconcileErr error) error {
	// Re-calculate readiness based on deployments
	// Note: This is a bit redundant with the check in the loop, but safer because
	// the loop logic above depends on the CreateOrUpdate return which might not be fully up-to-date regarding Status
	// if the object was just created.
	// Actually, relying on the loop variable `allReady` passed via closure/argument is hard because updateStatus is deferred.
	// We need to re-check the deployments here or accept that `defer` captures arguments at call time? No, defer executes function at end.
	// But we can't easily pass `allReady` to the deferred function unless we wrap it.

	// Let's just list the deployments again to be sure.
	var depList appsv1.DeploymentList
	if err := r.List(ctx, &depList, client.InNamespace(weftTunnel.Namespace), client.MatchingLabels{"tunnel": weftTunnel.Name, "created-by": "weft-operator"}); err != nil {
		return err
	}

	allReady := true
	if len(depList.Items) == 0 {
		// No deployments yet?
		allReady = false
	}
	for _, d := range depList.Items {
		if d.Status.AvailableReplicas == 0 {
			allReady = false
			break
		}
	}

	if reconcileErr != nil {
		meta.SetStatusCondition(&weftTunnel.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "ReconcileError",
			Message: reconcileErr.Error(),
		})
	} else if !allReady {
		meta.SetStatusCondition(&weftTunnel.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingForDeployments",
			Message: "One or more tunnel deployments are not yet available",
		})
	} else {
		meta.SetStatusCondition(&weftTunnel.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciled",
			Message: "WeftTunnel reconciled successfully and all tunnels are up",
		})
	}
	return r.Status().Update(ctx, weftTunnel)
}

func (r *WeftTunnelReconciler) constructTargetURL(srv *weftv1alpha1.WeftServer) (string, error) {
	if srv.Spec.Location == weftv1alpha1.WeftServerLocationInternal {
		// For internal WeftServers, the tunnel should connect to the Kubernetes Service
		// created for the WeftServer, not the IP specified in the connection string.
		u, err := url.Parse(srv.Spec.ConnectionString)
		if err != nil {
			return "", fmt.Errorf("failed to parse connection string for internal server %s: %w", srv.Name, err)
		}

		serviceName := fmt.Sprintf("%s-server", srv.Name)
		serviceHost := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, srv.Namespace)

		// Save the port before modifying u.Host
		port := u.Port()
		if port == "" {
			// Default port if not specified in the original connection string
			port = "9092" // Assuming 9092 is the default for weftserver
		}

		// Reconstruct the URL with the service host and the original/defaulted port
		u.Host = fmt.Sprintf("%s:%s", serviceHost, port)
		return u.String(), nil
	}
	// For external WeftServers, use the connection string as is
	return srv.Spec.ConnectionString, nil
}

func int32Ptr(i int32) *int32 {
	return &i
}

// SetupWithManager sets up the controller with the Manager.
func (r *WeftTunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.WeftTunnel{}).
		Owns(&appsv1.Deployment{}).
		Watches(
			&weftv1alpha1.WeftServer{},
			handler.EnqueueRequestsFromMapFunc(r.findTunnelsForServer),
		).
		Complete(r)
}

// findTunnelsForServer returns a list of requests for WeftTunnels that target the given WeftServer
func (r *WeftTunnelReconciler) findTunnelsForServer(ctx context.Context, obj client.Object) []reconcile.Request {
	server, ok := obj.(*weftv1alpha1.WeftServer)
	if !ok {
		return nil
	}

	var tunnelList weftv1alpha1.WeftTunnelList
	if err := r.List(ctx, &tunnelList, client.InNamespace(server.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, tunnel := range tunnelList.Items {
		match := len(tunnel.Spec.TargetServers) == 0 || slices.Contains(tunnel.Spec.TargetServers, server.Name)

		if match {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tunnel.Name,
					Namespace: tunnel.Namespace,
				},
			})
		}
	}
	return requests
}
