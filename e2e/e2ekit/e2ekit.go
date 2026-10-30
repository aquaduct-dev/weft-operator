// Package e2ekit is the shared guest-side harness for the firecracker k3s
// e2e scenarios under //e2e. A scenario binary runs on the microVM next to
// k3s (as root, on the node), drives the cluster through the API server and
// writes a clusterharness report that firecracker_k3s_test turns into the
// test result.
//
// A scenario is a list of Steps run in order; the first failure stops the
// run (later steps depend on earlier ones) and dumps diagnostics to the
// serial console. Main parses the flags the VM init passes and picks the
// step list by scenario name (the basename of --scenario).
package e2ekit

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/clusterharness"
)

const (
	// OperatorNS is where every scenario installs the operator.
	OperatorNS = "weft-system"
	// Kubectl is the VM's kubectl.
	Kubectl = "/usr/local/bin/kubectl"
	// K3s is the VM's k3s binary (for `k3s crictl`, `k3s ctr`).
	K3s = "/usr/local/bin/k3s"

	PollInterval   = 2 * time.Second
	progressPeriod = 15 * time.Second
)

// Kit is a scenario's handle on the cluster.
type Kit struct {
	Kubeconfig string
	Config     *rest.Config
	C          client.Client
	CS         *kubernetes.Clientset
	Scheme     *runtime.Scheme

	// Diagnostics, if set, is called after the generic dump when a step
	// fails, for scenario-specific state.
	Diagnostics func(ctx context.Context)
}

// New connects to the cluster with a scheme that knows core, weft and
// Gateway API (v1 + v1alpha2) types.
func New(kubeconfig string) (*Kit, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		weftv1alpha1.AddToScheme,
		gatewayv1.Install,
		gatewayv1alpha2.Install,
	} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Kit{Kubeconfig: kubeconfig, Config: cfg, C: c, CS: cs, Scheme: scheme}, nil
}

// Step is one pass condition of a scenario.
type Step struct {
	ID, Label string
	Fn        func(context.Context) error
}

// Scenarios maps a scenario name (the --scenario file's basename without
// .yaml) to a constructor for its steps.
type Scenarios map[string]func(k *Kit) []Step

// Main runs the scenario the VM init asked for and returns the exit code.
// It never returns early without writing a report.
func Main(scenarios Scenarios) int {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	var scenarioPath, reportPath, kubeconfig, namespace string
	var timeout time.Duration
	flag.StringVar(&scenarioPath, "scenario", "", "scenario YAML (applied by the VM init)")
	flag.StringVar(&reportPath, "report-json", "/tmp/report.json", "clusterharness report output")
	flag.StringVar(&kubeconfig, "kubeconfig", "/etc/rancher/k3s/k3s.yaml", "kubeconfig")
	flag.StringVar(&namespace, "namespace", "default", "unused; scenarios use their own namespaces")
	flag.DurationVar(&timeout, "timeout", 25*time.Minute, "whole-scenario deadline")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	name := strings.TrimSuffix(filepath.Base(scenarioPath), ".yaml")
	report := &clusterharness.Report{Scenario: name}
	k, err := New(kubeconfig)
	steps := scenarios[name]
	switch {
	case err != nil:
		report.PassConditions = []clusterharness.ReportPC{{ID: "e2e.setup", Label: "kube-client", Error: err.Error()}}
	case steps == nil:
		report.PassConditions = []clusterharness.ReportPC{{ID: "e2e.setup", Label: "scenario", Error: fmt.Sprintf("unknown scenario %q", name)}}
	default:
		report.PassConditions = k.Run(ctx, steps(k))
	}
	if err := clusterharness.WriteReportJSON(reportPath, report); err != nil {
		log.Printf("write report: %v", err)
		return 1
	}
	for _, pc := range report.PassConditions {
		if !pc.Passed {
			fmt.Printf("FAILED %s (%s): %s\n", pc.ID, pc.Label, pc.Error)
			return 1
		}
	}
	return 0
}

