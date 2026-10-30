// crds_scenario.go: the "crds" scenario (//e2e/install:crds_test). The
// operator is installed on a cluster with no Gateway API at all, then the
// standard channel, then the experimental one, and its lifecycle (leader
// election, probes, restarts) is checked on the way.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	operatorManifest     = "/etc/e2e/weft-operator.yaml"
	standardCRDs         = "/etc/e2e/gateway-api-standard-crds.yaml"
	experimentalCRDs     = "/etc/e2e/gateway-api-crds.yaml"
	preflightMsg         = "Gateway API CRDs are not installed in this cluster"
	routesIgnoredMsg     = "Gateway API CRD not installed; its routes are ignored until the operator restarts"
	tunnelReconciledMsg  = "Tunnel Deployment reconciled"
	serverName           = "srv"
	serverDeploymentName = serverName + "-server"
)

type crds struct {
	k *e2ekit.Kit
	// The HTTP Gateway's tunnel and its Deployment, for the restart check.
	tunnel, tunnelDep string
}

func crdsSteps(k *e2ekit.Kit) []e2ekit.Step {
	s := &crds{k: k}
	k.Diagnostics = s.diagnostics
	return []e2ekit.Step{
		{ID: "crds.no-gateway-api", Label: "without any Gateway API CRDs the operator exits with the preflight message and crash-loops", Fn: s.noGatewayAPI},
		{ID: "crds.standard-ready", Label: "after the standard-channel CRDs are applied the crash-looping operator becomes Ready", Fn: s.standardReady},
		{ID: "crds.routes-ignored-logged", Label: "on the standard channel the operator logs that TCPRoute, TLSRoute and UDPRoute are ignored", Fn: s.routesIgnoredLogged},
		{ID: "crds.http-gateway-tunnel", Label: "on the standard channel an HTTP Gateway + HTTPRoute still produce a WeftTunnel and its Deployment", Fn: s.httpGatewayTunnel},
		{ID: "crds.standard-stays-ready", Label: "on the standard channel the operator stays Ready with no restart for 3 minutes (past the 2-minute cache sync timeout)", Fn: s.standardStaysReady},
		{ID: "lifecycle.lease", Label: "the leader-election Lease weft-operator.aquaduct.dev is held by the operator pod and renewed", Fn: s.lease},
		{ID: "lifecycle.probes", Label: "/healthz and /readyz on :8081 answer 200 ok", Fn: s.probes},
		{ID: "lifecycle.restart-no-rewrites", Label: "after the operator pod is deleted the new leader leaves existing Deployments alone (no generation bump, no rollout, no tunnel Deployment writes)", Fn: s.restartNoRewrites},
		{ID: "crds.experimental-tcproute", Label: "after the experimental CRDs are applied and the operator restarted, a TCPRoute produces a WeftTunnel", Fn: s.experimentalTCPRoute},
	}
}

func (s *crds) noGatewayAPI(ctx context.Context) error {
	k := s.k
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "CoreDNS available", k.DeploymentAvailable("kube-system", "coredns")); err != nil {
		return err
	}
	if err := k.Apply(ctx, operatorManifest, ns); err != nil {
		return err
	}
	var prev string
	if err := e2ekit.WaitFor(ctx, 4*time.Minute, "operator restarted after the preflight failure", func() (bool, string, error) {
		p, err := operatorPod(ctx, k)
		if err != nil {
			return false, err.Error(), nil
		}
		if restarts(p) < 1 {
			return false, e2ekit.ContainerState(p), nil
		}
		prev, err = k.PodLogs(ctx, ns, p.Name, "", true)
		if err != nil {
			return false, "previous log: " + err.Error(), nil
		}
		return strings.Contains(prev, preflightMsg), e2ekit.ContainerState(p), nil
	}); err != nil {
		return err
	}
	log.Printf("previous operator instance's preflight lines:\n%s", strings.Join(logLines(prev, preflightMsg), "\n"))
	d, err := deployment(ctx, k, ns, "weft-operator")
	if err != nil {
		return err
	}
	if d.Status.AvailableReplicas != 0 {
		return fmt.Errorf("operator Deployment available=%d without Gateway API", d.Status.AvailableReplicas)
	}
	return nil
}

func (s *crds) standardReady(ctx context.Context) error {
	if err := s.k.Apply(ctx, standardCRDs, "default"); err != nil {
		return err
	}
	// The kubelet's crash-loop back-off decides when the next attempt is;
	// it is at most a few doublings in by now.
	if err := e2ekit.WaitFor(ctx, 6*time.Minute, "operator available", s.k.DeploymentAvailable(ns, "weft-operator")); err != nil {
		return err
	}
	p, err := operatorPod(ctx, s.k)
	if err != nil {
		return err
	}
	log.Printf("operator %s ready after %d restarts", p.Name, restarts(p))
	return nil
}

