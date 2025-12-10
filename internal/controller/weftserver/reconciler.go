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
	"time"

	"k8s.io/client-go/kubernetes"

	"k8s.io/apimachinery/pkg/api/meta"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/resource"
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
	Clientset     kubernetes.Interface
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

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
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-certs", weftServer.Name),
			Namespace: weftServer.Namespace,
		},
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
		// The secret is stored as the username in the URL (weft://<secret>@<host>)
		connectionSecret = u.User.Username()
		bindIP = u.Hostname()
	} else {
		log.Error(err, "Failed to parse ConnectionString, using default port 8080")
	}

	// If the server is internal (auto-created), we determine the correct bind IP
	if weftServer.Spec.Location == weftv1alpha1.WeftServerLocationInternal {
		if nodeName, ok := weftServer.Labels["weft.aquaduct.dev/node"]; ok {
			var node corev1.Node
			if err := r.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err == nil {
				externalIP := ""
				for _, addr := range node.Status.Addresses {
					if addr.Type == corev1.NodeExternalIP {
						externalIP = addr.Address
						break
					}
				}
				if externalIP != "" {
					bindIP = externalIP
					log.Info("Using node ExternalIP for bindIP", "node", nodeName, "externalIP", externalIP)
				} else {
					// If no external IP found, always default to 0.0.0.0 for internal WeftServers
					bindIP = "0.0.0.0"
					log.Info("No node ExternalIP found, defaulting bindIP to 0.0.0.0", "node", nodeName)
				}
			} else {
				log.Error(err, "Failed to get Node for WeftServer, defaulting bindIP to 0.0.0.0", "node", nodeName)
				bindIP = "0.0.0.0"
			}
		} else {
			log.Info("No node label found for WeftServer, defaulting bindIP to 0.0.0.0", "weftServer", weftServer.Name)
			bindIP = "0.0.0.0"
		}
	} else if bindIP == "" {
		// For external servers, if hostname from connection string was empty, default to 0.0.0.0
		bindIP = "0.0.0.0"
	}

	// Construct command arguments
	cmdArgs := []string{"server"}
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

	// Add PVC mount path
	certsPath := "/var/lib/weft/certs"
	cmdArgs = append(cmdArgs, fmt.Sprintf("--certs-cache-path=%s", certsPath))

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

	// Reconcile PVC
	_, err = resource.Resource(resource.Options{
		Name: fmt.Sprintf("pvc/%s", pvc.Name),
		Log:  func(v ...any) { log.Info(fmt.Sprint(v...)) },
		Exists: func() bool {
			return r.Get(ctx, client.ObjectKeyFromObject(pvc), pvc) == nil
		},
		ShouldExist: func() bool {
			return weftServer.Spec.Location != weftv1alpha1.WeftServerLocationExternal
		},
		IsUpToDate: func() bool {
			// PVCs are immutable usually, but we can check size? For now, assume existing is fine.
			return true
		},
		Create: func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
				pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
				pvc.Spec.Resources = corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: k8sresource.MustParse("1Gi"),
					},
				}
				return controllerutil.SetOwnerReference(&weftServer, pvc, r.Scheme)
			})
			return err
		},
		Update: func() error {
			// Generally no-op for PVC
			return nil
		},
		Delete: func() error {
			return r.Delete(ctx, pvc)
		},
	})
	if err != nil {
		log.Error(err, "Failed to reconcile PVC")
		return r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	// Reconcile Deployment
	_, err = resource.Resource(resource.Options{
		Name: fmt.Sprintf("deployment/%s", depName),
		Log:  func(v ...any) { log.Info(fmt.Sprint(v...)) },
		Exists: func() bool {
			return r.Get(ctx, client.ObjectKeyFromObject(dep), dep) == nil
		},
		ShouldExist: func() bool {
			return weftServer.Spec.Location != weftv1alpha1.WeftServerLocationExternal
		},
		IsUpToDate: func() bool {
			return false // Always attempt to sync
		},
		Create: func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
				dep.Spec.Selector = &metav1.LabelSelector{
					MatchLabels: labels,
				}
				dep.Spec.Replicas = int32Ptr(1)
				dep.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
				dep.Spec.Template.ObjectMeta.Labels = labels
				dep.Spec.Template.Spec.HostNetwork = true
				dep.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "certs",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvc.Name,
							},
						},
					},
				}
				dep.Spec.Template.Spec.Containers = []corev1.Container{
					{
						Name:            "server",
						Image:           "ghcr.io/aquaduct-dev/weft:latest", // TODO: Versioning
						Args:            cmdArgs,
						Env:             envVars,
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "certs",
								MountPath: certsPath,
							},
						},
					},
				}
				return controllerutil.SetOwnerReference(&weftServer, dep, r.Scheme)
			})
			return err
		},
		Update: func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
				dep.Spec.Selector = &metav1.LabelSelector{
					MatchLabels: labels,
				}
				dep.Spec.Replicas = int32Ptr(1)
				dep.Spec.Template.ObjectMeta.Labels = labels
				dep.Spec.Template.Spec.HostNetwork = true
				dep.Spec.Template.Spec.Volumes = []corev1.Volume{
					{
						Name: "certs",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvc.Name,
							},
						},
					},
				}
				dep.Spec.Template.Spec.Containers = []corev1.Container{
					{
						Name:            "server",
						Image:           "ghcr.io/aquaduct-dev/weft:latest", // TODO: Versioning
						Args:            cmdArgs,
						Env:             envVars,
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: certsPath,
								Name:      "certs",
							},
						},
					},
				}
				return controllerutil.SetOwnerReference(&weftServer, dep, r.Scheme)
			})
			return err
		},
		Delete: func() error {
			return r.Delete(ctx, dep)
		},
	})
	if err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		return r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	// Reconcile Service
	_, err = resource.Resource(resource.Options{
		Name: fmt.Sprintf("service/%s", depName),
		Log:  func(v ...any) { log.Info(fmt.Sprint(v...)) },
		Exists: func() bool {
			return r.Get(ctx, client.ObjectKeyFromObject(svc), svc) == nil
		},
		ShouldExist: func() bool {
			return weftServer.Spec.Location != weftv1alpha1.WeftServerLocationExternal
		},
		IsUpToDate: func() bool {
			return false // Always attempt to sync
		},
		Create: func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
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
				return controllerutil.SetOwnerReference(&weftServer, svc, r.Scheme)
			})
			return err
		},
		Update: func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
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
				return controllerutil.SetOwnerReference(&weftServer, svc, r.Scheme)
			})
			return err
		},
		Delete: func() error {
			return r.Delete(ctx, svc)
		},
	})
	if err != nil {
		log.Error(err, "Failed to reconcile Service")
		return r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	// If external, we returned early in the old code, but here we might continue to updateStatus.
	// If external, ShouldExist was false, so Dep/Svc were deleted.
	// We should still update status.

	if weftServer.Spec.Location == weftv1alpha1.WeftServerLocationExternal {
		return r.updateWeftServerStatus(ctx, &weftServer, nil)
	}

	// Get deployment status and update WeftServer status
	// We re-fetch the deployment to get the latest status.
	// Exists logic in resource.Resource fetched it into `dep` but `Update` might have changed it?
	// `controllerutil.CreateOrUpdate` updates the object in place.
	// But if it was deleted, `dep` might be stale or empty?
	// If Location == Internal, we expect it to exist.

	if err := r.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
		log.Error(err, "Failed to get deployment after creation/update")
		return r.updateWeftServerStatus(ctx, &weftServer, err)
	}

	return r.updateWeftServerStatus(ctx, &weftServer, nil)
}

