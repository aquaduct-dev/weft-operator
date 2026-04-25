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

package aquaducttaas

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

const (
	// ownerLabel is stamped on every WeftServer this reconciler creates so we
	// can list them back and prune ones that aquaduct.dev no longer returns.
	ownerLabel = "weft.aquaduct.dev/aquaducttaas"

	// bastionIDAnnotation stores the opaque aquaduct.dev bastion ID on each
	// mirrored WeftServer. Needed because the WeftServer is named by the
	// bastion's human-friendly name, but the suspend API uses the ID.
	bastionIDAnnotation = "weft.aquaduct.dev/bastion-id"

	// finalizerName blocks deletion of an AquaductTaaS until we've called
	// SuspendServer for every bastion we manage. Without it, deleting the CR
	// would leave cloud bastions running (and billed) on aquaduct.dev.
	finalizerName = "weft.aquaduct.dev/suspend-on-delete"

	// resyncInterval is how often we poll aquaduct.dev on a successful sync.
	// Errors requeue sooner.
	resyncInterval = 5 * time.Minute
	errorRetry     = 30 * time.Second

	conditionAvailable = "Available"
)

// AquaductTaaSReconciler reconciles a AquaductTaaS object
type AquaductTaaSReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIClient talks to api.aquaduct.dev. Tests inject a fake; main.go wires
	// an HTTPAPIClient. Zero value is treated as a configuration error.
	APIClient APIClient
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=aquaducttaases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=aquaducttaases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=aquaducttaases/finalizers,verbs=update
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile pulls the list of cloud-hosted bastions from aquaduct.dev and
// mirrors them as External WeftServers owned by this AquaductTaaS object.
func (r *AquaductTaaSReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var taas weftv1alpha1.AquaductTaaS
	if err := r.Get(ctx, req.NamespacedName, &taas); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if r.APIClient == nil {
		return r.failure(ctx, &taas, "ConfigError", "AquaductTaaSReconciler has no APIClient configured")
	}

	if !taas.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &taas)
	}

	if !controllerutil.ContainsFinalizer(&taas, finalizerName) {
		controllerutil.AddFinalizer(&taas, finalizerName)
		if err := r.Update(ctx, &taas); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if taas.Spec.AccessTokenSecretRef == nil {
		return r.failure(ctx, &taas, "SpecInvalid", "spec.accessTokenSecretRef is required")
	}

	token, err := r.readToken(ctx, &taas)
	if err != nil {
		log.Error(err, "Failed to read access token")
		return r.failure(ctx, &taas, "SecretError", err.Error())
	}

	servers, err := r.APIClient.ListExternalServers(ctx, token)
	if err != nil {
		log.Error(err, "Failed to list servers from aquaduct.dev")
		return r.failure(ctx, &taas, "APIError", err.Error())
	}

	synced, err := r.syncWeftServers(ctx, &taas, servers)
	if err != nil {
		log.Error(err, "Failed to sync WeftServers")
		return r.failure(ctx, &taas, "SyncError", err.Error())
	}

	sort.Strings(synced)
	// Publish the full bastion list on status so downstream reconcilers
	// (DNSRecord) can compute fan-out / expected-IP decisions without
	// re-listing /api/bastion themselves.
	bastions := make([]weftv1alpha1.BastionInfo, 0, len(servers))
	for _, s := range servers {
		bastions = append(bastions, weftv1alpha1.BastionInfo{
			ID:        s.ID,
			Name:      s.Name,
			IP:        s.IP,
			Suspended: s.Suspended,
		})
	}
	sort.Slice(bastions, func(i, j int) bool { return bastions[i].ID < bastions[j].ID })
	now := metav1.Now()
	taas.Status.LastSyncTime = &now
	taas.Status.SyncedServers = synced
	taas.Status.Bastions = bastions
	meta.SetStatusCondition(&taas.Status.Conditions, metav1.Condition{
		Type:    conditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "Synced",
		Message: fmt.Sprintf("Synced %d external server(s) from aquaduct.dev", len(synced)),
	})
	if err := r.Status().Update(ctx, &taas); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// readToken resolves the access token through the referenced Secret. An empty
// token is treated as an error so a typo in the key doesn't silently succeed.
func (r *AquaductTaaSReconciler) readToken(ctx context.Context, taas *weftv1alpha1.AquaductTaaS) (string, error) {
	ref := taas.Spec.AccessTokenSecretRef
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: taas.Namespace, Name: ref.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("secret %q not found in namespace %q", ref.Name, taas.Namespace)
		}
		return "", err
	}
	data, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %q has no key %q", ref.Name, ref.Key)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("secret %q key %q is empty", ref.Name, ref.Key)
	}
	return string(data), nil
}

