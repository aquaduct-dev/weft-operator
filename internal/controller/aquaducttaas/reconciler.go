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
	"k8s.io/apimachinery/pkg/api/equality"
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

	// secretFinalizerName is stamped on the access-token Secret while any
	// AquaductTaaS still references it. It keeps users from deleting the
	// secret out from under us — without the token, neither AquaductTaaS
	// nor its DNSRecords can finish their cleanup paths (suspend bastions,
	// DELETE /domain), and both end up wedged on their own finalizers.
	// Released when the last referencing AquaductTaaS is itself deleted.
	secretFinalizerName = "weft.aquaduct.dev/aquaducttaas-token-in-use"

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
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=dnsrecords,verbs=get;list;watch;delete

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

	// Pin the secret with our in-use finalizer once we've successfully
	// read it. Doing it here (post-readToken) means we never stamp a
	// finalizer on the wrong secret if spec.accessTokenSecretRef points
	// at a missing/wrong-key secret — a stuck finalizer on a stranger's
	// secret would be much harder to recover from than a SecretError.
	if err := r.ensureSecretFinalizer(ctx, &taas); err != nil {
		log.Error(err, "Failed to add in-use finalizer to access-token secret")
		return r.failure(ctx, &taas, "SecretError", err.Error())
	}

	// Snapshot the status as read so commitStatus can tell a real change
	// from a fresh heartbeat.
	before := taas.Status.DeepCopy()

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
	lastSync := taas.Status.LastSyncTime
	taas.Status.SyncedServers = synced
	taas.Status.Bastions = bastions
	meta.SetStatusCondition(&taas.Status.Conditions, metav1.Condition{
		Type:    conditionAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "Synced",
		Message: fmt.Sprintf("Synced %d external server(s) from aquaduct.dev", len(synced)),
	})
	if err := r.commitStatus(ctx, &taas, before, lastSync); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// commitStatus writes the status subresource only when something other than
// the heartbeat changed, or when the heartbeat itself has gone stale.
//
// LastSyncTime used to be stamped with metav1.Now() and written on every
// pass. Since that always produced a new resourceVersion, the write tripped
// this controller's own For() watch and enqueued the next reconcile
// immediately — a self-sustaining loop that re-hit GET /api/bastion on
// aquaduct.dev every iteration and raced itself into "the object has been
// modified" conflicts. RequeueAfter(resyncInterval) never got a chance to be
// the pacer it was written to be.
//
// before is the status as read at the top of the pass; lastSync is the
// heartbeat carried on it, compared separately so a fresh timestamp alone is
// not treated as a change.
func (r *AquaductTaaSReconciler) commitStatus(
	ctx context.Context,
	taas *weftv1alpha1.AquaductTaaS,
	before *weftv1alpha1.AquaductTaaSStatus,
	lastSync *metav1.Time,
) error {
	prev := before.DeepCopy()
	prev.LastSyncTime = nil
	next := taas.Status.DeepCopy()
	next.LastSyncTime = nil

	stale := lastSync == nil || time.Since(lastSync.Time) >= resyncInterval
	if equality.Semantic.DeepEqual(prev, next) && !stale {
		// Nothing moved and the heartbeat is still fresh — leave the object
		// alone so we don't wake ourselves up again.
		taas.Status.LastSyncTime = lastSync
		return nil
	}

	now := metav1.Now()
	taas.Status.LastSyncTime = &now
	return r.Status().Update(ctx, taas)
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
		// Suspended or no-IP bastions have no usable address; mirroring
		// them would write a connection string like "weft://<sec>@:9092"
		// that the WeftServer reconciler and tunnel pods can't dial. The
		// bastion still surfaces on AquaductTaaS.status.bastions for
		// downstream reconcilers (DNSRecord already filters the same way).
		if s.Suspended || s.IP == "" {
			continue
		}
		if _, dup := desired[s.Name]; dup {
			return nil, fmt.Errorf("aquaduct.dev returned duplicate server name %q", s.Name)
		}
		desired[s.Name] = s
	}

	for _, s := range servers {
		if _, ok := desired[s.Name]; !ok {
			continue
		}
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

// handleDeletion runs cleanup in a strict order:
//
//  1. Cascade-delete DNSRecords that reference this TaaS. They have their
//     own finalizers that need our token to call DELETE /domain, so we
//     stay around (with our finalizer attached, plus the secret finalizer
//     pinning the token) until they're all gone.
//  2. Suspend every managed bastion on aquaduct.dev so cloud resources
//     stop billing.
//  3. Release the secret's in-use finalizer (only if no other AquaductTaaS
//     still references it) so the user's secret can be deleted.
//  4. Drop our own finalizer.
//
// Idempotent: any step that fails leaves the AquaductTaaS finalizer attached
// and surfaces an Available=False condition so the next reconcile retries.
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

	// Step 1: cascade DNSRecords. Each one has a unregister-on-delete
	// finalizer that calls DELETE /domain — and that call needs to read
	// our spec/secret. Because we still hold our own finalizer, the
	// AquaductTaaS object remains queryable until the records drain.
	ownedDR, err := r.listOwnedDNSRecords(ctx, taas)
	if err != nil {
		return r.failure(ctx, taas, "SyncError", fmt.Sprintf("list owned DNSRecords: %s", err))
	}
	if len(ownedDR) > 0 {
		for i := range ownedDR {
			dr := &ownedDR[i]
			if !dr.DeletionTimestamp.IsZero() {
				continue
			}
			log.Info("Cascade-deleting DNSRecord owned by AquaductTaaS", "dnsrecord", dr.Name)
			if err := r.Delete(ctx, dr); err != nil && !errors.IsNotFound(err) {
				return r.failure(ctx, taas, "SyncError",
					fmt.Sprintf("delete DNSRecord %q: %s", dr.Name, err))
			}
		}
		// At least one record is still draining — wait for its finalizer
		// to complete before we suspend bastions or release the secret.
		return ctrl.Result{RequeueAfter: errorRetry}, nil
	}

	// Step 2: suspend bastions.
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

	// Step 3: release the secret finalizer (if no other AquaductTaaS still
	// uses it). Done before removing our own finalizer so a crash between
	// the two updates doesn't leave the user's secret pinned forever.
	if err := r.releaseSecretFinalizer(ctx, taas); err != nil {
		log.Error(err, "Failed to release secret finalizer during deletion")
		return r.failure(ctx, taas, "SecretError", fmt.Sprintf("release secret finalizer: %s", err))
	}

	// Step 4: drop our own finalizer.
	controllerutil.RemoveFinalizer(taas, finalizerName)
	if err := r.Update(ctx, taas); err != nil {
		return ctrl.Result{}, err
	}
	// Owned WeftServers are GC'd automatically via owner references once the
	// apiserver finishes deleting this object.
	return ctrl.Result{}, nil
}

