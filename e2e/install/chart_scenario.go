// chart_scenario.go: the "chart" scenario (//e2e/install:chart_test). The
// real packaged chart is installed, upgraded and uninstalled with the real
// helm binary inside the VM: adopting CRDs that were applied outside Helm,
// the bundled VPA stack in each installController mode, CRD upgrades under
// existing CRs, and extraEnv.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	release     = "weft-operator"
	chartNew    = "/etc/e2e/weft-operator.tgz"
	chartOld    = "/etc/e2e/weft-operator-old.tgz"
	oldCRDs     = "/etc/e2e/weft-operator-old-crds.yaml"
	chartValues = "/etc/e2e/chart-values.yaml"
	legacyCR    = "legacy"
	envName     = "E2E_MARKER"
	envValue    = "from-extra-env"
	vpaCRD      = "verticalpodautoscalers.autoscaling.k8s.io"

	reflectorCRD       = "weftlocaldnsreflectors.weft.aquaduct.dev"
	reflectorFinalizer = "weft.aquaduct.dev/cleanup-coredns"
)

var (
	weftCRDs = []string{
		"aquaducttaases.weft.aquaduct.dev",
		"dnsrecords.weft.aquaduct.dev",
		"weftgateways.weft.aquaduct.dev",
		"weftlocaldnsreflectors.weft.aquaduct.dev",
		"weftservers.weft.aquaduct.dev",
		"wefttunnels.weft.aquaduct.dev",
	}
	vpaDeployments = []string{"vpa-recommender", "vpa-updater", "vpa-admission-controller"}
	vpaGVR         = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}
	crdGVR         = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
)

type chart struct {
	k   *e2ekit.Kit
	dyn dynamic.Interface
	// UIDs that must survive the upgrade.
	legacyUID types.UID
	vpaUIDs   map[string]types.UID
}

func chartSteps(k *e2ekit.Kit) []e2ekit.Step {
	s := &chart{k: k, vpaUIDs: map[string]types.UID{}}
	k.Diagnostics = s.diagnostics
	return []e2ekit.Step{
		{ID: "chart.unowned-crds-rejected", Label: "helm upgrade --install refuses weft CRDs that exist without Helm ownership metadata", Fn: s.unownedCRDsRejected},
		{ID: "chart.adopted-install", Label: "after labelling/annotating the CRDs for the release, helm upgrade --install (old chart) succeeds and the operator is Ready", Fn: s.adoptedInstall},
		{ID: "chart.vpa-auto-installs-stack", Label: "installController=auto on a cluster without VPA installs the VPA CRDs + recommender/updater/admission-controller, Available, updater --min-replicas=1, operator VPA not yet attached", Fn: s.vpaAutoInstallsStack},
		{ID: "chart.cr-before-upgrade", Label: "a WeftServer created under the old CRD (no spec.location default) is stored without spec.location", Fn: s.crBeforeUpgrade},
		{ID: "chart.adopted-upgrade-conflicts", Label: "helm v4's server-side apply refuses to change the adopted CRDs: conflict with the kubectl-client-side-apply field manager over .spec.versions", Fn: s.adoptedUpgradeConflicts},
		{ID: "chart.upgrade-crds", Label: "helm upgrade --force-conflicts to the current chart updates the CRDs in place: the CR survives and now reads back the new spec.location default", Fn: s.upgradeCRDs},
		{ID: "chart.vpa-attached-on-upgrade", Label: "on the upgrade the operator's own VPA object attaches (updateMode Off, memory only) and the bundled stack stays installed", Fn: s.vpaAttachedOnUpgrade},
		{ID: "chart.extra-env", Label: "extraEnv reaches the operator Deployment and the running operator process", Fn: s.extraEnv},
		{ID: "chart.uninstall", Label: "helm uninstall removes the operator, VPA stack, GatewayClass, CRDs and their CRs, except the reflector CRD, stuck Terminating behind the cluster-default finalizer (known defect; recovered by removing it)", Fn: s.uninstall},
		{ID: "chart.take-ownership-vpa-false", Label: "unowned CRDs: --take-ownership alone conflicts with kubectl-client-side-apply, --take-ownership --force-conflicts adopts them; installController=false installs no vpa-* objects", Fn: s.takeOwnershipVPAFalse},
		{ID: "chart.vpa-auto-foreign-crd", Label: "installController=auto with a VPA CRD owned by someone else installs no stack but attaches the operator's VPA", Fn: s.vpaAutoForeignCRD},
	}
}

