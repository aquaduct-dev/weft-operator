// prober_scenario.go — the "prober" scenario: the NodeProber's success
// path. It runs in its own VM because an auto-created weft-system server
// becomes a target of every tunnel with empty targetServers.
//
// `weft probe` (no args) discovers the node's public IPv4 from its
// interfaces and HEADs http://<ip>:80/.well-known/acme-challenge/ (dial
// pinned to <ip>:80), passing on a 403/404. The VM has only RFC1918
// addresses, so the scenario gives the node a documentation address
// (198.51.100.10, TEST-NET-2, not "private" to Go's netip) on lo, and runs a
// hostNetwork stand-in on :80 that answers 404 like autocert does for an
// unknown token. Both are set up before the operator starts, because the
// prober runs at startup (then every 3h).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	publicIP   = "198.51.100.10"
	nodeServer = "node-server0"
)

type proberScenario struct {
	k *e2ekit.Kit
}

func proberSteps(k *e2ekit.Kit) []e2ekit.Step {
	s := &proberScenario{k: k}
	k.Diagnostics = func(ctx context.Context) { logPods(ctx, k, e2ekit.OperatorNS, log.Printf) }
	return []e2ekit.Step{
		{ID: "prober.node-setup", Label: "node has a public-looking IPv4 and an ACME-challenge stand-in on :80", Fn: s.nodeSetup},
		{ID: "prober.install", Label: "operator installed from the chart", Fn: s.install},
		{ID: "prober.job-succeeds", Label: "the startup probe Job (hostNetwork `weft probe`, image :e2e) succeeds", Fn: s.jobSucceeds},
		{ID: "prober.server-created", Label: "a node-<node> WeftServer is created with the node labels and the probed public IP in its connectionString", Fn: s.serverCreated},
		{ID: "prober.server-available", Label: "the auto-created server runs and goes Available", Fn: s.serverAvailable},
		{ID: "prober.restart-idempotent", Label: "after an operator restart the existing node server is kept, no new probe runs, and a server for a missing node is deleted", Fn: s.restart},
	}
}

func (s *proberScenario) nodeSetup(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "ip", "addr", "add", publicIP+"/32", "dev", "lo").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "exists") {
		return fmt.Errorf("ip addr add: %v: %s", err, out)
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "CoreDNS available", s.k.DeploymentAvailable("kube-system", "coredns")); err != nil {
		return err
	}
	// weft-system exists (00-namespaces.yaml); the stand-in lives there too.
	if err := s.k.C.Create(ctx, selfDeployment(e2ekit.OperatorNS, "acme-standin", true, "acme-standin", "--listen=:80")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "acme-standin available", s.k.DeploymentAvailable(e2ekit.OperatorNS, "acme-standin")); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "challenge path answers 404 on "+publicIP, func() (bool, string, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "http://"+publicIP+"/.well-known/acme-challenge/", nil)
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			return false, err.Error(), nil
		}
		resp.Body.Close()
		return resp.StatusCode == 404, resp.Status, nil
	})
}

func (s *proberScenario) install(ctx context.Context) error {
	return s.k.InstallOperator(ctx, e2ekit.OperatorInstall{CRDs: gatewayAPICRDs, Manifest: operatorManifest})
}

// probeJobs lists the probe Jobs for server0.
func (s *proberScenario) probeJobs(ctx context.Context) ([]batchv1.Job, error) {
	jobs := &batchv1.JobList{}
	if err := s.k.C.List(ctx, jobs, client.InNamespace(e2ekit.OperatorNS), client.MatchingLabels{"app": "weft-probe"}); err != nil {
		return nil, err
	}
	var out []batchv1.Job
	for _, j := range jobs.Items {
		if strings.HasPrefix(j.Name, "weft-probe-server0-") {
			out = append(out, j)
		}
	}
	return out, nil
}

