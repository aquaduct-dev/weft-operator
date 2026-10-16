// reconciler.go — the WeftLocalDNSReflector controller. One pass:
//
//  1. match spec hostnames against WeftTunnels to get proxy routes
//  2. ensure the CA + serving certificate and publish the shared trust
//     bundle everywhere (the trust webhook injects it into new pods)
//  3. ensure the proxy Service and Deployment (`weft proxy` with static TLS)
//  4. once the proxy is available AND every running pod trusts the CA,
//     point CoreDNS at the Service; otherwise withdraw the override so
//     pods keep using the public path
//  5. under the same gate, run the node agent so containerd pulls and host
//     processes follow the same path
//
// The CoreDNS key lives in a shared ConfigMap the reflector cannot own, so a
// finalizer removes it on delete; everything else is garbage-collected
// through ownerReferences.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/pki"
	"aquaduct.dev/weft-operator/internal/trust"
)

const (
	conditionReady       = "Ready"
	conditionCertificate = "CertificateReady"
	conditionProxy       = "ProxyAvailable"
	conditionDNS         = "DNSPublished"
	conditionTrust       = "ClientsTrustCA"

	// maxRequeue bounds the wait for the next certificate renewal check.
	maxRequeue = 12 * time.Hour
	// trustRecheck is how often pod trust is re-evaluated: pods admitted
	// while the webhook was down must close the gate promptly.
	trustRecheck = 2 * time.Minute
	// trustBlockedRecheck is the faster recheck while the gate is closed.
	trustBlockedRecheck = 30 * time.Second
)

// Reconciler reconciles WeftLocalDNSReflector objects.
type Reconciler struct {
	client.Client
	// APIReader reads objects the manager cache deliberately excludes
	// (the shared CoreDNS custom ConfigMap).
	APIReader client.Reader
	Scheme    *runtime.Scheme
	// WeftImage runs `weft proxy`.
	WeftImage string
	// OperatorImage runs the `node-agent` subcommand of this binary.
	OperatorImage string
	// Trust builds the shared trust bundle (shared with the webhook).
	Trust *trust.Source
	// ExcludedNamespaces are not injected by the trust webhook, so their
	// pods are exempt from the trust gate.
	ExcludedNamespaces []string
	// TrustRecheck / TrustBlockedRecheck override how often pod trust is
	// re-evaluated with the gate open / closed (0 means the defaults).
	TrustRecheck        time.Duration
	TrustBlockedRecheck time.Duration
	// Now is a clock seam for tests; nil means time.Now.
	Now func() time.Time
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftlocaldnsreflectors,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftlocaldnsreflectors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=weftlocaldnsreflectors/finalizers,verbs=update
//+kubebuilder:rbac:groups=weft.aquaduct.dev,resources=wefttunnels,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments;daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;secrets;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces;pods,verbs=get;list;watch

// Reconcile drives one WeftLocalDNSReflector toward its desired state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("reflector", req.Name)
	ctx = log.IntoContext(ctx, logger)

	refl := &weftv1alpha1.WeftLocalDNSReflector{}
	if err := r.Get(ctx, req.NamespacedName, refl); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	applyDefaults(refl)

	if !refl.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, refl)
	}
	if controllerutil.AddFinalizer(refl, finalizer) {
		logger.Info("Adding finalizer")
		if err := r.Update(ctx, refl); err != nil {
			return ctrl.Result{}, err
		}
		applyDefaults(refl)
	}

	before := refl.Status.DeepCopy()
	result, err := r.reconcile(ctx, refl)
	if err != nil {
		logger.Error(err, "Reconcile failed")
		setCondition(refl, conditionReady, false, "ReconcileError", err.Error())
	}
	refl.Status.ObservedGeneration = refl.Generation
	if !equality.Semantic.DeepEqual(before, &refl.Status) {
		if uerr := r.Status().Update(ctx, refl); apierrors.IsConflict(uerr) {
			// The cache handed us a reflector older than our own last
			// write; retry against the fresh one rather than backing off.
			logger.V(1).Info("Status update conflicted; requeueing")
			if err == nil {
				return ctrl.Result{Requeue: true}, nil
			}
		} else if uerr != nil {
			logger.Error(uerr, "Failed to update status")
			if err == nil {
				err = uerr
			}
		}
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	return result, nil
}