// Run executes steps in order; a failed step stops the run and dumps
// diagnostics.
func (k *Kit) Run(ctx context.Context, steps []Step) []clusterharness.ReportPC {
	var pcs []clusterharness.ReportPC
	failed := ""
	for _, st := range steps {
		pc := clusterharness.ReportPC{ID: st.ID, Label: st.Label}
		if failed != "" {
			pc.Error = "not run: " + failed + " failed"
			pcs = append(pcs, pc)
			continue
		}
		log.Printf("=== %s: %s", st.ID, st.Label)
		start := time.Now()
		if err := st.Fn(ctx); err != nil {
			pc.Error = err.Error()
			failed = st.ID
			log.Printf("--- FAIL %s (%s): %v", st.ID, time.Since(start).Round(time.Second), err)
			k.DumpDiagnostics(context.Background())
		} else {
			pc.Passed = true
			log.Printf("--- PASS %s (%s)", st.ID, time.Since(start).Round(time.Second))
		}
		pcs = append(pcs, pc)
	}
	return pcs
}

// WaitFor polls check until it reports done, an error, or timeout. check
// returns a short state description that is logged periodically and
// included in the timeout error.
func WaitFor(ctx context.Context, timeout time.Duration, what string, check func() (bool, string, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	lastLog := time.Time{}
	state := ""
	for {
		done, s, err := check()
		state = s
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if done {
			log.Printf("ok: %s (%s, %s)", what, state, time.Since(start).Round(time.Second))
			return nil
		}
		if time.Since(lastLog) > progressPeriod {
			log.Printf("waiting: %s (%s, %s)", what, state, time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: timed out after %s; last state: %s", what, timeout, state)
		case <-time.After(PollInterval):
		}
	}
}

// Holds checks that cond stays true for the whole duration (polling), for
// asserting that something does NOT happen (no crash, no write).
func Holds(ctx context.Context, d time.Duration, what string, cond func() (bool, string, error)) error {
	deadline := time.Now().Add(d)
	for {
		ok, state, err := cond()
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if !ok {
			return fmt.Errorf("%s: stopped holding: %s", what, state)
		}
		if time.Now().After(deadline) {
			log.Printf("ok: %s held for %s (%s)", what, d, state)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}

// Kubectl runs the VM's kubectl and returns its combined output.
func (k *Kit) Kubectl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, Kubectl, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+k.Kubeconfig)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// Apply applies a manifest file into namespace, server-side so large CRDs
// do not overflow the last-applied annotation.
func (k *Kit) Apply(ctx context.Context, path, namespace string) error {
	out, err := k.Kubectl(ctx, "apply", "--server-side", "--force-conflicts", "-n", namespace, "-f", path)
	log.Printf("kubectl apply -n %s -f %s:\n%s", namespace, path, out)
	return err
}

// Delete deletes what a manifest file describes, ignoring missing objects.
func (k *Kit) Delete(ctx context.Context, path, namespace string) error {
	out, err := k.Kubectl(ctx, "delete", "--ignore-not-found", "--wait=true", "-n", namespace, "-f", path)
	log.Printf("kubectl delete -n %s -f %s:\n%s", namespace, path, out)
	return err
}

// OperatorInstall describes the files InstallOperator applies.
type OperatorInstall struct {
	// CRDs is a Gateway API install manifest; empty skips it.
	CRDs string
	// Manifest is the rendered operator chart, applied into OperatorNS.
	Manifest string
	// SkipWebhookCheck skips waiting for the trust webhook registration.
	SkipWebhookCheck bool
}

// InstallOperator waits for CoreDNS, applies the Gateway API CRDs and the
// operator, and waits for the operator to be available and its trust
// webhook registered.
func (k *Kit) InstallOperator(ctx context.Context, in OperatorInstall) error {
	if err := WaitFor(ctx, 3*time.Minute, "CoreDNS available", k.DeploymentAvailable("kube-system", "coredns")); err != nil {
		return err
	}
	if in.CRDs != "" {
		if err := k.Apply(ctx, in.CRDs, "default"); err != nil {
			return err
		}
	}
	if err := k.Apply(ctx, in.Manifest, OperatorNS); err != nil {
		return err
	}
	if err := WaitFor(ctx, 3*time.Minute, "operator available", k.DeploymentAvailable(OperatorNS, "weft-operator")); err != nil {
		return err
	}
	if in.SkipWebhookCheck {
		return nil
	}
	return WaitFor(ctx, time.Minute, "trust webhook registered", func() (bool, string, error) {
		mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
		err := k.C.Get(ctx, types.NamespacedName{Name: "weft-operator-trust-injector"}, mwc)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		return err == nil && len(mwc.Webhooks) == 1 && len(mwc.Webhooks[0].ClientConfig.CABundle) > 0, "present", err
	})
}

// DeploymentAvailable is a WaitFor check for a Deployment with at least one
// available replica.
func (k *Kit) DeploymentAvailable(ns, name string) func() (bool, string, error) {
	return func() (bool, string, error) {
		d := &appsv1.Deployment{}
		err := k.C.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, d)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		if err != nil {
			return false, "", err
		}
		return d.Status.AvailableReplicas > 0, fmt.Sprintf("available=%d", d.Status.AvailableReplicas), nil
	}
}

// Gone is a WaitFor check that obj (by key) no longer exists.
func (k *Kit) Gone(obj client.Object, ns, name string) func() (bool, string, error) {
	return func() (bool, string, error) {
		err := k.C.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, obj)
		if apierrors.IsNotFound(err) {
			return true, "gone", nil
		}
		return false, "present", client.IgnoreNotFound(err)
	}
}

// Condition returns the condition of type t, or nil.
func Condition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

// CondString renders a condition for state messages.
func CondString(c *metav1.Condition) string {
	if c == nil {
		return "<none>"
	}
	return fmt.Sprintf("%s=%s/%s", c.Type, c.Status, c.Reason)
}

// PodLogs returns a container's log (the previous instance's if previous).
func (k *Kit) PodLogs(ctx context.Context, ns, name, container string, previous bool) (string, error) {
	rc, err := k.CS.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{Container: container, Previous: previous}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	return string(b), err
}

// OperatorLogs returns the current operator pod's log.
func (k *Kit) OperatorLogs(ctx context.Context) (string, error) {
	pl, err := k.CS.CoreV1().Pods(OperatorNS).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=weft-operator"})
	if err != nil {
		return "", err
	}
	if len(pl.Items) == 0 {
		return "", fmt.Errorf("no operator pod")
	}
	return k.PodLogs(ctx, OperatorNS, pl.Items[0].Name, "", false)
}

// RunPod runs pod to completion (RestartPolicy forced to Never), replacing
// any earlier pod of the same name, and returns its logs. It fails if the
// pod does not succeed.
func (k *Kit) RunPod(ctx context.Context, pod *corev1.Pod, timeout time.Duration) (string, error) {
	pod.Spec.RestartPolicy = corev1.RestartPolicyNever
	key := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
	_ = k.C.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name}})
	if err := WaitFor(ctx, time.Minute, "old pod "+pod.Name+" gone", k.Gone(&corev1.Pod{}, pod.Namespace, pod.Name)); err != nil {
		return "", err
	}
	if err := k.C.Create(ctx, pod); err != nil {
		return "", fmt.Errorf("create pod %s: %w", pod.Name, err)
	}
	var phase corev1.PodPhase
	err := WaitFor(ctx, timeout, "pod "+pod.Name+" finished", func() (bool, string, error) {
		p := &corev1.Pod{}
		if err := k.C.Get(ctx, key, p); err != nil {
			return false, "", err
		}
		phase = p.Status.Phase
		return phase == corev1.PodSucceeded || phase == corev1.PodFailed, string(phase) + " " + ContainerState(p), nil
	})
	logs, lerr := k.PodLogs(ctx, pod.Namespace, pod.Name, "", false)
	log.Printf("pod %s logs:\n%s", pod.Name, logs)
	if err != nil {
		return logs, err
	}
	if lerr != nil {
		return logs, lerr
	}
	if phase != corev1.PodSucceeded {
		return logs, fmt.Errorf("pod %s ended %s", pod.Name, phase)
	}
	return logs, nil
}