// syncWeftServers creates or updates a WeftServer per returned ExternalServer
// and deletes any previously-synced WeftServer that's no longer returned.
// Returns the sorted list of server names now under management.
func (r *AquaductTaaSReconciler) syncWeftServers(ctx context.Context, taas *weftv1alpha1.AquaductTaaS, servers []ExternalServer) ([]string, error) {
	log := log.FromContext(ctx)
	desired := make(map[string]ExternalServer, len(servers))
	for _, s := range servers {
		if s.Name == "" {
			return nil, fmt.Errorf("aquaduct.dev returned a server with empty name")
		}
		if s.ID == "" {
			return nil, fmt.Errorf("aquaduct.dev returned server %q with empty id", s.Name)
		}
		if _, dup := desired[s.Name]; dup {
			return nil, fmt.Errorf("aquaduct.dev returned duplicate server name %q", s.Name)
		}
		desired[s.Name] = s
	}

	for _, s := range servers {
		ws := &weftv1alpha1.WeftServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.Name,
				Namespace: taas.Namespace,
			},
		}
		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, ws, func() error {
			if ws.Labels == nil {
				ws.Labels = map[string]string{}
			}
			ws.Labels[ownerLabel] = taas.Name
			if ws.Annotations == nil {
				ws.Annotations = map[string]string{}
			}
			ws.Annotations[bastionIDAnnotation] = s.ID
			ws.Spec.Location = weftv1alpha1.WeftServerLocationExternal
			ws.Spec.ConnectionString = s.ConnectionString
			return controllerutil.SetControllerReference(taas, ws, r.Scheme)
		})
		if err != nil {
			return nil, fmt.Errorf("upsert WeftServer %q: %w", s.Name, err)
		}
		if op != controllerutil.OperationResultNone {
			log.Info("Synced external WeftServer", "name", s.Name, "id", s.ID, "operation", op)
		}
	}

	var existing weftv1alpha1.WeftServerList
	if err := r.List(ctx, &existing,
		client.InNamespace(taas.Namespace),
		client.MatchingLabels{ownerLabel: taas.Name},
	); err != nil {
		return nil, fmt.Errorf("list existing WeftServers: %w", err)
	}
	for i := range existing.Items {
		ws := &existing.Items[i]
		if _, keep := desired[ws.Name]; keep {
			continue
		}
		log.Info("Pruning stale external WeftServer", "name", ws.Name)
		if err := r.Delete(ctx, ws); err != nil && !errors.IsNotFound(err) {
			return nil, fmt.Errorf("delete stale WeftServer %q: %w", ws.Name, err)
		}
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	return names, nil
}

// handleDeletion suspends every bastion this AquaductTaaS manages, then drops
// the finalizer so k8s can finish deletion. It is idempotent: if SuspendServer
// for some server fails, the finalizer stays and the next reconcile retries
// the whole list (SuspendServer must itself be idempotent — see APIClient
// contract).
func (r *AquaductTaaSReconciler) handleDeletion(ctx context.Context, taas *weftv1alpha1.AquaductTaaS) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(taas, finalizerName) {
		// Either we never got to add the finalizer (deletion raced with
		// create) or we've already cleaned up. Either way nothing to do.
		return ctrl.Result{}, nil
	}

	// Read the token first. Without it we can't call the API at all; block
	// deletion via an Available=False condition so the user notices rather
	// than silently leaving bastions running.
	if taas.Spec.AccessTokenSecretRef == nil {
		return r.failure(ctx, taas, "SpecInvalid", "cannot suspend bastions: spec.accessTokenSecretRef is required")
	}
	token, err := r.readToken(ctx, taas)
	if err != nil {
		log.Error(err, "Failed to read access token during deletion")
		return r.failure(ctx, taas, "SecretError", err.Error())
	}

	var owned weftv1alpha1.WeftServerList
	if err := r.List(ctx, &owned,
		client.InNamespace(taas.Namespace),
		client.MatchingLabels{ownerLabel: taas.Name},
	); err != nil {
		return r.failure(ctx, taas, "SyncError", fmt.Sprintf("list owned WeftServers: %s", err))
	}

	for i := range owned.Items {
		ws := &owned.Items[i]
		id := ws.Annotations[bastionIDAnnotation]
		if id == "" {
			// Mirrored WeftServers always carry this annotation. Missing it
			// means either a hand-crafted object slipped into our label
			// namespace or the annotation was stripped — either way we can't
			// identify the bastion to suspend, so surface the error.
			return r.failure(ctx, taas, "SuspendError",
				fmt.Sprintf("WeftServer %q is missing the %s annotation; cannot suspend", ws.Name, bastionIDAnnotation))
		}
		if err := r.APIClient.SuspendServer(ctx, token, id); err != nil {
			log.Error(err, "Failed to suspend bastion", "name", ws.Name, "id", id)
			return r.failure(ctx, taas, "SuspendError", fmt.Sprintf("suspend %q (id=%s): %s", ws.Name, id, err))
		}
		log.Info("Suspended bastion on aquaduct.dev", "name", ws.Name, "id", id)
	}

	controllerutil.RemoveFinalizer(taas, finalizerName)
	if err := r.Update(ctx, taas); err != nil {
		return ctrl.Result{}, err
	}
	// Owned WeftServers are GC'd automatically via owner references once the
	// apiserver finishes deleting this object.
	return ctrl.Result{}, nil
}

// failure records an Available=False condition with the given reason/message
// and schedules a short requeue. It never returns the reconcile error itself
// because the condition is the source of truth for the user.
func (r *AquaductTaaSReconciler) failure(ctx context.Context, taas *weftv1alpha1.AquaductTaaS, reason, message string) (ctrl.Result, error) {
	meta.SetStatusCondition(&taas.Status.Conditions, metav1.Condition{
		Type:    conditionAvailable,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	if err := r.Status().Update(ctx, taas); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: errorRetry}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AquaductTaaSReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.AquaductTaaS{}).
		Owns(&weftv1alpha1.WeftServer{}).
		Complete(r)
}