func (s *chart) helmUpgrade(ctx context.Context, chartPath string, extra ...string) (string, error) {
	args := append([]string{"upgrade", "--install", release, chartPath, "--namespace", ns, "--values", chartValues}, extra...)
	return helm(ctx, s.k, args...)
}

func (s *chart) unownedCRDsRejected(ctx context.Context) error {
	k := s.k
	var err error
	if s.dyn, err = dynamic.NewForConfig(k.Config); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "CoreDNS available", k.DeploymentAvailable("kube-system", "coredns")); err != nil {
		return err
	}
	if err := k.Apply(ctx, "/etc/e2e/gateway-api-crds.yaml", "default"); err != nil {
		return err
	}
	// What a pre-templates install (or a hand `kubectl apply`) left behind.
	if out, err := k.Kubectl(ctx, "apply", "-f", oldCRDs); err != nil {
		return err
	} else {
		log.Print(out)
	}
	out, err := s.helmUpgrade(ctx, chartOld)
	if err == nil {
		return fmt.Errorf("helm accepted CRDs without Helm ownership metadata:\n%s", out)
	}
	log.Printf("EXPECTED helm failure on unowned CRDs:\n%s", out)
	for _, want := range []string{"cannot be imported into the current release", "invalid ownership metadata", `"app.kubernetes.io/managed-by": must be set to "Helm"`, "meta.helm.sh/release-name", "meta.helm.sh/release-namespace"} {
		if !strings.Contains(out, want) {
			return fmt.Errorf("helm error lacks %q:\n%s", want, out)
		}
	}
	if _, err := deployment(ctx, k, ns, "weft-operator"); !apierrors.IsNotFound(err) {
		return fmt.Errorf("operator Deployment exists after the failed install (err=%v)", err)
	}
	return nil
}

func (s *chart) adoptedInstall(ctx context.Context) error {
	k := s.k
	if _, err := k.Kubectl(ctx, append([]string{"label", "--overwrite", "crd"}, append(weftCRDs, "app.kubernetes.io/managed-by=Helm")...)...); err != nil {
		return err
	}
	if _, err := k.Kubectl(ctx, append([]string{"annotate", "--overwrite", "crd"}, append(weftCRDs, "meta.helm.sh/release-name="+release, "meta.helm.sh/release-namespace="+ns)...)...); err != nil {
		return err
	}
	if _, err := s.helmUpgrade(ctx, chartOld); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 4*time.Minute, "operator available", k.DeploymentAvailable(ns, "weft-operator"))
}

func (s *chart) operatorVPA(ctx context.Context) (*unstructured.Unstructured, error) {
	return s.dyn.Resource(vpaGVR).Namespace(ns).Get(ctx, "weft-operator", metav1.GetOptions{})
}

func (s *chart) crdExists(ctx context.Context, name string) (bool, error) {
	_, err := s.dyn.Resource(crdGVR).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *chart) vpaAutoInstallsStack(ctx context.Context) error {
	k := s.k
	for _, d := range vpaDeployments {
		if err := e2ekit.WaitFor(ctx, 4*time.Minute, d+" available", k.DeploymentAvailable(ns, d)); err != nil {
			return err
		}
		dep, err := deployment(ctx, k, ns, d)
		if err != nil {
			return err
		}
		s.vpaUIDs[d] = dep.UID
	}
	for _, c := range []string{vpaCRD, "verticalpodautoscalercheckpoints.autoscaling.k8s.io"} {
		if ok, err := s.crdExists(ctx, c); err != nil || !ok {
			return fmt.Errorf("CRD %s not installed (err=%v)", c, err)
		}
	}
	up, err := deployment(ctx, k, ns, "vpa-updater")
	if err != nil {
		return err
	}
	if args := up.Spec.Template.Spec.Containers[0].Args; !slices.Contains(args, "--min-replicas=1") {
		return fmt.Errorf("vpa-updater args %v lack --min-replicas=1", args)
	}
	// The release's first render could not see the VPA API it was installing.
	if _, err := s.operatorVPA(ctx); !apierrors.IsNotFound(err) {
		return fmt.Errorf("operator VPA present after the first install (err=%v); expected it to attach on the next upgrade", err)
	}
	return nil
}