func (s *proberScenario) jobSucceeds(ctx context.Context) error {
	var job batchv1.Job
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "probe Job succeeded", func() (bool, string, error) {
		jobs, err := s.probeJobs(ctx)
		if err != nil || len(jobs) == 0 {
			return false, "no probe job", err
		}
		job = jobs[0]
		if job.Status.Failed > 0 {
			return false, "", fmt.Errorf("probe job %s failed", job.Name)
		}
		return job.Status.Succeeded > 0, fmt.Sprintf("%s succeeded=%d", job.Name, job.Status.Succeeded), nil
	}); err != nil {
		return err
	}
	return checkProbeJob(&job)
}

var nodeConnStr = regexp.MustCompile(`^weft://[0-9a-f]{10}@` + regexp.QuoteMeta(publicIP) + `:9092$`)

func (s *proberScenario) serverCreated(ctx context.Context) error {
	ws := &weftv1alpha1.WeftServer{}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, nodeServer+" created", func() (bool, string, error) {
		gone, state, err := s.k.Gone(ws, e2ekit.OperatorNS, nodeServer)()
		return !gone, state, err
	}); err != nil {
		return err
	}
	var errs []string
	if ws.Labels["weft.aquaduct.dev/auto-created"] != "true" || ws.Labels["weft.aquaduct.dev/node"] != "server0" {
		errs = append(errs, fmt.Sprintf("labels %v", ws.Labels))
	}
	if !nodeConnStr.MatchString(ws.Spec.ConnectionString) {
		errs = append(errs, fmt.Sprintf("connectionString %q, want weft://<10 hex>@%s:9092", ws.Spec.ConnectionString, publicIP))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s: %s", nodeServer, strings.Join(errs, "; "))
	}
	return nil
}

func (s *proberScenario) serverAvailable(ctx context.Context) error {
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, nodeServer+"-server available", s.k.DeploymentAvailable(e2ekit.OperatorNS, nodeServer+"-server")); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, nodeServer+" Available=True", serverAvailable(s.k, e2ekit.OperatorNS, nodeServer))
}

func (s *proberScenario) restart(ctx context.Context) error {
	// A server labelled for a node that does not exist; External so it
	// needs no workloads.
	ghost := weftServer(e2ekit.OperatorNS, "node-ghost", "weft://ghostsecret@192.0.2.1:9092")
	ghost.Labels = map[string]string{"weft.aquaduct.dev/auto-created": "true", "weft.aquaduct.dev/node": "ghost"}
	ghost.Spec.Location = weftv1alpha1.WeftServerLocationExternal
	if err := s.k.C.Create(ctx, ghost); err != nil {
		return err
	}
	before := &weftv1alpha1.WeftServer{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: nodeServer}, before); err != nil {
		return err
	}
	restartedAt := metav1.Now()
	pods := &corev1.PodList{}
	if err := s.k.C.List(ctx, pods, client.InNamespace(e2ekit.OperatorNS), client.MatchingLabels{"app.kubernetes.io/name": "weft-operator"}); err != nil {
		return err
	}
	for i := range pods.Items {
		if err := s.k.C.Delete(ctx, &pods.Items[i]); err != nil {
			return err
		}
	}
	// The cleanup pass runs right after the (skipped) probe at startup.
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "node-ghost deleted by the restarted operator", s.k.Gone(&weftv1alpha1.WeftServer{}, e2ekit.OperatorNS, "node-ghost")); err != nil {
		return err
	}
	return e2ekit.Holds(ctx, 15*time.Second, "node server kept, no new probe Job", func() (bool, string, error) {
		ws := &weftv1alpha1.WeftServer{}
		if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: e2ekit.OperatorNS, Name: nodeServer}, ws); err != nil {
			return false, err.Error(), nil
		}
		if ws.UID != before.UID || ws.Spec.ConnectionString != before.Spec.ConnectionString {
			return false, "node server was replaced", nil
		}
		jobs, err := s.probeJobs(ctx)
		if err != nil {
			return false, "", err
		}
		for _, j := range jobs {
			if !j.CreationTimestamp.Before(&restartedAt) {
				return false, "new probe job " + j.Name, nil
			}
		}
		return true, "kept", nil
	})
}
