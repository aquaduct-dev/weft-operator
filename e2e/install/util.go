package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	ns        = e2ekit.OperatorNS
	appNS     = "e2e-app"
	opLabel   = "app.kubernetes.io/name=weft-operator"
	leaseName = "weft-operator.aquaduct.dev"
	helmBin   = "/usr/local/bin/helm"
)

// applyYAML writes doc to a temp file and server-side applies it.
func applyYAML(ctx context.Context, k *e2ekit.Kit, name, doc string) error {
	dir, err := os.MkdirTemp("", "e2e-yaml")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		return err
	}
	// No -n: the documents name their own namespaces.
	out, err := k.Kubectl(ctx, "apply", "--server-side", "--force-conflicts", "-f", p)
	log.Printf("kubectl apply -f %s:\n%s", name, out)
	return err
}

// operatorPod returns the single non-terminating operator pod.
func operatorPod(ctx context.Context, k *e2ekit.Kit) (*corev1.Pod, error) {
	pl, err := k.CS.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: opLabel})
	if err != nil {
		return nil, err
	}
	var live []corev1.Pod
	for _, p := range pl.Items {
		if p.DeletionTimestamp == nil {
			live = append(live, p)
		}
	}
	if len(live) != 1 {
		return nil, fmt.Errorf("%d live operator pods", len(live))
	}
	return &live[0], nil
}

func restarts(p *corev1.Pod) int32 {
	var n int32
	for _, cs := range p.Status.ContainerStatuses {
		n += cs.RestartCount
	}
	return n
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// operatorReadyOther waits for a Ready operator pod whose UID differs from
// notUID (pass "" for any) and returns it.
func operatorReadyOther(ctx context.Context, k *e2ekit.Kit, notUID types.UID, timeout time.Duration) (*corev1.Pod, error) {
	var got *corev1.Pod
	err := e2ekit.WaitFor(ctx, timeout, "operator pod Ready", func() (bool, string, error) {
		p, err := operatorPod(ctx, k)
		if err != nil {
			return false, err.Error(), nil
		}
		if p.UID == notUID {
			return false, "still the old pod", nil
		}
		got = p
		return podReady(p), fmt.Sprintf("%s %s ready=%v", p.Name, e2ekit.ContainerState(p), podReady(p)), nil
	})
	return got, err
}

// leaseHolder returns the leader-election Lease's holder and renew time.
func leaseHolder(ctx context.Context, k *e2ekit.Kit) (string, time.Time, error) {
	l, err := k.CS.CoordinationV1().Leases(ns).Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		return "", time.Time{}, err
	}
	h, t := "", time.Time{}
	if l.Spec.HolderIdentity != nil {
		h = *l.Spec.HolderIdentity
	}
	if l.Spec.RenewTime != nil {
		t = l.Spec.RenewTime.Time
	}
	return h, t, nil
}

// waitLeader waits until pod holds the lease (controller-runtime's identity
// is "<hostname>_<uuid>", and the hostname is the pod name).
func waitLeader(ctx context.Context, k *e2ekit.Kit, pod string, timeout time.Duration) error {
	return e2ekit.WaitFor(ctx, timeout, "lease held by "+pod, func() (bool, string, error) {
		h, _, err := leaseHolder(ctx, k)
		if err != nil {
			return false, err.Error(), nil
		}
		return strings.HasPrefix(h, pod+"_"), "holder=" + h, nil
	})
}

// waitLog waits until the named pod's current log contains every needle.
func waitLog(ctx context.Context, k *e2ekit.Kit, pod string, timeout time.Duration, needles ...string) (string, error) {
	var logs string
	err := e2ekit.WaitFor(ctx, timeout, fmt.Sprintf("operator log has %q", needles), func() (bool, string, error) {
		var err error
		logs, err = k.PodLogs(ctx, ns, pod, "", false)
		if err != nil {
			return false, err.Error(), nil
		}
		for _, n := range needles {
			if !strings.Contains(logs, n) {
				return false, "missing " + n, nil
			}
		}
		return true, "all present", nil
	})
	return logs, err
}

// logLines returns the lines of logs containing every needle.
func logLines(logs string, needles ...string) []string {
	var out []string
outer:
	for _, l := range strings.Split(logs, "\n") {
		for _, n := range needles {
			if !strings.Contains(l, n) {
				continue outer
			}
		}
		out = append(out, l)
	}
	return out
}

// httpGet fetches url from the node.
func httpGet(ctx context.Context, url string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// gatewayTunnel returns the WeftTunnel the gateway reconciler made for
// gateway gw, or nil.
func gatewayTunnel(ctx context.Context, k *e2ekit.Kit, gw string) (*weftv1alpha1.WeftTunnel, error) {
	l := &weftv1alpha1.WeftTunnelList{}
	if err := k.C.List(ctx, l, client.InNamespace(appNS), client.MatchingLabels{"gateway": gw}); err != nil {
		return nil, err
	}
	if len(l.Items) == 0 {
		return nil, nil
	}
	return &l.Items[0], nil
}

// waitGatewayTunnel waits for gw's tunnel with the given src/dst URLs.
func waitGatewayTunnel(ctx context.Context, k *e2ekit.Kit, gw, src, dst string) (*weftv1alpha1.WeftTunnel, error) {
	var t *weftv1alpha1.WeftTunnel
	err := e2ekit.WaitFor(ctx, 2*time.Minute, "WeftTunnel for Gateway "+gw, func() (bool, string, error) {
		var err error
		t, err = gatewayTunnel(ctx, k, gw)
		if err != nil || t == nil {
			return false, "absent", err
		}
		return t.Spec.SrcURL == src && t.Spec.DstURL == dst, fmt.Sprintf("%s src=%s dst=%s", t.Name, t.Spec.SrcURL, t.Spec.DstURL), nil
	})
	return t, err
}

// deployment fetches a Deployment.
func deployment(ctx context.Context, k *e2ekit.Kit, namespace, name string) (*appsv1.Deployment, error) {
	d := &appsv1.Deployment{}
	return d, k.C.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, d)
}

// helm runs the VM's helm against the node's cluster and returns its
// combined output.
func helm(ctx context.Context, k *e2ekit.Kit, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, helmBin, args...)
	cmd.Env = append(os.Environ(),
		"KUBECONFIG="+k.Kubeconfig,
		"HOME=/tmp/helm-home",
		"HELM_CACHE_HOME=/tmp/helm-home/cache",
		"HELM_CONFIG_HOME=/tmp/helm-home/config",
		"HELM_DATA_HOME=/tmp/helm-home/data",
	)
	out, err := cmd.CombinedOutput()
	log.Printf("helm %s:\n%s", strings.Join(args, " "), out)
	if err != nil {
		return string(out), fmt.Errorf("helm %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
