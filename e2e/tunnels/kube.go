// kube.go — object builders and API-state checks shared by the tunnels and
// prober scenarios.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	// Images preloaded into the VM.
	weftImage = "ghcr.io/aquaduct-dev/weft:e2e"
	selfImage = "ghcr.io/aquaduct-dev/weft-operator-e2e-tunnels:e2e"

	operatorManifest = "/etc/e2e/weft-operator.yaml"
	gatewayAPICRDs   = "/etc/e2e/gateway-api-crds.yaml"
)

// tunnelName is the operator's name for a Gateway-made WeftTunnel
// (weftgateway: gw-<gateway>-<sha256(gateway-route-dstURL)[:8]>).
func tunnelName(gateway, route, dstURL string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%s", gateway, route, dstURL)))
	return fmt.Sprintf("gw-%s-%s", gateway, hex.EncodeToString(h[:])[:8])
}

// tunnelDeploymentName is the WeftTunnel controller's Deployment name for
// one (tunnel, server) pair.
func tunnelDeploymentName(tunnel, server string) string {
	return fmt.Sprintf("tunnel-%s-to-%s", tunnel, server)
}

func weftServer(ns, name, connStr string) *weftv1alpha1.WeftServer {
	return &weftv1alpha1.WeftServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       weftv1alpha1.WeftServerSpec{ConnectionString: connStr},
	}
}

func weftTunnel(ns, name, src, dst string, targets ...string) *weftv1alpha1.WeftTunnel {
	return &weftv1alpha1.WeftTunnel{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       weftv1alpha1.WeftTunnelSpec{SrcURL: src, DstURL: dst, TargetServers: targets},
	}
}

// selfDeployment runs this binary in mode args[0] as a one-replica
// Deployment.
func selfDeployment(ns, name string, hostNetwork bool, args ...string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: e2ekit.Ptr(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					HostNetwork:                  hostNetwork,
					AutomountServiceAccountToken: e2ekit.Ptr(false),
					Containers: []corev1.Container{{
						Name:            name,
						Image:           selfImage,
						ImagePullPolicy: corev1.PullNever,
						Args:            args,
					}},
				},
			},
		},
	}
}

// echoService fronts the echo backend: HTTP on 80, TCP echo on 9000, UDP
// echo on 9001.
func echoService(ns string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "echo"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "echo"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
				{Name: "tcp", Port: 9000, TargetPort: intstr.FromInt(9000), Protocol: corev1.ProtocolTCP},
				{Name: "udp", Port: 9001, TargetPort: intstr.FromInt(9001), Protocol: corev1.ProtocolUDP},
			},
		},
	}
}

// checkTunnelDeployment asserts the Deployment the WeftTunnel controller
// made for (tunnel, server) runs `weft tunnel` with exactly these args.
func checkTunnelDeployment(ctx context.Context, k *e2ekit.Kit, ns, tunnel, server, connStr, src, dst string) error {
	d := &appsv1.Deployment{}
	name := tunnelDeploymentName(tunnel, server)
	if err := k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
		return fmt.Errorf("deployment %s/%s: %w", ns, name, err)
	}
	var errs []string
	if len(d.Spec.Template.Spec.Containers) != 1 {
		return fmt.Errorf("deployment %s: %d containers, want 1", name, len(d.Spec.Template.Spec.Containers))
	}
	c := d.Spec.Template.Spec.Containers[0]
	want := []string{"tunnel", "--tunnel-name=" + tunnel, connStr, src, dst}
	if !slices.Equal(c.Args, want) {
		errs = append(errs, fmt.Sprintf("args %q, want %q", c.Args, want))
	}
	if c.Image != weftImage || c.ImagePullPolicy != corev1.PullIfNotPresent {
		errs = append(errs, fmt.Sprintf("image %s (%s), want %s (IfNotPresent)", c.Image, c.ImagePullPolicy, weftImage))
	}
	if d.Spec.MinReadySeconds != 15 {
		errs = append(errs, fmt.Sprintf("minReadySeconds %d, want 15", d.Spec.MinReadySeconds))
	}
	wantLabels := map[string]string{"app": "weft-tunnel", "tunnel": tunnel, "target": server, "created-by": "weft-operator"}
	for lk, lv := range wantLabels {
		if d.Labels[lk] != lv {
			errs = append(errs, fmt.Sprintf("label %s=%q, want %q", lk, d.Labels[lk], lv))
		}
	}
	if !ownedBy(d.OwnerReferences, "WeftTunnel", tunnel) {
		errs = append(errs, fmt.Sprintf("owner refs %v lack WeftTunnel %s", ownerNames(d.OwnerReferences), tunnel))
	}
	if len(errs) > 0 {
		return fmt.Errorf("deployment %s/%s: %s", ns, name, strings.Join(errs, "; "))
	}
	return nil
}

func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}

func ownerNames(refs []metav1.OwnerReference) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.Kind+"/"+r.Name)
	}
	return out
}

