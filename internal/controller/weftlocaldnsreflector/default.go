// default.go — the operator-managed cluster-default WeftLocalDNSReflector.
// When enabled (the chart's defaultReflector.enabled, default true) the
// leader keeps a reflector with allHostnames (or the configured hostnames)
// in place; when disabled it deletes that reflector again. It only ever
// touches a reflector carrying its label, so a hand-made reflector that
// happens to use the same name is left alone.
//
// This lives in the operator rather than as a chart template because the
// chart ships the CRDs as templates: Helm cannot create a custom resource in
// the same release that first installs its CRD.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftlocaldnsreflectors,verbs=get;list;watch;create;update;patch;delete

// DefaultReflectorLabel marks the reflector the operator manages itself.
const DefaultReflectorLabel = "weft.aquaduct.dev/default-reflector"

// DefaultReflector is a manager Runnable that ensures (or removes) the
// cluster-default reflector, at start and then every Interval.
type DefaultReflector struct {
	Client client.Client
	// Enabled creates/updates the reflector; false deletes an operator-made one.
	Enabled bool
	// Name of the reflector (cluster-scoped).
	Name string
	// Hostnames, if set, are reflected explicitly; empty means allHostnames.
	Hostnames []string
	// Interval between re-checks (so a deleted default comes back). 0 = 10m.
	Interval time.Duration
	// RetryInterval after a failed sync. 0 = 30s.
	RetryInterval time.Duration
}

// NeedLeaderElection keeps this on the leader only.
func (d *DefaultReflector) NeedLeaderElection() bool { return true }

// Start implements manager.Runnable.
func (d *DefaultReflector) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("default-reflector")
	every := d.Interval
	if every == 0 {
		every = 10 * time.Minute
	}
	retry := d.RetryInterval
	if retry == 0 {
		retry = 30 * time.Second
	}
	for {
		wait := every
		if err := d.Sync(ctx); err != nil {
			logger.Error(err, "Syncing the cluster-default reflector failed; will retry", "name", d.Name, "enabled", d.Enabled, "in", retry)
			wait = retry
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// Sync makes the cluster match the configuration once.
func (d *DefaultReflector) Sync(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("default-reflector").WithValues("name", d.Name)
	existing := &weftv1alpha1.WeftLocalDNSReflector{}
	err := d.Client.Get(ctx, types.NamespacedName{Name: d.Name}, existing)
	found := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("get reflector %s: %w", d.Name, err)
	}
	ours := found && existing.Labels[DefaultReflectorLabel] == "true"

	if !d.Enabled {
		if ours {
			logger.Info("Default reflector disabled; deleting it")
			if err := d.Client.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("delete reflector %s: %w", d.Name, err)
			}
		}
		return nil
	}
	if found && !ours {
		logger.Info("A reflector with the default name exists but was not created by the operator; leaving it alone")
		return nil
	}

	want := weftv1alpha1.WeftLocalDNSReflectorSpec{AllHostnames: len(d.Hostnames) == 0}
	if len(d.Hostnames) > 0 {
		want.Hostnames = sortedUnique(d.Hostnames)
	}
	if !found {
		refl := &weftv1alpha1.WeftLocalDNSReflector{
			ObjectMeta: metav1.ObjectMeta{Name: d.Name, Labels: map[string]string{DefaultReflectorLabel: "true", createdByLabel: createdByValue}},
			Spec:       want,
		}
		logger.Info("Creating the cluster-default reflector", "allHostnames", want.AllHostnames, "hostnames", want.Hostnames)
		if err := d.Client.Create(ctx, refl); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create reflector %s: %w", d.Name, err)
		}
		return nil
	}
	if existing.Spec.AllHostnames == want.AllHostnames && slices.Equal(existing.Spec.Hostnames, want.Hostnames) {
		return nil
	}
	// Only the hostname selection is owned here; every other spec field
	// stays as an administrator may have tuned it.
	existing.Spec.AllHostnames = want.AllHostnames
	existing.Spec.Hostnames = want.Hostnames
	logger.Info("Updating the cluster-default reflector's hostnames", "allHostnames", want.AllHostnames, "hostnames", want.Hostnames)
	return d.Client.Update(ctx, existing)
}