func (s *chart) legacyLocation(ctx context.Context) (string, bool, types.UID, error) {
	u, err := s.dyn.Resource(weftv1alpha1.GroupVersion.WithResource("weftservers")).Namespace(appNS).Get(ctx, legacyCR, metav1.GetOptions{})
	if err != nil {
		return "", false, "", err
	}
	loc, found, err := unstructured.NestedString(u.Object, "spec", "location")
	return loc, found, u.GetUID(), err
}

func (s *chart) crBeforeUpgrade(ctx context.Context) error {
	if err := applyYAML(ctx, s.k, "legacy", fmt.Sprintf(`
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftServer
metadata:
  name: %s
  namespace: %s
spec:
  connectionString: weft://legacy@127.0.0.1:9092
`, legacyCR, appNS)); err != nil {
		return err
	}
	loc, found, uid, err := s.legacyLocation(ctx)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("old CRD defaulted spec.location=%q; the old chart should not have that default", loc)
	}
	s.legacyUID = uid
	return nil
}

// adoptedUpgradeConflicts: labels + annotations satisfy Helm's ownership
// check, but kubectl's client-side apply still owns the CRDs' fields, and
// helm v4 applies server-side, so the first upgrade that changes a CRD
// conflicts.
func (s *chart) adoptedUpgradeConflicts(ctx context.Context) error {
	out, err := s.helmUpgrade(ctx, chartNew)
	if err == nil {
		return fmt.Errorf("upgrade of adopted CRDs went through without --force-conflicts:\n%s", out)
	}
	log.Printf("EXPECTED helm failure upgrading adopted CRDs:\n%s", out)
	for _, want := range []string{"UPGRADE FAILED", "Apply failed with 1 conflict", `conflict with "kubectl-client-side-apply"`, ".spec.versions"} {
		if !strings.Contains(out, want) {
			return fmt.Errorf("helm error lacks %q:\n%s", want, out)
		}
	}
	return nil
}

func (s *chart) upgradeCRDs(ctx context.Context) error {
	k := s.k
	if _, err := s.helmUpgrade(ctx, chartNew, "--force-conflicts", "--set-json", fmt.Sprintf(`extraEnv=[{"name":%q,"value":%q}]`, envName, envValue)); err != nil {
		return err
	}
	// The CRD's schema now carries the default.
	out, err := k.Kubectl(ctx, "get", "crd", "weftservers.weft.aquaduct.dev", "-o",
		"jsonpath={.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.location.default}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "Internal" {
		return fmt.Errorf("upgraded weftservers CRD location default = %q, want Internal", out)
	}
	return e2ekit.WaitFor(ctx, time.Minute, "legacy WeftServer reads back the new default", func() (bool, string, error) {
		loc, found, uid, err := s.legacyLocation(ctx)
		if err != nil {
			return false, "", err
		}
		if uid != s.legacyUID {
			return false, "", fmt.Errorf("WeftServer %s was recreated across the upgrade (uid %s -> %s)", legacyCR, s.legacyUID, uid)
		}
		return found && loc == "Internal", fmt.Sprintf("location=%q found=%v", loc, found), nil
	})
}

