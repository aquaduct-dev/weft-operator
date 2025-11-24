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

package weftserver

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	weftclient "github.com/aquaduct-dev/weft/src/client"
)

// WeftClient defines the interface for interacting with the weft server
type WeftClient interface {
	ListTunnels() (map[string]weftclient.TunnelInfo, error)
}

// WeftClientFactory defines a function to create a WeftClient
type WeftClientFactory func(url string, tunnelName string) (WeftClient, error)

// defaultClientFactory is the default implementation using the actual weftclient
func defaultClientFactory(urlStr string, tunnelName string) (WeftClient, error) {
	return weftclient.NewClient(urlStr, tunnelName)
}

// WeftServerReconciler reconciles a WeftServer object
type WeftServerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientFactory WeftClientFactory
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *WeftServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Initialize ClientFactory if not set
	if r.ClientFactory == nil {
		r.ClientFactory = defaultClientFactory
	}

	var weftServer weftv1alpha1.WeftServer
	if err := r.Get(ctx, req.NamespacedName, &weftServer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	depName := weftServer.Name + "-server"
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: weftServer.Namespace,
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: weftServer.Namespace,
		},
	}

	if weftServer.Spec.Location == weftv1alpha1.WeftServerLocationExternal {
		// Delete deployment if it exists
		if err := r.Delete(ctx, dep); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "Failed to delete external WeftServer deployment")
			return ctrl.Result{}, err
		}
		// Delete service if it exists
		if err := r.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "Failed to delete external WeftServer service")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.updateWeftServerStatus(ctx, &weftServer, nil)
	}

	// Internal: Create or Update Deployment and Service
	labels := map[string]string{
		"app":      "weft-server",
		"instance": weftServer.Name,
	}

	// Parse port from ConnectionString
	port := int32(8080) // Default
	var connectionSecret, bindIP string

	u, err := url.Parse(weftServer.Spec.ConnectionString)
	if err == nil {
		p := u.Port()
		if p != "" {
			if pInt, err := strconv.Atoi(p); err == nil {
				port = int32(pInt)
			}
		}
		if pwd, ok := u.User.Password(); ok {
			connectionSecret = pwd
		}
		bindIP = u.Hostname()
	} else {
		log.Error(err, "Failed to parse ConnectionString, using default port 8080")
	}

	// Construct command arguments
	cmdArgs := []string{"weft", "server"}
	if connectionSecret != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--connection-secret=%s", connectionSecret))
	}
	if bindIP != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--bind-ip=%s", bindIP))
	}
	if weftServer.Spec.BindInterface != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--bind-interface=%s", weftServer.Spec.BindInterface))
	}
	if weftServer.Spec.UsageReportingURL != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--usage-reporting-url=%s", weftServer.Spec.UsageReportingURL))
	}

	var envVars []corev1.EnvVar
	if weftServer.Spec.CloudflareTokenSecretRef != nil {
		cmdArgs = append(cmdArgs, "--cloudflare-token=$(CLOUDFLARE_TOKEN)")
		envVars = append(envVars, corev1.EnvVar{
			Name: "CLOUDFLARE_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: weftServer.Spec.CloudflareTokenSecretRef,
			},
		})
	}

	// Reconcile Deployment
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		dep.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: labels,
		}
		dep.Spec.Replicas = int32Ptr(1)
		dep.Spec.Template.ObjectMeta.Labels = labels
		dep.Spec.Template.Spec.HostNetwork = true
		dep.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:    "server",
				Image:   "ghcr.io/aquaduct-dev/weft:latest", // TODO: Versioning
				Command: cmdArgs,
				Env:     envVars,
			},
		}
		return controllerutil.SetControllerReference(&weftServer, dep, r.Scheme)
	})

	if err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	if op != controllerutil.OperationResultNone {
		log.Info("Deployment reconciled", "operation", op)
	}

	// Reconcile Service
	opSvc, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.Selector = labels
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "server",
				Port:       port,
				TargetPort: intstr.FromInt(int(port)),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		return controllerutil.SetControllerReference(&weftServer, svc, r.Scheme)
	})

	if err != nil {
		log.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	if opSvc != controllerutil.OperationResultNone {
		log.Info("Service reconciled", "operation", opSvc)
	}

	// Get deployment status and update WeftServer status
	err = r.Get(ctx, client.ObjectKeyFromObject(dep), dep) // Get updated deployment
	if err != nil {
		log.Error(err, "Failed to get deployment after creation/update")
		return ctrl.Result{}, r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	return ctrl.Result{}, r.updateWeftServerStatus(ctx, &weftServer, nil)
}

func (r *WeftServerReconciler) updateWeftServerStatus(ctx context.Context, weftServer *weftv1alpha1.WeftServer, reconcileErr error) error {
	log := log.FromContext(ctx)

	// Set initial status conditions
	if reconcileErr != nil {
		meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "ReconcilingError",
			Message: reconcileErr.Error(),
		})
	} else {
		// Create weftclient and list tunnels
		weftClient, err := r.ClientFactory(weftServer.Spec.ConnectionString, "")
		if err != nil {
			log.Error(err, "Failed to create weft client")
			meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientError",
				Message: fmt.Sprintf("Failed to create weft client: %s", err.Error()),
			})
			return r.Status().Update(ctx, weftServer)
		}

		tunnels, err := weftClient.ListTunnels()
		if err != nil {
			// Check if it's a connection error (server might be starting up)
			if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
				log.Info("Failed to connect to weft server (server might be starting)", "error", err)
				meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  "ConnectionError",
					Message: fmt.Sprintf("Failed to connect: %s", err.Error()),
				})
			} else {
				log.Error(err, "Failed to list tunnels from weft server")
				meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  "TunnelListError",
					Message: fmt.Sprintf("Failed to list tunnels: %s", err.Error()),
				})
			}
			return r.Status().Update(ctx, weftServer)
		}

		// Convert weftclient.TunnelInfo to weftv1alpha1.TunnelStatus
		weftServer.Status.Tunnels = make([]weftv1alpha1.TunnelStatus, 0, len(tunnels))
		for _, tunnelInfo := range tunnels {
			weftServer.Status.Tunnels = append(weftServer.Status.Tunnels, weftv1alpha1.TunnelStatus{
				Tx:     tunnelInfo.Tx,
				Rx:     tunnelInfo.Rx,
				SrcURL: tunnelInfo.SrcURL,
				DstURL: tunnelInfo.DstURL,
			})
		}

		meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciled",
			Message: "WeftServer reconciled successfully",
		})
	}

	return r.Status().Update(ctx, weftServer)
}

func int32Ptr(i int32) *int32 {
	return &i
}

// SetupWithManager sets up the controller with the Manager.
func (r *WeftServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(&NodeProber{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.WeftServer{}).
		Complete(r)
}