func (r *Reconciler) reconcile(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tunnels weftv1alpha1.WeftTunnelList
	if err := r.List(ctx, &tunnels); err != nil {
		return ctrl.Result{}, fmt.Errorf("list WeftTunnels: %w", err)
	}
	var reflectors weftv1alpha1.WeftLocalDNSReflectorList
	if err := r.List(ctx, &reflectors); err != nil {
		return ctrl.Result{}, fmt.Errorf("list reflectors: %w", err)
	}
	claimed, conflicting := resolveClaims(refl, reflectors.Items, tunnels.Items)
	if len(conflicting) > 0 {
		logger.Info("Hostnames owned by another reflector", "hostnames", conflicting)
	}
	refl.Status.ReflectedHostnames = claimed
	refl.Status.ConflictingHostnames = conflicting

	routes := computeRoutes(claimed, tunnels.Items)
	for tunnel, reason := range routes.Skipped {
		logger.Info("WeftTunnel not reflected", "tunnel", tunnel, "reason", reason)
	}
	logger.V(1).Info("Computed routes", "routes", len(routes.Routes), "unrouted", routes.Unrouted)
	refl.Status.Routes = routes.Routes
	refl.Status.UnroutedHostnames = routes.Unrouted

	// The certificate covers every claimed hostname, routed or not, so for
	// an explicit list a tunnel appearing or disappearing never forces a
	// reissue (allHostnames reissues as the set of tunnels changes).
	certs, err := r.ensureCertificates(ctx, refl, certNames(refl, claimed))
	if err != nil {
		setCondition(refl, conditionCertificate, false, "IssueFailed", err.Error())
		return ctrl.Result{}, err
	}
	refl.Status.CertificateNotAfter = &metav1.Time{Time: certs.NotAfter}
	setCondition(refl, conditionCertificate, true, "Issued", "serving certificate expires "+certs.NotAfter.UTC().Format(time.RFC3339))

	if err := r.publishTrustBundle(ctx); err != nil {
		return ctrl.Result{}, err
	}
	refl.Status.CABundleConfigMap = trust.ConfigMapName

	// Trust gate: is there any running pod that would be handed our
	// certificate without trusting it?
	untrusted, sample, err := r.untrustedPods(ctx, pki.Fingerprint(certs.CACertPEM))
	if err != nil {
		return ctrl.Result{}, err
	}
	refl.Status.UntrustedPods = untrusted
	refl.Status.UntrustedPodSample = sample
	trusted := untrusted == 0 || refl.Spec.IgnoreUntrustedPods
	switch {
	case untrusted == 0:
		setCondition(refl, conditionTrust, true, "AllPodsTrust", "every running pod trusts the reflector CA")
	case refl.Spec.IgnoreUntrustedPods:
		setCondition(refl, conditionTrust, true, "Ignored", fmt.Sprintf("%d running pod(s) do not trust the CA; ignored by spec", untrusted))
	default:
		setCondition(refl, conditionTrust, false, "PodsNeedRestart", fmt.Sprintf("%d running pod(s) were created before trust injection and must be restarted, e.g. %s", untrusted, strings.Join(sample, ", ")))
	}

	serviceIP, err := r.ensureService(ctx, refl)
	if err != nil {
		return ctrl.Result{}, err
	}
	refl.Status.ServiceIP = serviceIP

	dep, err := r.ensureProxy(ctx, refl, routes, certs.Fingerprint)
	if err != nil {
		return ctrl.Result{}, err
	}
	available := deploymentAvailable(dep) && len(routes.Routes) > 0
	switch {
	case len(routes.Routes) == 0:
		setCondition(refl, conditionProxy, false, "NoRoutes", "no WeftTunnel serves any claimed hostname "+strings.Join(claimed, ", "))
	case !available:
		setCondition(refl, conditionProxy, false, "Waiting", "proxy deployment has no available replica yet")
	default:
		setCondition(refl, conditionProxy, true, "Available", fmt.Sprintf("%d route(s) served", len(routes.Routes)))
	}

	// DNS only points at the proxy while it can answer; otherwise the
	// override is withdrawn so pods keep working through the public path.
	routed := routes.RoutedHostnames()
	dnsContent := ""
	if !refl.Spec.ClusterDNS.Disabled && available && trusted && serviceIP != "" {
		if dnsContent, err = renderCoreDNS(refl.Name, serviceIP, routed); err != nil {
			setCondition(refl, conditionDNS, false, "RenderFailed", err.Error())
			return ctrl.Result{}, err
		}
	}
	published, err := r.syncCoreDNS(ctx, refl, dnsContent)
	if err != nil {
		setCondition(refl, conditionDNS, false, "SyncFailed", err.Error())
		return ctrl.Result{}, err
	}
	switch {
	case refl.Spec.ClusterDNS.Disabled:
		setCondition(refl, conditionDNS, true, "Disabled", "cluster DNS override disabled by spec")
	case published:
		setCondition(refl, conditionDNS, true, "Published", strings.Join(routed, ", ")+" → "+serviceIP)
	case !trusted:
		setCondition(refl, conditionDNS, false, "WaitingForTrust", "override withheld until every running pod trusts the CA")
	default:
		setCondition(refl, conditionDNS, false, "Withdrawn", "override withheld until the proxy is available")
	}

	// The agent gates its own /etc/hosts entry on a verified TLS handshake,
	// so it only needs the trust gate here: hostNetwork pods and host
	// processes resolve through /etc/hosts.
	agentHosts := routed
	if !trusted {
		agentHosts = nil
	}
	if err := r.ensureNodeAgent(ctx, refl, serviceIP, agentHosts); err != nil {
		return ctrl.Result{}, err
	}

	ready := available && trusted && (published || refl.Spec.ClusterDNS.Disabled)
	if ready {
		setCondition(refl, conditionReady, true, "Reflecting", fmt.Sprintf("%s served in-cluster at %s", strings.Join(routed, ", "), serviceIP))
	} else {
		setCondition(refl, conditionReady, false, "NotReady", "see ProxyAvailable, ClientsTrustCA and DNSPublished")
	}

	// Come back to re-check pod trust, and in time to renew the certificate.
	recheck := orDefault(r.TrustRecheck, trustRecheck)
	if !trusted {
		recheck = orDefault(r.TrustBlockedRecheck, trustBlockedRecheck)
	}
	requeue := min(certs.NotAfter.Add(-pki.LeafRenewBefore).Sub(r.now()), maxRequeue, recheck)
	requeue = max(requeue, min(recheck, 10*time.Second))
	return ctrl.Result{RequeueAfter: requeue}, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// finalize removes the CoreDNS key before letting the reflector go. Owned
// objects (proxy, node agent, Secrets, CA ConfigMaps) are garbage-collected.
func (r *Reconciler) finalize(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector) error {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(refl, finalizer) {
		return nil
	}
	logger.Info("Reflector deleted; withdrawing CoreDNS override")
	if _, err := r.syncCoreDNS(ctx, refl, ""); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(refl, finalizer)
	return r.Update(ctx, refl)
}

// applyDefaults fills spec defaults in memory for objects created before
// the CRD carried them (the API server applies them for new objects).
func applyDefaults(refl *weftv1alpha1.WeftLocalDNSReflector) {
	if refl.Spec.Namespace == "" {
		refl.Spec.Namespace = "weft-system"
	}
	if refl.Spec.ClusterDNS.ConfigMapNamespace == "" {
		refl.Spec.ClusterDNS.ConfigMapNamespace = "kube-system"
	}
	if refl.Spec.ClusterDNS.ConfigMapName == "" {
		refl.Spec.ClusterDNS.ConfigMapName = "coredns-custom"
	}
	if refl.Spec.NodeTrust.ContainerdCertsDir == "" {
		refl.Spec.NodeTrust.ContainerdCertsDir = "/var/lib/rancher/k3s/agent/etc/containerd/certs.d"
	}
}

func setCondition(refl *weftv1alpha1.WeftLocalDNSReflector, kind string, ok bool, reason, message string) {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&refl.Status.Conditions, metav1.Condition{
		Type:               kind,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: refl.Generation,
	})
}