// ensureSecretFinalizer stamps secretFinalizerName on the referenced
// access-token Secret if it isn't already there. Idempotent and safe to
// call on every reconcile. Caller must have just successfully readToken'd
// the secret so we know it exists and has the right key.
func (r *AquaductTaaSReconciler) ensureSecretFinalizer(ctx context.Context, taas *weftv1alpha1.AquaductTaaS) error {
	log := log.FromContext(ctx)
	if taas.Spec.AccessTokenSecretRef == nil {
		return nil
	}
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: taas.Namespace, Name: taas.Spec.AccessTokenSecretRef.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		return err
	}
	if controllerutil.ContainsFinalizer(&secret, secretFinalizerName) {
		return nil
	}
	controllerutil.AddFinalizer(&secret, secretFinalizerName)
	if err := r.Update(ctx, &secret); err != nil {
		return err
	}
	log.Info("Added in-use finalizer to access-token secret", "secret", key.String())
	return nil
}

// releaseSecretFinalizer drops secretFinalizerName from the referenced
// secret iff no other AquaductTaaS in the same namespace still references
// the same secret name. Multi-TaaS-per-namespace isn't the current
// pattern (singleton TaaS via parametersRef is the design intent), but
// the count-and-release model means a future shift to multiple TaaSes
// won't strand the secret as soon as one of them is deleted.
func (r *AquaductTaaSReconciler) releaseSecretFinalizer(ctx context.Context, taas *weftv1alpha1.AquaductTaaS) error {
	log := log.FromContext(ctx)
	if taas.Spec.AccessTokenSecretRef == nil {
		return nil
	}
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: taas.Namespace, Name: taas.Spec.AccessTokenSecretRef.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		if errors.IsNotFound(err) {
			// Secret is gone (somehow — finalizer should have prevented
			// this, but maybe it was patched off, or the AquaductTaaS
			// was created without us ever stamping one). Nothing to do.
			return nil
		}
		return err
	}
	if !controllerutil.ContainsFinalizer(&secret, secretFinalizerName) {
		return nil
	}

	var taases weftv1alpha1.AquaductTaaSList
	if err := r.List(ctx, &taases, client.InNamespace(taas.Namespace)); err != nil {
		return fmt.Errorf("list AquaductTaaS to count secret references: %w", err)
	}
	for i := range taases.Items {
		other := &taases.Items[i]
		if other.UID == taas.UID {
			continue
		}
		if !other.DeletionTimestamp.IsZero() {
			// Also being deleted — its own handleDeletion will run the
			// release pass too. Don't pin the finalizer on its account.
			continue
		}
		if other.Spec.AccessTokenSecretRef == nil {
			continue
		}
		if other.Spec.AccessTokenSecretRef.Name == secret.Name {
			log.Info("Leaving secret finalizer in place; still referenced by another AquaductTaaS",
				"secret", key.String(), "by", other.Name)
			return nil
		}
	}

	controllerutil.RemoveFinalizer(&secret, secretFinalizerName)
	if err := r.Update(ctx, &secret); err != nil {
		return err
	}
	log.Info("Released in-use finalizer on access-token secret", "secret", key.String())
	return nil
}

// listOwnedDNSRecords returns every DNSRecord in the AquaductTaaS's
// namespace whose spec.aquaductTaaSRef.name points at this TaaS. We
// can't use ownerRefs because DNSRecords are stamped by other
// reconcilers (Gateway) and we don't own them — the spec reference
// is the source of truth for "this DNSRecord depends on this TaaS".
func (r *AquaductTaaSReconciler) listOwnedDNSRecords(ctx context.Context, taas *weftv1alpha1.AquaductTaaS) ([]weftv1alpha1.DNSRecord, error) {
	var all weftv1alpha1.DNSRecordList
	if err := r.List(ctx, &all, client.InNamespace(taas.Namespace)); err != nil {
		return nil, err
	}
	owned := make([]weftv1alpha1.DNSRecord, 0, len(all.Items))
	for i := range all.Items {
		if all.Items[i].Spec.AquaductTaaSRef.Name == taas.Name {
			owned = append(owned, all.Items[i])
		}
	}
	return owned, nil
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