func (s *chart) vpaAttachedOnUpgrade(ctx context.Context) error {
	v, err := s.operatorVPA(ctx)
	if err != nil {
		return fmt.Errorf("operator VPA after the upgrade: %w", err)
	}
	mode, _, _ := unstructured.NestedString(v.Object, "spec", "updatePolicy", "updateMode")
	target, _, _ := unstructured.NestedString(v.Object, "spec", "targetRef", "name")
	pols, _, _ := unstructured.NestedSlice(v.Object, "spec", "resourcePolicy", "containerPolicies")
	log.Printf("operator VPA: updateMode=%s target=%s policies=%v", mode, target, pols)
	if mode != "Off" || target != "weft-operator" || len(pols) != 1 {
		return fmt.Errorf("operator VPA updateMode=%q target=%q policies=%v", mode, target, pols)
	}
	if res, _, _ := unstructured.NestedStringSlice(pols[0].(map[string]any), "controlledResources"); !slices.Equal(res, []string{"memory"}) {
		return fmt.Errorf("operator VPA controlledResources=%v, want [memory]", res)
	}
	for _, d := range vpaDeployments {
		dep, err := deployment(ctx, s.k, ns, d)
		if err != nil {
			return fmt.Errorf("%s after the upgrade: %w", d, err)
		}
		if dep.UID != s.vpaUIDs[d] {
			return fmt.Errorf("%s was recreated by the upgrade", d)
		}
	}
	return nil
}

func (s *chart) extraEnv(ctx context.Context) error {
	k := s.k
	d, err := deployment(ctx, k, ns, "weft-operator")
	if err != nil {
		return err
	}
	found := false
	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		found = found || (e.Name == envName && e.Value == envValue)
	}
	if !found {
		return fmt.Errorf("operator Deployment env lacks %s=%s", envName, envValue)
	}
	want := envName + "=" + envValue
	return e2ekit.WaitFor(ctx, 4*time.Minute, "running operator process has "+want, func() (bool, string, error) {
		p, err := operatorPod(ctx, k)
		if err != nil || !podReady(p) {
			return false, fmt.Sprintf("pod: %v", err), nil
		}
		pid, err := e2ekit.ContainerPID(ctx, "weft-operator")
		if err != nil {
			return false, err.Error(), nil
		}
		env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			return false, err.Error(), nil
		}
		return slices.Contains(strings.Split(string(env), "\x00"), want), fmt.Sprintf("pid %d", pid), nil
	})
}

