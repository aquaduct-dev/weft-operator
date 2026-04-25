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

	// Compute the expected A-record set. Source of truth is the
	// AquaductTaaS's published bastion list, filtered by
	// spec.targetBastionIDs (or all non-suspended bastions when that's
	// empty — same default as the server). If the AquaductTaaS hasn't
	// finished its first sync, we don't know enough to evaluate
	// Resolved, so we wait.
	if taas.Status.LastSyncTime == nil {
		return r.failure(ctx, &dr, conditionRegistered, "WaitingForBastions",
			fmt.Sprintf("AquaductTaaS %q has not yet published its bastion list", taas.Name))
	}
	expectedIPs, targetBastionIDs, missing := selectBastions(taas.Status.Bastions, dr.Spec.TargetBastionIDs)
	if len(missing) > 0 {
		return r.failure(ctx, &dr, conditionRegistered, "UnknownBastion",
			fmt.Sprintf("spec.targetBastionIDs references unknown bastion(s): %v", missing))
	}
	dr.Status.ExpectedIPs = expectedIPs

	// Body field for PUT/PATCH. With spec.targetBastionIDs unset (and
	// thus targetBastionIDs == nil), we send no bastion_ids field —
	// server falls through to "fan out to all". With it explicitly set
	// (even to []), we send the field and the server applies exactly
	// that set. This matches the server's tri-state PATCH semantics.
	var bastionIDsForBody *[]string
	if dr.Spec.TargetBastionIDs != nil {
		ids := append([]string(nil), targetBastionIDs...)
		bastionIDsForBody = &ids
	}

	// PUT/PATCH and surface the outcome on `Registered`. Mid-loop we
	// also stash patchForeign so the post-lookup logic can rewrite the
	// reason to "ExternallyManaged" if the world is otherwise correct.
	registeredStatus := metav1.ConditionTrue
	registeredReason := "Registered"
	registeredMessage := ""
	patchForeign := false

	_, getErr := r.APIClient.GetDomain(ctx, token, dr.Spec.DomainName)
	switch {
	case errors.Is(getErr, aquaducttaas.ErrDomainNotFound):
		created, err := r.APIClient.PutDomain(ctx, token, dr.Spec.DomainName, bastionIDsForBody)
		if err != nil {
			logger.Error(err, "failed to register domain", "domain", dr.Spec.DomainName)
			return r.failure(ctx, &dr, conditionRegistered, "RegisterFailed",
				fmt.Sprintf("PUT /domain/%s: %s", dr.Spec.DomainName, err))
		}
		dr.Status.DomainID = created.ID
		dr.Status.AppliedBastionIDs = created.BastionIDs
		dr.Status.AppliedIPs = created.IPs
		dr.Status.ClobberedPreexisting = false
		registeredMessage = fmt.Sprintf("Domain %q registered (id=%s, bastions=%d)",
			dr.Spec.DomainName, created.ID, len(created.BastionIDs))
		logger.Info("Registered new domain on aquaduct.dev",
			"domain", dr.Spec.DomainName, "id", created.ID,
			"bastions", created.BastionIDs)
	case getErr != nil:
		logger.Error(getErr, "failed to query domain", "domain", dr.Spec.DomainName)
		return r.failure(ctx, &dr, conditionRegistered, "APIError",
			fmt.Sprintf("GET /domain/%s: %s", dr.Spec.DomainName, getErr))
	default:
		updated, patchErr := r.APIClient.PatchDomain(ctx, token, dr.Spec.DomainName, bastionIDsForBody)
		switch {
		case errors.Is(patchErr, aquaducttaas.ErrDomainForeign):
			// Don't return early — let the lookup run so we can decide
			// between "ForeignOwned" (records also wrong) and
			// "ExternallyManaged" (records happen to be correct). The
			// post-lookup branch flips `Resolved` and the
			// `Registered.reason` accordingly.
			logger.Info("PATCH refused as foreign-owned; will check whether DNS is otherwise correct",
				"domain", dr.Spec.DomainName)
			patchForeign = true
			registeredStatus = metav1.ConditionFalse
			registeredReason = "ForeignOwned"
			registeredMessage = fmt.Sprintf("PATCH /domain/%s refused — record belongs to a different user",
				dr.Spec.DomainName)
		case errors.Is(patchErr, aquaducttaas.ErrDomainNotFound):
			logger.Info("Record vanished between GET and PATCH, retrying",
				"domain", dr.Spec.DomainName)
			return ctrl.Result{Requeue: true}, nil
		case patchErr != nil:
			logger.Error(patchErr, "failed to patch domain", "domain", dr.Spec.DomainName)
			return r.failure(ctx, &dr, conditionRegistered, "APIError",
				fmt.Sprintf("PATCH /domain/%s: %s", dr.Spec.DomainName, patchErr))
		default:
			if dr.Status.DomainID == "" {
				dr.Status.ClobberedPreexisting = true
				logger.Info("Took over pre-existing domain registration",
					"domain", dr.Spec.DomainName, "id", updated.ID)
			}
			dr.Status.DomainID = updated.ID
			dr.Status.AppliedBastionIDs = updated.BastionIDs
			dr.Status.AppliedIPs = updated.IPs
			registeredMessage = fmt.Sprintf("Domain %q is registered (id=%s, bastions=%d)",
				dr.Spec.DomainName, updated.ID, len(updated.BastionIDs))
		}
	}

	// Lookup runs regardless of register/patch outcome. Resolved is
	// driven by ExpectedIPs vs ResolvedIPs equality — that's the
	// "domain records are correct" check.
	resolvedStatus, resolvedReason, resolvedMessage := metav1.ConditionFalse, "Unknown", ""
	ips, lookupErr := r.APIClient.LookupDomain(ctx, token, dr.Spec.DomainName)
	if lookupErr != nil {
		logger.Error(lookupErr, "domain lookup failed", "domain", dr.Spec.DomainName)
		resolvedReason = "LookupFailed"
		resolvedMessage = fmt.Sprintf("GET /domain/lookup?domain=%s: %s", dr.Spec.DomainName, lookupErr)
	} else {
		sort.Strings(ips)
		dr.Status.ResolvedIPs = ips
		switch {
		case len(expectedIPs) == 0:
			// No bastions to fan to → there are no expected A records.
			// The world is "correct" iff there also aren't any (the
			// domain points nowhere). Useful as a transient state but
			// rarely useful as an end state — surface a distinct reason.
			if len(ips) == 0 {
				resolvedStatus = metav1.ConditionTrue
				resolvedReason = "NoTargets"
				resolvedMessage = "no bastions to fan to and no A records present"
			} else {
				resolvedReason = "UnexpectedRecords"
				resolvedMessage = fmt.Sprintf("expected no A records, got %d", len(ips))
			}
		case stringSlicesEqual(ips, expectedIPs):
			resolvedStatus = metav1.ConditionTrue
			resolvedReason = "Resolved"
			resolvedMessage = fmt.Sprintf("%d A record(s) match expected bastion IPs", len(ips))
		default:
			resolvedReason = "Mismatched"
			resolvedMessage = fmt.Sprintf("expected %v, got %v", expectedIPs, ips)
		}
	}

	// If PATCH was refused as foreign-owned but the records are
	// already correct, that's the "externally managed" case — Ready
	// follows Resolved (=True), and Registered carries an
	// informational reason rather than a hard failure. The DELETE
	// finalizer will still run on teardown and the server's auth
	// layer will decide whether to honor it.
	if patchForeign && resolvedStatus == metav1.ConditionTrue {
		registeredReason = "ExternallyManaged"
		registeredMessage = fmt.Sprintf("Domain %q is registered to a different user, but DNS records match the expected bastion IPs", dr.Spec.DomainName)
	}

	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionRegistered,
		Status:             registeredStatus,
		Reason:             registeredReason,
		Message:            registeredMessage,
		ObservedGeneration: dr.Generation,
	})
	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionResolved,
		Status:             resolvedStatus,
		Reason:             resolvedReason,
		Message:            resolvedMessage,
		ObservedGeneration: dr.Generation,
	})

	// Ready follows Resolved alone — the user's framing was "indicate
	// success if the domain records are correct", regardless of who
	// owns them. Registered is informational.
	readyStatus := metav1.ConditionFalse
	readyReason := resolvedReason
	readyMessage := resolvedMessage
	if resolvedStatus == metav1.ConditionTrue {
		readyStatus = metav1.ConditionTrue
		readyReason = "Ready"
		readyMessage = "domain records match expected bastion IPs"
	}
	meta.SetStatusCondition(&dr.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMessage,
		ObservedGeneration: dr.Generation,
	})

	now := metav1.Now()
	dr.Status.LastSyncTime = &now
	dr.Status.ObservedGeneration = dr.Generation
	if err := r.Status().Update(ctx, &dr); err != nil {
		return ctrl.Result{}, err
	}
	if lookupErr != nil || resolvedStatus != metav1.ConditionTrue {
		// Record exists but the world isn't yet correct: short retry
		// while DNS propagates / cloudflare diffs apply / external
		// owner releases the domain.
		return ctrl.Result{RequeueAfter: errorRetry}, nil
	}
	return ctrl.Result{RequeueAfter: resyncInterval}, nil
}

