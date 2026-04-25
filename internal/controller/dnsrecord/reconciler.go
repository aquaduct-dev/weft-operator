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

package dnsrecord

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
)

const (
	// finalizerName keeps the DNSRecord around until DeleteDomain
	// succeeds. The operator is allowed to clobber pre-existing
	// registrations, but must always clean up — so the finalizer runs
	// regardless of whether we created the record or inherited it.
	finalizerName = "weft.aquaduct.dev/unregister-on-delete"

	// resyncInterval / errorRetry match the AquaductTaaS reconciler. Any
	// faster and we'd hammer aquaduct.dev during transient failures; any
	// slower and legitimate server-side changes take too long to surface.
	resyncInterval = 5 * time.Minute
	errorRetry     = 30 * time.Second

	// Condition types. Ready is the top-level health signal; the other
	// two are diagnostic breakdowns for operators debugging why Ready is
	// false. (A record can be Registered=True but Resolved=False when
	// DNS propagation hasn't caught up yet — still mostly healthy.)
	conditionReady      = "Ready"
	conditionRegistered = "Registered"
	conditionResolved   = "Resolved"
)

// DNSRecordReconciler drives a DNSRecord toward the desired registration
// state on aquaduct.dev. It depends on an AquaductTaaS in the same
// namespace for API credentials + endpoint — that indirection is what
// makes a DNSRecord portable between clusters (credentials follow the
// AquaductTaaS, not the DNSRecord).
type DNSRecordReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIClient talks to the /domain endpoints on aquaduct.dev. Tests
	// inject a fake; main.go wires an HTTPAPIClient. A nil APIClient is
	// surfaced as a ConfigError condition rather than a panic — keeps
	// the operator self-diagnosing in broken deployments.
	APIClient aquaducttaas.DomainAPIClient
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=dnsrecords/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=dnsrecords/finalizers,verbs=update
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=aquaducttaases,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is the main entry point. It's structured as a sequence of
// stages: deletion → finalizer bootstrap → resolve deps → register →
// resolve DNS → status write. Every stage short-circuits via failure()
// on a recoverable error so the next requeue picks up where we left off.
func (r *DNSRecordReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var dr weftv1alpha1.DNSRecord
	if err := r.Get(ctx, req.NamespacedName, &dr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if r.APIClient == nil {
		// Unrecoverable from the reconciler's perspective, but we still
		// want the user to see *why* nothing is happening — set a clear
		// condition and requeue on the error cadence.
		logger.Error(nil, "DNSRecordReconciler has no APIClient configured")
		return r.failure(ctx, &dr, conditionRegistered, "ConfigError",
			"DNSRecordReconciler has no APIClient configured; check operator deployment")
	}

	if !dr.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &dr)
	}

	// Ensure finalizer before doing anything else. If we race with a
	// delete between adding the finalizer and writing it, the deletion
	// path will no-op (finalizer absent → nothing to clean up).
	if !controllerutil.ContainsFinalizer(&dr, finalizerName) {
		controllerutil.AddFinalizer(&dr, finalizerName)
		if err := r.Update(ctx, &dr); err != nil {
			// The API server refused the update — almost always a
			// conflict; the next reconcile will see the new resourceVersion.
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	taas, token, result, err := r.resolveAquaductTaaS(ctx, &dr)
	if err != nil || result != nil {
		if result != nil {
			return *result, err
		}
		return ctrl.Result{}, err
	}
	_ = taas // currently only used for endpoint/token resolution

	// Query existing server-side state. A 404 means "safe to create"; any
	// other error is transient (network, 5xx, auth) and gets retried.
	existing, err := r.APIClient.GetDomain(ctx, token, dr.Spec.DomainName)
	switch {
	case errors.Is(err, aquaducttaas.ErrDomainNotFound):
		// Not there — create it. This also handles the recovery case
		// where our status claimed we had a record but the server lost
		// it (manual deletion, DB wipe, etc.): the PUT re-establishes
		// the desired state and the status stays internally consistent
		// because we rewrite DomainID below.
		created, err := r.APIClient.PutDomain(ctx, token, dr.Spec.DomainName)
		if err != nil {
			logger.Error(err, "failed to register domain", "domain", dr.Spec.DomainName)
			return r.failure(ctx, &dr, conditionRegistered, "RegisterFailed",
				fmt.Sprintf("PUT /domain/%s: %s", dr.Spec.DomainName, err))
		}
		dr.Status.DomainID = created.ID
		dr.Status.ClobberedPreexisting = false
		logger.Info("Registered new domain on aquaduct.dev",
			"domain", dr.Spec.DomainName, "id", created.ID)
	case err != nil:
		// Transient API error. Preserve any previously-recorded
		// DomainID so a flapping API doesn't churn status.
		logger.Error(err, "failed to query domain", "domain", dr.Spec.DomainName)
		return r.failure(ctx, &dr, conditionRegistered, "APIError",
			fmt.Sprintf("GET /domain/%s: %s", dr.Spec.DomainName, err))
	default:
		// Record exists — assert our write access via PATCH. If the
		// server accepts, we own it (possibly just took it over); if
		// the server refuses with 401/403/409, the record belongs to
		// someone else and we surface a ForeignOwned condition instead
		// of falsely claiming Registered=True.
		//
		// We PATCH on every reconcile (not just the first time we see
		// an existing record) so ownership stays periodically verified
		// and a server-side reassignment surfaces within one resync.
		updated, patchErr := r.APIClient.PatchDomain(ctx, token, dr.Spec.DomainName)
		switch {
		case errors.Is(patchErr, aquaducttaas.ErrDomainForeign):
			logger.Error(patchErr, "domain is owned by a different user",
				"domain", dr.Spec.DomainName)
			return r.failure(ctx, &dr, conditionRegistered, "ForeignOwned",
				fmt.Sprintf("PATCH /domain/%s refused — record belongs to a different user: %s",
					dr.Spec.DomainName, patchErr))
		case errors.Is(patchErr, aquaducttaas.ErrDomainNotFound):
			// Race: record was deleted between our GET and PATCH.
			// Requeue immediately so the next loop hits the PUT path.
			logger.Info("Record vanished between GET and PATCH, retrying",
				"domain", dr.Spec.DomainName)
			return ctrl.Result{Requeue: true}, nil
		case patchErr != nil:
			logger.Error(patchErr, "failed to patch domain", "domain", dr.Spec.DomainName)
			return r.failure(ctx, &dr, conditionRegistered, "APIError",
				fmt.Sprintf("PATCH /domain/%s: %s", dr.Spec.DomainName, patchErr))
		}
		// If this is the first reconcile that observed the record, we
		// took ownership of something pre-existing. Flag it so operators
		// can distinguish "we created this" from "we inherited this".
		// Subsequent reconciles preserve the flag — a clobber in history
		// is a clobber forever, since the finalizer's DeleteDomain will
		// still unregister regardless of origin.
		if dr.Status.DomainID == "" {
			dr.Status.ClobberedPreexisting = true
			logger.Info("Took over pre-existing domain registration",
				"domain", dr.Spec.DomainName, "id", existing.ID)
		}
		dr.Status.DomainID = updated.ID
	}

	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionRegistered,
		Status:             metav1.ConditionTrue,
		Reason:             "Registered",
		Message:            fmt.Sprintf("Domain %q is registered on aquaduct.dev (id=%s)", dr.Spec.DomainName, dr.Status.DomainID),
		ObservedGeneration: dr.Generation,
	})

	// DNS lookup is best-effort: a registered domain with no resolving
	// A records is still partially-healthy (Registered=True) and the
	// lookup may succeed on a later reconcile without any action. So a
	// lookup failure does NOT short-circuit the status write — we just
	// mark Resolved=False and keep going.
	ips, lookupErr := r.APIClient.LookupDomain(ctx, token, dr.Spec.DomainName)
	if lookupErr != nil {
		logger.Error(lookupErr, "domain lookup failed", "domain", dr.Spec.DomainName)
		meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
			Type:               conditionResolved,
			Status:             metav1.ConditionFalse,
			Reason:             "LookupFailed",
			Message:            fmt.Sprintf("GET /domain/lookup?domain=%s: %s", dr.Spec.DomainName, lookupErr),
			ObservedGeneration: dr.Generation,
		})
	} else {
		sort.Strings(ips)
		dr.Status.ResolvedIPs = ips
		if len(ips) == 0 {
			meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
				Type:               conditionResolved,
				Status:             metav1.ConditionFalse,
				Reason:             "NoRecords",
				Message:            "aquaduct.dev lookup returned no A records yet",
				ObservedGeneration: dr.Generation,
			})
		} else {
			meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
				Type:               conditionResolved,
				Status:             metav1.ConditionTrue,
				Reason:             "Resolved",
				Message:            fmt.Sprintf("%d A record(s) returned by aquaduct.dev", len(ips)),
				ObservedGeneration: dr.Generation,
			})
		}
	}

	// Ready is the AND of the component conditions. Users who just want
	// a single signal should watch this one.
	if meta.IsStatusConditionTrue(dr.Status.Conditions, conditionRegistered) &&
		meta.IsStatusConditionTrue(dr.Status.Conditions, conditionResolved) {
		meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "domain is registered and resolves",
			ObservedGeneration: dr.Generation,
		})
	} else {
		meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "NotReady",
			Message:            "one of Registered / Resolved is not True; see component conditions",
			ObservedGeneration: dr.Generation,
		})
	}

	now := metav1.Now()
	dr.Status.LastSyncTime = &now
	dr.Status.ObservedGeneration = dr.Generation
	if err := r.Status().Update(ctx, &dr); err != nil {
		return ctrl.Result{}, err
	}
	// If the lookup failed we want a faster retry — the record might
	// resolve moments from now once DNS propagates. Registered but
	// unresolved is the common "just created" state.
	if lookupErr != nil {
		return ctrl.Result{RequeueAfter: errorRetry}, nil
	}
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// resolveAquaductTaaS loads the referenced AquaductTaaS and its token.
// Returns (taas, token, nil, nil) on success. On any recoverable failure
// returns a non-nil *ctrl.Result so the caller can forward the requeue
// to controller-runtime. On a hard error (status update failure) returns
// a real error.
func (r *DNSRecordReconciler) resolveAquaductTaaS(ctx context.Context, dr *weftv1alpha1.DNSRecord) (*weftv1alpha1.AquaductTaaS, string, *ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if dr.Spec.AquaductTaaSRef.Name == "" {
		res, err := r.failure(ctx, dr, conditionRegistered, "SpecInvalid",
			"spec.aquaductTaaSRef.name is required")
		return nil, "", &res, err
	}

	var taas weftv1alpha1.AquaductTaaS
	key := types.NamespacedName{Namespace: dr.Namespace, Name: dr.Spec.AquaductTaaSRef.Name}
	if err := r.Get(ctx, key, &taas); err != nil {
		if kerrors.IsNotFound(err) {
			res, ferr := r.failure(ctx, dr, conditionRegistered, "MissingAquaductTaaS",
				fmt.Sprintf("AquaductTaaS %q not found in namespace %q", key.Name, key.Namespace))
			return nil, "", &res, ferr
		}
		logger.Error(err, "failed to load AquaductTaaS", "ref", key)
		return nil, "", nil, err
	}

	if taas.Spec.AccessTokenSecretRef == nil {
		res, err := r.failure(ctx, dr, conditionRegistered, "SpecInvalid",
			fmt.Sprintf("AquaductTaaS %q has no spec.accessTokenSecretRef", taas.Name))
		return nil, "", &res, err
	}

	var secret corev1.Secret
	skey := types.NamespacedName{Namespace: taas.Namespace, Name: taas.Spec.AccessTokenSecretRef.Name}
	if err := r.Get(ctx, skey, &secret); err != nil {
		if kerrors.IsNotFound(err) {
			res, ferr := r.failure(ctx, dr, conditionRegistered, "SecretError",
				fmt.Sprintf("secret %q not found in namespace %q", skey.Name, skey.Namespace))
			return nil, "", &res, ferr
		}
		return nil, "", nil, err
	}
	data, ok := secret.Data[taas.Spec.AccessTokenSecretRef.Key]
	if !ok {
		res, err := r.failure(ctx, dr, conditionRegistered, "SecretError",
			fmt.Sprintf("secret %q has no key %q", skey.Name, taas.Spec.AccessTokenSecretRef.Key))
		return nil, "", &res, err
	}
	if len(data) == 0 {
		res, err := r.failure(ctx, dr, conditionRegistered, "SecretError",
			fmt.Sprintf("secret %q key %q is empty", skey.Name, taas.Spec.AccessTokenSecretRef.Key))
		return nil, "", &res, err
	}
	return &taas, string(data), nil, nil
}