// SetupWithManager registers the controller and its watches.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueAll := handler.EnqueueRequestsFromMapFunc(r.allReflectors)
	return ctrl.NewControllerManagedBy(mgr).
		For(&weftv1alpha1.WeftLocalDNSReflector{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		// Claims depend on every other reflector: one appearing, changing
		// or going away can move hostnames between them.
		Watches(&weftv1alpha1.WeftLocalDNSReflector{}, enqueueAll, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// Routes come from tunnels; any tunnel change can add or remove one.
		Watches(&weftv1alpha1.WeftTunnel{}, enqueueAll, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// New namespaces need the CA bundle.
		Watches(&corev1.Namespace{}, enqueueAll, builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(event.CreateEvent) bool { return true },
			UpdateFunc:  func(event.UpdateEvent) bool { return false },
			DeleteFunc:  func(event.DeleteEvent) bool { return false },
			GenericFunc: func(event.GenericEvent) bool { return false },
		})).
		Complete(r)
}

// allReflectors enqueues every reflector; there are only ever a handful.
func (r *Reconciler) allReflectors(ctx context.Context, _ client.Object) []reconcile.Request {
	var list weftv1alpha1.WeftLocalDNSReflectorList
	if err := r.List(ctx, &list); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list WeftLocalDNSReflectors for fan-out")
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, item := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}})
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Name < reqs[j].Name })
	return reqs
}