// selectBastions resolves spec.targetBastionIDs against the AquaductTaaS's
// published bastion list. Returns the IPs in deterministic (sorted) order,
// the matching bastion IDs (also sorted), and any IDs that didn't match
// anything in the list (so the reconciler can surface UnknownBastion).
//
// With targetIDs nil/empty, we include every non-suspended bastion —
// matching the server's "fan out to all" default — and `missing` is empty.
func selectBastions(bastions []weftv1alpha1.BastionInfo, targetIDs []string) (ips []string, ids []string, missing []string) {
	byID := make(map[string]weftv1alpha1.BastionInfo, len(bastions))
	for _, b := range bastions {
		byID[b.ID] = b
	}
	if len(targetIDs) == 0 {
		for _, b := range bastions {
			if b.Suspended || b.IP == "" {
				continue
			}
			ips = append(ips, b.IP)
			ids = append(ids, b.ID)
		}
	} else {
		for _, want := range targetIDs {
			b, ok := byID[want]
			if !ok {
				missing = append(missing, want)
				continue
			}
			if b.IP == "" {
				// A bastion that's known but has no IP is effectively
				// unusable for DNS — surface it as missing rather than
				// silently dropping it.
				missing = append(missing, want)
				continue
			}
			ips = append(ips, b.IP)
			ids = append(ids, b.ID)
		}
	}
	sort.Strings(ips)
	sort.Strings(ids)
	sort.Strings(missing)
	return
}

// stringSlicesEqual compares two pre-sorted string slices for set
// equality. Both slices must already be sorted (the caller guarantees
// this). Faster than a set-based compare and avoids allocation.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
		if errors.Is(err, aquaducttaas.ErrDomainForeign) {
			// The record is owned by someone else. We never had write
			// access, so there's nothing for the operator to clean up
			// — drop the finalizer and let the object delete. Logged
			// at info level so operators can spot externally-managed
			// records that weft never controlled.
			logger.Info("DELETE refused as foreign-owned; record was never ours, removing finalizer",
				"domain", dr.Spec.DomainName, "error", err)
		} else {
			logger.Error(err, "failed to delete domain", "domain", dr.Spec.DomainName)
			return r.failure(ctx, dr, conditionReady, "CleanupFailed",
				fmt.Sprintf("DELETE /domain/%s: %s", dr.Spec.DomainName, err))
		}
	} else {
		logger.Info("Unregistered domain on aquaduct.dev", "domain", dr.Spec.DomainName)
	}

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