// handleDeletion calls DeleteDomain and removes the finalizer. It must
// be robust to several flavors of "I can't finish cleanup right now":
//
//   - AquaductTaaS has been deleted out from under us → we have no way
//     to call the API. Surface a condition and keep retrying.
//   - Secret is gone → same story.
//   - API call itself fails → retry on the error cadence.
//
// The finalizer is only removed once DeleteDomain returns success (or
// ErrDomainNotFound, which we treat as success). That's the "must clean
// up after itself" contract from the operator brief.
func (r *DNSRecordReconciler) handleDeletion(ctx context.Context, dr *weftv1alpha1.DNSRecord) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(dr, finalizerName) {
		// Either we never added the finalizer (deletion raced creation)
		// or we've already cleaned up. Either way — done.
		return ctrl.Result{}, nil
	}

	taas, token, result, err := r.resolveAquaductTaaS(ctx, dr)
	if err != nil || result != nil {
		// Re-wrap the failure reason to make it obvious this happened
		// during cleanup, not initial registration.
		if result != nil {
			logger.Info("cleanup blocked — credentials unavailable",
				"domain", dr.Spec.DomainName)
			return *result, err
		}
		return ctrl.Result{}, err
	}
	_ = taas

	if err := r.APIClient.DeleteDomain(ctx, token, dr.Spec.DomainName); err != nil {
		logger.Error(err, "failed to delete domain", "domain", dr.Spec.DomainName)
		return r.failure(ctx, dr, conditionReady, "CleanupFailed",
			fmt.Sprintf("DELETE /domain/%s: %s", dr.Spec.DomainName, err))
	}
	logger.Info("Unregistered domain on aquaduct.dev", "domain", dr.Spec.DomainName)

	controllerutil.RemoveFinalizer(dr, finalizerName)
	if err := r.Update(ctx, dr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// failure records a False condition, logs, and schedules a short retry.
// It never returns the original error because the condition is the
// durable signal — returning a Go error would also trigger controller-
// runtime's exponential-backoff requeue, which we don't want layered on
// top of our own errorRetry.
func (r *DNSRecordReconciler) failure(ctx context.Context, dr *weftv1alpha1.DNSRecord, conditionType, reason, message string) (ctrl.Result, error) {
	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dr.Generation,
	})
	// Anything gated on Registered also blocks Ready. Keep the two
	// consistent so a user filtering on Ready=False sees this event.
	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dr.Generation,
	})
	dr.Status.ObservedGeneration = dr.Generation
	if err := r.Status().Update(ctx, dr); err != nil {
		if kerrors.IsNotFound(err) {
			// Object was deleted between our Get and this Update —
			// nothing to recover, let it go.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: errorRetry}, nil
}

// SetupWithManager wires the reconciler to its owned type. We don't
// watch AquaductTaaS changes explicitly — the 5-minute resync is fast
// enough to pick up most secret rotations, and an explicit watch would
// cross-reconcile every DNSRecord in the namespace on every AquaductTaaS
// change. Operators who need immediate convergence can force-reconcile
// via annotation.
func (r *DNSRecordReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.DNSRecord{}).
		Complete(r)
}