// tunnelDeployments lists the Deployments the WeftTunnel controller made
// for tunnel in ns.
func tunnelDeployments(ctx context.Context, k *e2ekit.Kit, ns, tunnel string) ([]string, error) {
	l := &appsv1.DeploymentList{}
	if err := k.C.List(ctx, l, client.InNamespace(ns), client.MatchingLabels{"tunnel": tunnel, "created-by": "weft-operator"}); err != nil {
		return nil, err
	}
	var names []string
	for _, d := range l.Items {
		names = append(names, d.Name)
	}
	slices.Sort(names)
	return names, nil
}

// tunnelAvailable is a WaitFor check for a WeftTunnel's Available
// condition having status want.
func tunnelAvailable(k *e2ekit.Kit, ns, name string, want metav1.ConditionStatus) func() (bool, string, error) {
	return func() (bool, string, error) {
		t := &weftv1alpha1.WeftTunnel{}
		err := k.C.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, t)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		if err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(t.Status.Conditions, "Available")
		return c != nil && c.Status == want, e2ekit.CondString(c), nil
	}
}

// serverAvailable is a WaitFor check for a WeftServer being
// Available=True/Reconciled, i.e. the operator listed its tunnels.
func serverAvailable(k *e2ekit.Kit, ns, name string) func() (bool, string, error) {
	return func() (bool, string, error) {
		s := &weftv1alpha1.WeftServer{}
		err := k.C.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, s)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		if err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(s.Status.Conditions, "Available")
		state := e2ekit.CondString(c)
		if c != nil {
			state += ": " + c.Message
		}
		return c != nil && c.Status == metav1.ConditionTrue && c.Reason == "Reconciled", state, nil
	}
}

// checkServerWorkloads asserts the Deployment, Service and PVC the
// WeftServer controller made for an internal server.
func checkServerWorkloads(ctx context.Context, k *e2ekit.Kit, ns, name string, wantArgs []string, port int32) error {
	d := &appsv1.Deployment{}
	if err := k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: name + "-server"}, d); err != nil {
		return err
	}
	var errs []string
	ps := d.Spec.Template.Spec
	if !ps.HostNetwork {
		errs = append(errs, "deployment is not hostNetwork")
	}
	if len(ps.Containers) != 1 {
		return fmt.Errorf("deployment %s-server: %d containers", name, len(ps.Containers))
	}
	c := ps.Containers[0]
	if !slices.Equal(c.Args, wantArgs) {
		errs = append(errs, fmt.Sprintf("args %q, want %q", c.Args, wantArgs))
	}
	if c.Image != weftImage || c.ImagePullPolicy != corev1.PullIfNotPresent {
		errs = append(errs, fmt.Sprintf("image %s (%s), want %s (IfNotPresent)", c.Image, c.ImagePullPolicy, weftImage))
	}
	if !ownedBy(d.OwnerReferences, "WeftServer", name) {
		errs = append(errs, "deployment not owned by the WeftServer")
	}
	svc := &corev1.Service{}
	if err := k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: name + "-server"}, svc); err != nil {
		return err
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != port || svc.Spec.Ports[0].TargetPort.IntValue() != int(port) {
		errs = append(errs, fmt.Sprintf("service ports %+v, want %d->%d", svc.Spec.Ports, port, port))
	}
	if svc.Spec.Selector["app"] != "weft-server" || svc.Spec.Selector["instance"] != name {
		errs = append(errs, fmt.Sprintf("service selector %v", svc.Spec.Selector))
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: name + "-certs"}, pvc); err != nil {
		return err
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		errs = append(errs, fmt.Sprintf("pvc %s-certs is %s, want Bound", name, pvc.Status.Phase))
	}
	if !ownedBy(pvc.OwnerReferences, "WeftServer", name) {
		errs = append(errs, "pvc not owned by the WeftServer")
	}
	if len(errs) > 0 {
		return fmt.Errorf("weftserver %s/%s: %s", ns, name, strings.Join(errs, "; "))
	}
	return nil
}

// serverArgs is what the WeftServer controller passes `weft server`.
func serverArgs(secret string, port int) []string {
	args := []string{"server", "--connection-secret=" + secret, "--bind-ip=0.0.0.0"}
	if port != 9092 {
		args = append(args, fmt.Sprintf("--port=%d", port))
	}
	return append(args, "--certs-cache-path=/var/lib/weft/certs")
}

// logPods logs every pod's state in ns (for Diagnostics).
func logPods(ctx context.Context, k *e2ekit.Kit, ns string, logf func(string, ...any)) {
	pods := &corev1.PodList{}
	if err := k.C.List(ctx, pods, client.InNamespace(ns)); err != nil {
		return
	}
	for _, p := range pods.Items {
		logf("pod %s/%s %s %s", p.Namespace, p.Name, p.Status.Phase, e2ekit.ContainerState(&p))
		if strings.HasPrefix(p.Name, "tunnel-") || strings.Contains(p.Name, "-server-") || strings.HasPrefix(p.Name, "weft-probe-") || strings.HasPrefix(p.Name, "echo-") {
			logs, _ := k.PodLogs(ctx, p.Namespace, p.Name, "", false)
			logf("--- logs %s/%s (tail):\n%s", p.Namespace, p.Name, e2ekit.Tail(logs, 25))
		}
	}
}