// uninstall documents what `helm uninstall` does with the chart defaults.
// Everything the release templated goes, CRDs (and so CRs) included, except
// weftlocaldnsreflectors: the operator's cluster-default reflector carries
// the weft.aquaduct.dev/cleanup-coredns finalizer, the operator is deleted
// in the same pass, and nothing is left to remove it, so that CRD sits in
// Terminating for good. (helm v4 --wait then reports every resource as
// "still exists ... status: Unknown"; hence no --wait here.) The step
// asserts that, then applies the manual recovery so the VM can reinstall.
func (s *chart) uninstall(ctx context.Context) error {
	k := s.k
	if _, err := helm(ctx, k, "uninstall", release, "--namespace", ns); err != nil {
		return err
	}
	gone := func(what string, check func() (bool, string, error)) error {
		return e2ekit.WaitFor(ctx, 2*time.Minute, what+" gone", check)
	}
	crdGone := func(c string) func() (bool, string, error) {
		return func() (bool, string, error) {
			ok, err := s.crdExists(ctx, c)
			return !ok, fmt.Sprintf("exists=%v", ok), err
		}
	}
	if err := gone("operator Deployment", k.Gone(&appsv1.Deployment{}, ns, "weft-operator")); err != nil {
		return err
	}
	for _, d := range vpaDeployments {
		if err := gone(d, k.Gone(&appsv1.Deployment{}, ns, d)); err != nil {
			return err
		}
	}
	// The CR goes with its CRD (then the kind itself is unknown).
	if err := gone("legacy WeftServer", func() (bool, string, error) {
		err := k.C.Get(ctx, types.NamespacedName{Namespace: appNS, Name: legacyCR}, &weftv1alpha1.WeftServer{})
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return true, "gone", nil
		}
		return false, fmt.Sprintf("get: %v", err), nil
	}); err != nil {
		return err
	}
	for _, c := range append(slices.Clone(weftCRDs), vpaCRD) {
		if c == reflectorCRD {
			continue
		}
		if err := gone("CRD "+c, crdGone(c)); err != nil {
			return err
		}
	}
	if err := gone("GatewayClass", k.Gone(&gatewayv1.GatewayClass{}, "", "weft-gateway-class")); err != nil {
		return err
	}

	// Known defect: the reflector CRD is stuck behind the default
	// reflector's finalizer.
	stuck := func() (bool, string, error) {
		r := &weftv1alpha1.WeftLocalDNSReflector{}
		if err := k.C.Get(ctx, types.NamespacedName{Name: "cluster-default"}, r); err != nil {
			return false, "reflector: " + err.Error(), nil
		}
		crd, err := s.dyn.Resource(crdGVR).Get(ctx, reflectorCRD, metav1.GetOptions{})
		if err != nil {
			return false, "crd: " + err.Error(), nil
		}
		ok := crd.GetDeletionTimestamp() != nil && r.DeletionTimestamp != nil && slices.Contains(r.Finalizers, reflectorFinalizer)
		return ok, fmt.Sprintf("crd deleting=%v reflector deleting=%v finalizers=%v", crd.GetDeletionTimestamp() != nil, r.DeletionTimestamp != nil, r.Finalizers), nil
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "reflector CRD Terminating behind the cluster-default finalizer", stuck); err != nil {
		return err
	}
	if err := e2ekit.Holds(ctx, 30*time.Second, "reflector CRD stays stuck with no operator to finalize", stuck); err != nil {
		return err
	}
	log.Printf("KNOWN DEFECT: helm uninstall leaves CRD %s Terminating: WeftLocalDNSReflector cluster-default keeps finalizer %s after the operator is gone", reflectorCRD, reflectorFinalizer)

	// Documented, not asserted: what the operator created itself outlives
	// the release.
	for _, args := range [][]string{
		{"get", "mutatingwebhookconfiguration,validatingwebhookconfiguration"},
		{"get", "all,secret,configmap", "-n", ns},
		{"get", "configmap", "-n", "kube-system", "coredns-custom", "-o", "yaml"},
	} {
		out, _ := k.Kubectl(ctx, args...)
		log.Printf("after uninstall, kubectl %s:\n%s", strings.Join(args, " "), out)
	}

	// Manual recovery: drop the finalizer (the CoreDNS override it guards
	// is left behind) and the CRD finishes deleting.
	if _, err := k.Kubectl(ctx, "patch", "weftlocaldnsreflector", "cluster-default", "--type=merge", "-p", `{"metadata":{"finalizers":null}}`); err != nil {
		return err
	}
	return gone("CRD "+reflectorCRD+" after removing the finalizer", crdGone(reflectorCRD))
}

func (s *chart) takeOwnershipVPAFalse(ctx context.Context) error {
	k := s.k
	if _, err := k.Kubectl(ctx, "apply", "-f", oldCRDs); err != nil {
		return err
	}
	// --take-ownership skips the ownership check, but kubectl's
	// client-side-apply manager still owns the fields helm v4's server-side
	// apply changes.
	out, err := helm(ctx, k, "install", release, chartNew, "--namespace", ns, "--values", chartValues,
		"--take-ownership", "--set", "verticalPodAutoscaler.installController=false")
	if err == nil {
		return fmt.Errorf("--take-ownership alone took over CRDs owned by kubectl-client-side-apply:\n%s", out)
	}
	if !strings.Contains(out, `conflict with "kubectl-client-side-apply"`) {
		return fmt.Errorf("unexpected --take-ownership failure:\n%s", out)
	}
	log.Printf("EXPECTED --take-ownership failure without --force-conflicts:\n%s", out)
	// The failed install left a failed release; upgrade --install replaces it.
	if _, err := s.helmUpgrade(ctx, chartNew, "--take-ownership", "--force-conflicts",
		"--set", "verticalPodAutoscaler.installController=false"); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 4*time.Minute, "operator available", k.DeploymentAvailable(ns, "weft-operator")); err != nil {
		return err
	}
	out, err = k.Kubectl(ctx, "get", "crd", "weftservers.weft.aquaduct.dev", "-o",
		`jsonpath={.metadata.labels.app\.kubernetes\.io/managed-by} {.metadata.annotations.meta\.helm\.sh/release-name}`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "Helm "+release {
		return fmt.Errorf("taken-over CRD ownership = %q", out)
	}
	return s.noVPAObjects(ctx, true)
}