func (s *crds) routesIgnoredLogged(ctx context.Context) error {
	p, err := operatorPod(ctx, s.k)
	if err != nil {
		return err
	}
	logs, err := waitLog(ctx, s.k, p.Name, time.Minute, routesIgnoredMsg)
	if err != nil {
		return err
	}
	lines := logLines(logs, routesIgnoredMsg)
	log.Printf("route-kinds-ignored lines:\n%s", strings.Join(lines, "\n"))
	for _, kind := range []string{"TCPRoute", "TLSRoute", "UDPRoute"} {
		if len(logLines(strings.Join(lines, "\n"), `"kind": "`+kind+`"`)) != 1 {
			return fmt.Errorf("no single %q line for %s", routesIgnoredMsg, kind)
		}
	}
	if n := len(lines); n != 3 {
		return fmt.Errorf("%d route kinds reported ignored, want 3", n)
	}
	return nil
}

func (s *crds) httpGatewayTunnel(ctx context.Context) error {
	k := s.k
	ip, err := k.NodeIP(ctx)
	if err != nil {
		return err
	}
	if err := applyYAML(ctx, k, "http-gateway", fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: weft-gateway-class
spec:
  controllerName: weft.aquaduct.dev/gateway-controller
---
apiVersion: weft.aquaduct.dev/v1alpha1
kind: WeftServer
metadata:
  name: %[2]s
  namespace: %[1]s
spec:
  connectionString: weft://e2esecret@%[3]s:9092
  location: Internal
---
apiVersion: v1
kind: Service
metadata:
  name: backend
  namespace: %[1]s
spec:
  selector: {app: backend}
  ports:
  - {name: http, port: 80}
  - {name: db, port: 5432}
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: web
  namespace: %[1]s
spec:
  gatewayClassName: weft-gateway-class
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: app.e2e.test
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: web
  namespace: %[1]s
spec:
  parentRefs:
  - name: web
  hostnames: [app.e2e.test]
  rules:
  - matches:
    - path: {type: PathPrefix, value: /}
    backendRefs:
    - name: backend
      port: 80
`, appNS, serverName, ip)); err != nil {
		return err
	}
	t, err := waitGatewayTunnel(ctx, k, "web", "http://backend."+appNS+".svc:80", "http://app.e2e.test/")
	if err != nil {
		return err
	}
	s.tunnel = t.Name
	s.tunnelDep = fmt.Sprintf("tunnel-%s-to-%s", t.Name, serverName)
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "tunnel Deployment "+s.tunnelDep, func() (bool, string, error) {
		_, err := deployment(ctx, k, appNS, s.tunnelDep)
		return err == nil, fmt.Sprintf("get: %v", err), nil
	}); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 3*time.Minute, "WeftServer Deployment available", k.DeploymentAvailable(appNS, serverDeploymentName))
}

func (s *crds) standardStaysReady(ctx context.Context) error {
	p0, err := operatorPod(ctx, s.k)
	if err != nil {
		return err
	}
	r0 := restarts(p0)
	return e2ekit.Holds(ctx, 3*time.Minute, "operator stays Ready without restarting", func() (bool, string, error) {
		p, err := operatorPod(ctx, s.k)
		if err != nil {
			return false, err.Error(), nil
		}
		state := fmt.Sprintf("%s %s ready=%v", p.Name, e2ekit.ContainerState(p), podReady(p))
		return p.UID == p0.UID && restarts(p) == r0 && podReady(p), state, nil
	})
}

func (s *crds) lease(ctx context.Context) error {
	p, err := operatorPod(ctx, s.k)
	if err != nil {
		return err
	}
	if err := waitLeader(ctx, s.k, p.Name, time.Minute); err != nil {
		return err
	}
	_, t0, err := leaseHolder(ctx, s.k)
	if err != nil {
		return err
	}
	// controller-runtime renews every 2s (RetryPeriod).
	return e2ekit.WaitFor(ctx, 30*time.Second, "lease renewed", func() (bool, string, error) {
		h, t, err := leaseHolder(ctx, s.k)
		if err != nil {
			return false, "", err
		}
		return t.After(t0) && strings.HasPrefix(h, p.Name+"_"), fmt.Sprintf("holder=%s renew=%s (was %s)", h, t.Format(time.RFC3339Nano), t0.Format(time.RFC3339Nano)), nil
	})
}

func (s *crds) probes(ctx context.Context) error {
	p, err := operatorPod(ctx, s.k)
	if err != nil {
		return err
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		url := fmt.Sprintf("http://%s:8081%s", p.Status.PodIP, path)
		code, body, err := httpGet(ctx, url)
		if err != nil {
			return err
		}
		log.Printf("GET %s: %d %q", url, code, body)
		if code != 200 || strings.TrimSpace(body) != "ok" {
			return fmt.Errorf("GET %s: %d %q", url, code, body)
		}
	}
	return nil
}

// snapshot is what must not change across an operator restart.
type snapshot map[string]string

func (s *crds) snapshot(ctx context.Context) (snapshot, error) {
	k := s.k
	out := snapshot{}
	for _, name := range []string{s.tunnelDep, serverDeploymentName} {
		d, err := deployment(ctx, k, appNS, name)
		if err != nil {
			return nil, err
		}
		out["deployment/"+name+" generation"] = fmt.Sprint(d.Generation)
		pods := &corev1.PodList{}
		if err := k.C.List(ctx, pods, client.InNamespace(appNS), client.MatchingLabels(d.Spec.Selector.MatchLabels)); err != nil {
			return nil, err
		}
		var uids []string
		for _, p := range pods.Items {
			uids = append(uids, string(p.UID))
		}
		out["deployment/"+name+" pods"] = strings.Join(uids, ",")
	}
	return out, nil
}

func (s *crds) restartNoRewrites(ctx context.Context) error {
	k := s.k
	before, err := s.snapshot(ctx)
	if err != nil {
		return err
	}
	log.Printf("before restart: %v", before)
	old, err := operatorPod(ctx, k)
	if err != nil {
		return err
	}
	if err := k.C.Delete(ctx, old); err != nil {
		return err
	}
	p, err := operatorReadyOther(ctx, k, old.UID, 3*time.Minute)
	if err != nil {
		return err
	}
	// The old leader did not release the lease; the new one takes it over
	// after the lease duration.
	if err := waitLeader(ctx, k, p.Name, 2*time.Minute); err != nil {
		return err
	}
	if _, err := waitLog(ctx, k, p.Name, 2*time.Minute,
		"Starting workers\t{\"controller\": \"wefttunnel\"",
		"Starting workers\t{\"controller\": \"weftserver\"",
		"Starting workers\t{\"controller\": \"gateway\"",
	); err != nil {
		return err
	}
	// Give the new leader's initial resync of every object time to land.
	if err := e2ekit.Holds(ctx, 45*time.Second, "tunnel and server Deployments untouched by the new leader", func() (bool, string, error) {
		now, err := s.snapshot(ctx)
		if err != nil {
			return false, "", err
		}
		for key, v := range before {
			if now[key] != v {
				return false, fmt.Sprintf("%s: %s -> %s", key, v, now[key]), nil
			}
		}
		return true, fmt.Sprint(now), nil
	}); err != nil {
		return err
	}
	logs, err := k.PodLogs(ctx, ns, p.Name, "", false)
	if err != nil {
		return err
	}
	if lines := logLines(logs, tunnelReconciledMsg); len(lines) > 0 {
		return fmt.Errorf("new leader wrote existing tunnel Deployments:\n%s", strings.Join(lines, "\n"))
	}
	return nil
}

func (s *crds) experimentalTCPRoute(ctx context.Context) error {
	k := s.k
	if err := k.Apply(ctx, experimentalCRDs, "default"); err != nil {
		return err
	}
	old, err := operatorPod(ctx, k)
	if err != nil {
		return err
	}
	if err := k.C.Delete(ctx, old); err != nil {
		return err
	}
	p, err := operatorReadyOther(ctx, k, old.UID, 3*time.Minute)
	if err != nil {
		return err
	}
	if err := waitLeader(ctx, k, p.Name, 2*time.Minute); err != nil {
		return err
	}
	if err := applyYAML(ctx, k, "tcp-gateway", fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: tcp
  namespace: %[1]s
spec:
  gatewayClassName: weft-gateway-class
  listeners:
  - name: db
    protocol: TCP
    port: 5432
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: db
  namespace: %[1]s
spec:
  parentRefs:
  - name: tcp
  rules:
  - backendRefs:
    - name: backend
      port: 5432
`, appNS)); err != nil {
		return err
	}
	if _, err := waitGatewayTunnel(ctx, k, "tcp", "tcp://backend."+appNS+".svc:5432", "tcp://:5432"); err != nil {
		return err
	}
	logs, err := k.PodLogs(ctx, ns, p.Name, "", false)
	if err != nil {
		return err
	}
	if lines := logLines(logs, routesIgnoredMsg); len(lines) > 0 {
		return fmt.Errorf("route kinds still reported missing with the experimental CRDs:\n%s", strings.Join(lines, "\n"))
	}
	// And the operator stays up with every route kind watched.
	return e2ekit.Holds(ctx, 30*time.Second, "operator stays Ready on the experimental channel", func() (bool, string, error) {
		q, err := operatorPod(ctx, k)
		if err != nil {
			return false, err.Error(), nil
		}
		return q.UID == p.UID && restarts(q) == restarts(p) && podReady(q), e2ekit.ContainerState(q), nil
	})
}

func (s *crds) diagnostics(ctx context.Context) {
	for _, args := range [][]string{
		{"get", "gatewayclass,gateway,httproute,wefttunnel,weftserver,deploy", "-A", "-o", "wide"},
		{"get", "lease", "-n", ns, "-o", "yaml"},
		{"get", "crd"},
	} {
		out, _ := s.k.Kubectl(ctx, args...)
		log.Printf("kubectl %s:\n%s", strings.Join(args, " "), out)
	}
	if p, err := operatorPod(ctx, s.k); err == nil {
		prev, _ := s.k.PodLogs(ctx, ns, p.Name, "", true)
		log.Printf("--- previous operator log (tail):\n%s", e2ekit.Tail(prev, 40))
	}
}
