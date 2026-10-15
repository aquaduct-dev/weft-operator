// trust.go — the reflector's side of client trust. It publishes the shared
// trust bundle (system roots + every reflector CA, see internal/trust) in
// every namespace for the injection webhook to mount, and it computes the
// trust gate: the DNS override and node agent are only enabled while every
// running pod already trusts this reflector's CA. Pods that predate the
// webhook (or were admitted while it was down) hold the gate closed until
// they are replaced, so turning a reflector on never breaks a running pod.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/trust"
)

// untrustedSampleSize bounds the pod names reported in status.
const untrustedSampleSize = 20

// publishTrustBundle writes the current bundle into every live namespace.
func (r *Reconciler) publishTrustBundle(ctx context.Context) error {
	logger := log.FromContext(ctx)
	bundle, err := r.Trust.Build(ctx)
	if err != nil {
		return fmt.Errorf("build trust bundle: %w", err)
	}
	if bundle.Empty() {
		return nil
	}
	var reflectors weftv1alpha1.WeftLocalDNSReflectorList
	if err := r.List(ctx, &reflectors); err != nil {
		return fmt.Errorf("list reflectors: %w", err)
	}
	cas, err := r.Trust.CAs(ctx)
	if err != nil {
		return err
	}
	owners := trust.OwnerRefs(reflectors.Items, cas)

	var namespaces corev1.NamespaceList
	if err := r.List(ctx, &namespaces); err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}
	changed := 0
	for _, ns := range namespaces.Items {
		if ns.Status.Phase == corev1.NamespaceTerminating || !ns.DeletionTimestamp.IsZero() {
			continue
		}
		op, err := trust.EnsureConfigMap(ctx, r.Client, ns.Name, bundle, owners)
		if apierrors.IsAlreadyExists(err) {
			// The cache only sees operator-labelled ConfigMaps, so this is
			// someone else's object with our name. Leave it alone.
			logger.Info("Namespace has an unrelated ConfigMap named like the trust bundle; not publishing there", "namespace", ns.Name, "configmap", trust.ConfigMapName)
			continue
		}
		if err != nil {
			return fmt.Errorf("publish trust bundle to %s: %w", ns.Name, err)
		}
		if op != controllerutil.OperationResultNone {
			changed++
			logger.V(1).Info("Trust bundle published", "namespace", ns.Name, "operation", op)
		}
	}
	if changed > 0 {
		logger.Info("Trust bundle ConfigMaps reconciled", "changed", changed, "namespaces", len(namespaces.Items), "cas", trust.FingerprintList(bundle.Fingerprints))
	}
	return nil
}

// untrustedPods lists running pods that would see this reflector's
// certificate without trusting it. It reads pod metadata only, straight from
// the API server (pods are not cached by this operator).
func (r *Reconciler) untrustedPods(ctx context.Context, caFingerprint string) (int32, []string, error) {
	pods := &metav1.PartialObjectMetadataList{}
	pods.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
	if err := r.APIReader.List(ctx, pods, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("status.phase", string(corev1.PodRunning))}); err != nil {
		return 0, nil, fmt.Errorf("list running pods: %w", err)
	}
	var count int32
	var sample []string
	for _, p := range pods.Items {
		if r.exemptFromTrust(p.Namespace, p.Labels, p.Annotations) || trust.TrustsCA(p.Annotations, caFingerprint) {
			continue
		}
		count++
		sample = append(sample, p.Namespace+"/"+p.Name)
	}
	sort.Strings(sample)
	if len(sample) > untrustedSampleSize {
		sample = sample[:untrustedSampleSize]
	}
	return count, sample, nil
}

// exemptFromTrust mirrors the webhook's selectors: excluded namespaces,
// opted-out pods, and static (mirror) pods, which admission never sees.
func (r *Reconciler) exemptFromTrust(namespace string, labels, annotations map[string]string) bool {
	if slices.Contains(r.ExcludedNamespaces, namespace) {
		return true
	}
	if labels[trust.OptOutKey] == "false" || annotations[trust.OptOutKey] == "false" {
		return true
	}
	_, mirror := annotations[corev1.MirrorPodAnnotationKey]
	return mirror
}