// noVPAObjects asserts the release installed nothing VPA; crdAbsent also
// requires the VPA CRD (and so the operator's VPA object) to be absent.
func (s *chart) noVPAObjects(ctx context.Context, crdAbsent bool) error {
	manifest, err := helm(ctx, s.k, "get", "manifest", release, "--namespace", ns)
	if err != nil {
		return err
	}
	for _, needle := range []string{"name: vpa-", "name: system:vpa-", "name: system:evictioner", "name: system:metrics-reader", "name: " + vpaCRD} {
		if strings.Contains(manifest, needle) {
			return fmt.Errorf("release manifest contains %q", needle)
		}
	}
	dl := &appsv1.DeploymentList{}
	if err := s.k.C.List(ctx, dl); err != nil {
		return err
	}
	for _, d := range dl.Items {
		if strings.HasPrefix(d.Name, "vpa-") {
			return fmt.Errorf("deployment %s/%s exists", d.Namespace, d.Name)
		}
	}
	crs, err := s.k.CS.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, r := range crs.Items {
		if strings.HasPrefix(r.Name, "system:vpa-") || r.Name == "system:evictioner" {
			return fmt.Errorf("ClusterRole %s exists", r.Name)
		}
	}
	if crdAbsent {
		if ok, err := s.crdExists(ctx, vpaCRD); err != nil || ok {
			return fmt.Errorf("VPA CRD exists=%v err=%v", ok, err)
		}
	}
	return nil
}

func (s *chart) vpaAutoForeignCRD(ctx context.Context) error {
	k := s.k
	// Someone else's VPA install: the same upstream CRDs, applied outside
	// this release.
	crd, err := helm(ctx, k, "template", "other", chartNew, "--namespace", "other-vpa",
		"--set", "verticalPodAutoscaler.installController=true", "--show-only", "templates/vpa-crd.yaml")
	if err != nil {
		return err
	}
	if err := os.WriteFile("/tmp/foreign-vpa-crd.yaml", []byte(crd), 0o644); err != nil {
		return err
	}
	if _, err := k.Kubectl(ctx, "apply", "--server-side", "-f", "/tmp/foreign-vpa-crd.yaml"); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "VPA API served", func() (bool, string, error) {
		_, err := k.CS.Discovery().ServerResourcesForGroupVersion("autoscaling.k8s.io/v1")
		return err == nil, fmt.Sprintf("discovery: %v", err), nil
	}); err != nil {
		return err
	}
	if _, err := s.helmUpgrade(ctx, chartNew, "--set", "verticalPodAutoscaler.installController=auto"); err != nil {
		return err
	}
	if err := s.noVPAObjects(ctx, false); err != nil {
		return err
	}
	// The foreign CRD is still not the release's.
	out, err := k.Kubectl(ctx, "get", "crd", vpaCRD, "-o", `jsonpath={.metadata.annotations.meta\.helm\.sh/release-name}`)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("foreign VPA CRD now annotated for release %q", out)
	}
	_, err = s.operatorVPA(ctx)
	return err
}

func (s *chart) diagnostics(ctx context.Context) {
	for _, args := range [][]string{
		{"get", "crd"},
		{"get", "deploy,vpa", "-A"},
		{"get", "secret", "-n", ns, "-l", "owner=helm"},
	} {
		out, _ := s.k.Kubectl(ctx, args...)
		log.Printf("kubectl %s:\n%s", strings.Join(args, " "), out)
	}
	out, _ := helm(ctx, s.k, "list", "--all", "--all-namespaces")
	log.Print(out)
}