// ContainerState summarizes the first container's state.
func ContainerState(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		switch {
		case cs.State.Waiting != nil:
			return cs.State.Waiting.Reason + ": " + cs.State.Waiting.Message
		case cs.State.Terminated != nil:
			return fmt.Sprintf("terminated %d %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
		case cs.State.Running != nil:
			return fmt.Sprintf("running (restarts=%d)", cs.RestartCount)
		}
	}
	return "no container status"
}

// NodeIP is the single node's InternalIP (the VM's address; hostNetwork
// pods listen here).
func (k *Kit) NodeIP(ctx context.Context) (string, error) {
	nodes := &corev1.NodeList{}
	if err := k.C.List(ctx, nodes); err != nil {
		return "", err
	}
	for _, n := range nodes.Items {
		for _, a := range n.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				return a.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no node InternalIP")
}

// ContainerPID returns the host PID of the running container named name
// (as crictl knows it), so a scenario on the node can inspect its /proc.
func ContainerPID(ctx context.Context, name string) (int, error) {
	out, err := exec.CommandContext(ctx, K3s, "crictl", "ps", "--state", "running", "--name", "^"+name+"$", "-q").Output()
	if err != nil {
		return 0, fmt.Errorf("crictl ps: %w", err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return 0, fmt.Errorf("no running container %q", name)
	}
	out, err = exec.CommandContext(ctx, K3s, "crictl", "inspect", "-o", "go-template", "--template", "{{.info.pid}}", ids[0]).Output()
	if err != nil {
		return 0, fmt.Errorf("crictl inspect: %w", err)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// EstablishedConns counts the ESTABLISHED TCP connections in pid's network
// namespace whose remote port is remotePort (0 = any).
func EstablishedConns(pid, remotePort int) (int, error) {
	n := 0
	for _, f := range []string{"tcp", "tcp6"} {
		fh, err := os.Open(fmt.Sprintf("/proc/%d/net/%s", pid, f))
		if err != nil {
			return 0, err
		}
		c, err := countEstablished(fh, remotePort)
		fh.Close()
		if err != nil {
			return 0, err
		}
		n += c
	}
	return n, nil
}

// countEstablished parses a /proc/net/tcp{,6} table.
func countEstablished(r io.Reader, remotePort int) (int, error) {
	n := 0
	sc := bufio.NewScanner(r)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[3] != "01" { // 01 = ESTABLISHED
			continue
		}
		if remotePort != 0 {
			i := strings.LastIndex(fields[2], ":")
			if i < 0 {
				continue
			}
			b, err := hex.DecodeString(fields[2][i+1:])
			if err != nil || len(b) != 2 || int(b[0])<<8|int(b[1]) != remotePort {
				continue
			}
		}
		n++
	}
	return n, sc.Err()
}

// DumpDiagnostics prints what is needed to debug a failed step from the
// serial console alone: every pod, warning events, and the tail of the
// operator's and every non-Running pod's logs.
func (k *Kit) DumpDiagnostics(ctx context.Context) {
	log.Printf("##### diagnostics")
	pods := &corev1.PodList{}
	if err := k.C.List(ctx, pods); err == nil {
		for _, p := range pods.Items {
			log.Printf("pod %s/%s %s ip=%s %s", p.Namespace, p.Name, p.Status.Phase, p.Status.PodIP, ContainerState(&p))
			if p.Labels["app.kubernetes.io/name"] == "weft-operator" || p.Status.Phase != corev1.PodRunning || restarted(&p) {
				for _, c := range p.Spec.Containers {
					logs, _ := k.PodLogs(ctx, p.Namespace, p.Name, c.Name, false)
					log.Printf("--- logs %s/%s[%s] (tail):\n%s", p.Namespace, p.Name, c.Name, Tail(logs, 60))
				}
			}
		}
	}
	events := &corev1.EventList{}
	if err := k.C.List(ctx, events); err == nil {
		for _, e := range events.Items {
			if e.Type == corev1.EventTypeWarning {
				log.Printf("event %s/%s %s: %s", e.InvolvedObject.Namespace, e.InvolvedObject.Name, e.Reason, e.Message)
			}
		}
	}
	if k.Diagnostics != nil {
		k.Diagnostics(ctx)
	}
}

func restarted(p *corev1.Pod) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.RestartCount > 0 {
			return true
		}
	}
	return false
}

// Tail returns the last n lines of s.
func Tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