func (r *WeftServerReconciler) updateWeftServerStatus(ctx context.Context, weftServer *weftv1alpha1.WeftServer, reconcileErr error) (ctrl.Result, error) {
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
		connStr := weftServer.Spec.ConnectionString
		if weftServer.Spec.Location == weftv1alpha1.WeftServerLocationInternal {
			u, err := url.Parse(connStr)
			if err == nil {
				svcName := fmt.Sprintf("%s-server", weftServer.Name)
				var svc corev1.Service
				if err := r.Client.Get(ctx, client.ObjectKey{Name: svcName, Namespace: weftServer.Namespace}, &svc); err != nil {
					log.Error(err, "Failed to get Service for internal server", "service", svcName, "namespace", weftServer.Namespace)
					// Fallback to original connection string if service not found
				} else {
					port := u.Port()
					if port == "" {
						port = "9092"
					}
					u.Host = fmt.Sprintf("%s:%s", svc.Spec.ClusterIP, port)
					connStr = u.String()
				}
			} else {
				log.Error(err, "Failed to parse ConnectionString for internal URL reconstruction")
			}
		}

		weftClient, err := r.ClientFactory(connStr, "")
		if err != nil {
			log.Error(err, "Failed to create weft client")
			meta.SetStatusCondition(&weftServer.Status.Conditions, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "ClientError",
				Message: fmt.Sprintf("Failed to create weft client: %s", err.Error()),
			})
			if err := r.Status().Update(ctx, weftServer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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
			if err := r.Status().Update(ctx, weftServer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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

	return ctrl.Result{}, r.Status().Update(ctx, weftServer)
}

func int32Ptr(i int32) *int32 {
	return &i
}

// SetupWithManager sets up the controller with the Manager.
func (r *WeftServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(&NodeProber{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Clientset: r.Clientset}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.WeftServer{}).
		Complete(r)
}
