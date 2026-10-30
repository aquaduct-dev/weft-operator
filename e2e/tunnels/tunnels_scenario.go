// tunnels_scenario.go — the "tunnels" scenario: real `weft server` and
// `weft tunnel` pods made by the operator, real traffic through them.
//
// Layout on the single node (IP nodeIP, every weft server is hostNetwork
// and binds 0.0.0.0):
//
//   - weft-system/main: WeftServer on :9092. Every tunnel with empty
//     targetServers fans out to it.
//   - e2e-alt/alt: WeftServer on :9093 (--port=9093), only reachable for
//     tunnels in e2e-alt.
//   - e2e-gw: the echo backend, the chart's GatewayClass, Gateway "gw" with
//     an HTTP (hostname), a TCP and a UDP listener and one route of each
//     kind. Its tunnels land on main only (e2e-gw has no server).
//
// Two servers on one node share the host's ports, so a tunnel fanned out to
// both can only bind its listener on one of them; the fan-out check below
// therefore asserts the Deployments, not their readiness.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const (
	gwNS       = "e2e-gw"
	altNS      = "e2e-alt"
	gwName     = "gw"
	gwClass    = "weft-gateway-class"
	appHost    = "app.weft-e2e.test"
	mainSecret = "e2emainsecret"
	altSecret  = "e2ealtsecret"
	altSecret2 = "e2ealtsecret2"

	// The value with a space checks the filter value survives the
	// fragment encoding (it used to arrive as "hello+world").
	setValue = "hello world"

	tcpPort      = 7000
	tcpPort2     = 7001 // after the listener edit
	tcpSparePort = 7002
	udpPort      = 7100
	pinnedPort   = 7300
	fanPort      = 7400
)

type tunnelsScenario struct {
	k      *e2ekit.Kit
	nodeIP string
	// expected Gateway tunnels, by role.
	tunnels map[string]expectedTunnel
}

type expectedTunnel struct {
	name, route, src, dst string
	// statusDst is how weft's /list reports dst (proto://host:port).
	statusDst string
}

func (s *tunnelsScenario) mainConn() string {
	return fmt.Sprintf("weft://%s@%s:9092", mainSecret, s.nodeIP)
}
func (s *tunnelsScenario) altConn(secret string) string {
	return fmt.Sprintf("weft://%s@%s:9093", secret, s.nodeIP)
}
func (s *tunnelsScenario) at(port int) string { return net.JoinHostPort(s.nodeIP, fmt.Sprint(port)) }

func tunnelsSteps(k *e2ekit.Kit) []e2ekit.Step {
	s := &tunnelsScenario{k: k}
	k.Diagnostics = s.diagnostics
	return []e2ekit.Step{
		{ID: "op.install", Label: "operator installed from the chart with its GatewayClass, which no Gateway has claimed yet", Fn: s.install},
		{ID: "prober.job-fails", Label: "node prober runs a hostNetwork `weft probe` Job; with no public IP it fails and no node WeftServer appears", Fn: s.proberFails},
		{ID: "server.internal", Label: "WeftServer on :9092 gets a hostNetwork Deployment, Service :9092 and bound PVC, and goes Available once the operator lists its tunnels", Fn: s.mainServer},
		{ID: "server.custom-port", Label: "WeftServer on :9093 passes --port=9093, its Service and status check use 9093", Fn: s.altServer},
		{ID: "gw.foreign-ignored", Label: "a Gateway of a foreign GatewayClass gets no finalizer, status or tunnels", Fn: s.foreignIgnored},
		{ID: "backend.echo", Label: "echo backend (HTTP, TCP, UDP) running", Fn: s.echoBackend},
		{ID: "gw.accepted", Label: "Gateway of the weft class gets the finalizer and Accepted/Programmed; the class becomes Accepted", Fn: s.gatewayAccepted},
		{ID: "gw.tunnels", Label: "one WeftTunnel per (route rule, listener its sectionName selects) with the operator's name, labels, owner and src/dst URLs", Fn: s.gatewayTunnels},
		{ID: "tunnel.deployments", Label: "each tunnel gets a `weft tunnel` Deployment to the weft-system server only, and goes Available", Fn: s.tunnelDeployments},
		{ID: "data.http", Label: "HTTP through the server's :80 vhost reaches the backend with the RequestHeaderModifier applied", Fn: s.dataHTTP},
		{ID: "data.http-path", Label: "a PathPrefix rule wins over / for its prefix (segment-aware); unknown hosts get 404", Fn: s.dataHTTPPath},
		{ID: "data.tcp", Label: "TCPRoute: raw TCP echo through the listener port", Fn: s.dataTCP},
		{ID: "data.udp", Label: "UDPRoute: UDP echo through the listener port", Fn: s.dataUDP},
		{ID: "server.status-tunnels", Label: "WeftServer status lists the live tunnels with their src/dst", Fn: s.statusTunnels},
		{ID: "server.status-txrx", Label: "status Tx/Rx grow after traffic", Fn: s.statusTxRx},
		{ID: "server.no-conn-leak", Label: "the operator holds a bounded number of connections to the server across many status refreshes", Fn: s.noConnLeak},
		{ID: "tunnel.explicit-target", Label: "explicit targetServers limits a tunnel to that server; traffic flows through the :9093 server", Fn: s.explicitTarget},
		{ID: "tunnel.fanout", Label: "empty targetServers fans out to the tunnel's namespace and weft-system; deleting the tunnel removes its Deployments", Fn: s.fanout},
		{ID: "tunnel.connstr-change", Label: "changing a WeftServer's connectionString updates the server and tunnel args; traffic recovers", Fn: s.connStrChange},
		{ID: "prune.route-edit", Label: "editing a route's path replaces its tunnel and Deployment; traffic follows", Fn: s.pruneRouteEdit},
		{ID: "prune.listener-edit", Label: "editing a listener port replaces its tunnel; the old port stops listening", Fn: s.pruneListenerEdit},
		{ID: "gc.server-delete", Label: "deleting a WeftServer removes its workloads and the tunnel Deployments to it", Fn: s.serverDelete},
		{ID: "gc.gateway-delete", Label: "deleting the Gateway removes its tunnels and their Deployments, then the finalizer lets it go", Fn: s.gatewayDelete},
	}
}

func (s *tunnelsScenario) install(ctx context.Context) error {
	ip, err := s.k.NodeIP(ctx)
	if err != nil {
		return err
	}
	s.nodeIP = ip
	log.Printf("node IP %s", ip)
	if err := s.k.InstallOperator(ctx, e2ekit.OperatorInstall{CRDs: gatewayAPICRDs, Manifest: operatorManifest}); err != nil {
		return err
	}
	gc := &gatewayv1.GatewayClass{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Name: gwClass}, gc); err != nil {
		return fmt.Errorf("chart GatewayClass: %w", err)
	}
	if gc.Spec.ControllerName != "weft.aquaduct.dev/gateway-controller" {
		return fmt.Errorf("GatewayClass controllerName %q", gc.Spec.ControllerName)
	}
	// Accepted only once a Gateway of the class is reconciled.
	return e2ekit.Holds(ctx, 10*time.Second, "GatewayClass not yet Accepted", s.classAccepted(false))
}

// classAccepted checks the chart class's Accepted condition is (or, with
// want=false, is not) True.
func (s *tunnelsScenario) classAccepted(want bool) func() (bool, string, error) {
	return func() (bool, string, error) {
		gc := &gatewayv1.GatewayClass{}
		if err := s.k.C.Get(context.Background(), types.NamespacedName{Name: gwClass}, gc); err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(gc.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
		accepted := c != nil && c.Status == metav1.ConditionTrue
		return accepted == want, e2ekit.CondString(c), nil
	}
}

func (s *tunnelsScenario) proberFails(ctx context.Context) error {
	var job batchv1.Job
	err := e2ekit.WaitFor(ctx, 2*time.Minute, "probe Job for server0", func() (bool, string, error) {
		jobs := &batchv1.JobList{}
		if err := s.k.C.List(ctx, jobs, client.InNamespace(e2ekit.OperatorNS), client.MatchingLabels{"app": "weft-probe"}); err != nil {
			return false, "", err
		}
		for _, j := range jobs.Items {
			if strings.HasPrefix(j.Name, "weft-probe-server0-") {
				job = j
				return j.Status.Failed > 0, fmt.Sprintf("%s failed=%d succeeded=%d", j.Name, j.Status.Failed, j.Status.Succeeded), nil
			}
		}
		return false, fmt.Sprintf("%d probe jobs", len(jobs.Items)), nil
	})
	if err != nil {
		return err
	}
	if err := checkProbeJob(&job); err != nil {
		return err
	}
	// The prober gives up after the failure: no node WeftServer.
	return e2ekit.Holds(ctx, 15*time.Second, "no node-server0 WeftServer", func() (bool, string, error) {
		gone, state, err := s.k.Gone(&weftv1alpha1.WeftServer{}, e2ekit.OperatorNS, "node-server0")()
		return gone, state, err
	})
}

// checkProbeJob asserts the shape of a node probe Job.
func checkProbeJob(j *batchv1.Job) error {
	ps := j.Spec.Template.Spec
	var errs []string
	if !ps.HostNetwork {
		errs = append(errs, "not hostNetwork")
	}
	if ps.NodeName != "server0" {
		errs = append(errs, "nodeName "+ps.NodeName)
	}
	if len(ps.Containers) != 1 || ps.Containers[0].Image != weftImage || !slices.Equal(ps.Containers[0].Args, []string{"probe"}) || ps.Containers[0].ImagePullPolicy != corev1.PullIfNotPresent {
		errs = append(errs, fmt.Sprintf("containers %+v", ps.Containers))
	}
	if j.Spec.BackoffLimit == nil || *j.Spec.BackoffLimit != 0 {
		errs = append(errs, "backoffLimit not 0")
	}
	if len(errs) > 0 {
		return fmt.Errorf("probe job %s: %s", j.Name, strings.Join(errs, "; "))
	}
	return nil
}

func (s *tunnelsScenario) mainServer(ctx context.Context) error {
	if err := s.k.C.Create(ctx, weftServer(e2ekit.OperatorNS, "main", s.mainConn())); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "main-server available", s.k.DeploymentAvailable(e2ekit.OperatorNS, "main-server")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "main Available=True", serverAvailable(s.k, e2ekit.OperatorNS, "main")); err != nil {
		return err
	}
	return checkServerWorkloads(ctx, s.k, e2ekit.OperatorNS, "main", serverArgs(mainSecret, 9092), 9092)
}

func (s *tunnelsScenario) altServer(ctx context.Context) error {
	if err := s.k.C.Create(ctx, weftServer(altNS, "alt", s.altConn(altSecret))); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "alt-server available", s.k.DeploymentAvailable(altNS, "alt-server")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "alt Available=True", serverAvailable(s.k, altNS, "alt")); err != nil {
		return err
	}
	return checkServerWorkloads(ctx, s.k, altNS, "alt", serverArgs(altSecret, 9093), 9093)
}

func (s *tunnelsScenario) foreignIgnored(ctx context.Context) error {
	if err := s.k.C.Create(ctx, &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "other-class"},
		Spec:       gatewayv1.GatewayClassSpec{ControllerName: "example.com/other-controller"},
	}); err != nil {
		return err
	}
	if err := s.k.C.Create(ctx, &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: "foreign"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "other-class",
			Listeners: []gatewayv1.Listener{{
				Name: "http", Port: 80, Protocol: gatewayv1.HTTPProtocolType,
				Hostname: e2ekit.Ptr(gatewayv1.Hostname("foreign.weft-e2e.test")),
			}},
		},
	}); err != nil {
		return err
	}
	if err := s.k.C.Create(ctx, httpRoute("foreign-route", "foreign", "http", "/", nil)); err != nil {
		return err
	}
	if err := e2ekit.Holds(ctx, 15*time.Second, "foreign Gateway untouched", func() (bool, string, error) {
		gw := &gatewayv1.Gateway{}
		if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: gwNS, Name: "foreign"}, gw); err != nil {
			return false, "", err
		}
		if len(gw.Finalizers) > 0 {
			return false, fmt.Sprintf("finalizers %v", gw.Finalizers), nil
		}
		for _, c := range gw.Status.Conditions {
			if c.Status == metav1.ConditionTrue {
				return false, "condition " + e2ekit.CondString(&c), nil
			}
		}
		tl := &weftv1alpha1.WeftTunnelList{}
		if err := s.k.C.List(ctx, tl, client.MatchingLabels{"gateway": "foreign"}); err != nil {
			return false, "", err
		}
		if len(tl.Items) > 0 {
			return false, fmt.Sprintf("%d tunnels", len(tl.Items)), nil
		}
		return true, "no finalizer, status or tunnels", nil
	}); err != nil {
		return err
	}
	// Nor does a foreign Gateway make our class Accepted.
	ok, state, err := s.classAccepted(false)()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("weft GatewayClass Accepted with no weft Gateway: %s", state)
	}
	return nil
}

func (s *tunnelsScenario) echoBackend(ctx context.Context) error {
	if err := s.k.C.Create(ctx, selfDeployment(gwNS, "echo", false, "echo")); err != nil {
		return err
	}
	if err := s.k.C.Create(ctx, echoService(gwNS)); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 3*time.Minute, "echo available", s.k.DeploymentAvailable(gwNS, "echo"))
}

// httpRoute builds an HTTPRoute on (gateway, sectionName) with one
// PathPrefix rule to echo:80.
func httpRoute(name, gateway, section, path string, filter *gatewayv1.HTTPHeaderFilter) *gatewayv1.HTTPRoute {
	r := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: name},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
				Name: gatewayv1.ObjectName(gateway), SectionName: e2ekit.Ptr(gatewayv1.SectionName(section)),
			}}},
		},
	}
	r.Spec.Rules = append(r.Spec.Rules, httpRule(path, filter))
	return r
}

func httpRule(path string, filter *gatewayv1.HTTPHeaderFilter) gatewayv1.HTTPRouteRule {
	rule := gatewayv1.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{
			Type: e2ekit.Ptr(gatewayv1.PathMatchPathPrefix), Value: e2ekit.Ptr(path),
		}}},
		BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
			Name: "echo", Port: e2ekit.Ptr(gatewayv1.PortNumber(80)),
		}}}},
	}
	if filter != nil {
		rule.Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier, RequestHeaderModifier: filter}}
	}
	return rule
}

func (s *tunnelsScenario) gatewayAccepted(ctx context.Context) error {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: gwName},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gwClass,
			Listeners: []gatewayv1.Listener{
				{Name: "http", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: e2ekit.Ptr(gatewayv1.Hostname(appHost))},
				{Name: "tcp", Port: tcpPort, Protocol: gatewayv1.TCPProtocolType},
				// No route names it: the sectionName-pinned TCPRoute must
				// not be published here too.
				{Name: "tcp-spare", Port: tcpSparePort, Protocol: gatewayv1.TCPProtocolType},
				{Name: "udp", Port: udpPort, Protocol: gatewayv1.UDPProtocolType},
			},
		},
	}
	if err := s.k.C.Create(ctx, gw); err != nil {
		return err
	}
	web := httpRoute("web", gwName, "http", "/", &gatewayv1.HTTPHeaderFilter{
		Set:    []gatewayv1.HTTPHeader{{Name: "X-E2e-Set", Value: setValue}},
		Add:    []gatewayv1.HTTPHeader{{Name: "X-E2e-Add", Value: "added"}},
		Remove: []string{"X-E2e-Remove"},
	})
	web.Spec.Hostnames = []gatewayv1.Hostname{appHost}
	web.Spec.Rules = append(web.Spec.Rules, httpRule("/api", &gatewayv1.HTTPHeaderFilter{
		Set: []gatewayv1.HTTPHeader{{Name: "X-E2e-Rule", Value: "api"}},
	}))
	if err := s.k.C.Create(ctx, web); err != nil {
		return err
	}
	parent := func(section string) gatewayv1.CommonRouteSpec {
		return gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: gwName, SectionName: e2ekit.Ptr(gatewayv1.SectionName(section))}}}
	}
	backend := func(port int32) gatewayv1.BackendRef {
		return gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "echo", Port: e2ekit.Ptr(port)}}
	}
	if err := s.k.C.Create(ctx, &gatewayv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: "tcp"},
		Spec: gatewayv1alpha2.TCPRouteSpec{CommonRouteSpec: parent("tcp"), Rules: []gatewayv1alpha2.TCPRouteRule{{
			BackendRefs: []gatewayv1alpha2.BackendRef{backend(9000)},
		}}},
	}); err != nil {
		return err
	}
	if err := s.k.C.Create(ctx, &gatewayv1alpha2.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: "udp"},
		Spec: gatewayv1alpha2.UDPRouteSpec{CommonRouteSpec: parent("udp"), Rules: []gatewayv1alpha2.UDPRouteRule{{
			BackendRefs: []gatewayv1alpha2.BackendRef{backend(9001)},
		}}},
	}); err != nil {
		return err
	}

	if err := e2ekit.WaitFor(ctx, time.Minute, "Gateway finalizer + Accepted/Programmed", func() (bool, string, error) {
		gw := &gatewayv1.Gateway{}
		if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: gwNS, Name: gwName}, gw); err != nil {
			return false, "", err
		}
		acc := e2ekit.Condition(gw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
		prog := e2ekit.Condition(gw.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
		ok := slices.Contains(gw.Finalizers, "weft.aquaduct.dev/cleanup-dnsrecords") &&
			acc != nil && acc.Status == metav1.ConditionTrue && prog != nil && prog.Status == metav1.ConditionTrue
		return ok, fmt.Sprintf("finalizers=%v %s %s", gw.Finalizers, e2ekit.CondString(acc), e2ekit.CondString(prog)), nil
	}); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 30*time.Second, "GatewayClass Accepted", s.classAccepted(true))
}

func (s *tunnelsScenario) gatewayTunnels(ctx context.Context) error {
	src := "http://echo." + gwNS + ".svc:80"
	s.tunnels = map[string]expectedTunnel{}
	add := func(role, route, src, dst, statusDst string) {
		s.tunnels[role] = expectedTunnel{name: tunnelName(gwName, route, dst), route: route, src: src, dst: dst, statusDst: statusDst}
	}
	// Header filter -> weft's src fragment: name=value sets, name=+value
	// adds if absent, name=!del removes.
	add("http-root", "web", src+"/#X-E2e-Set=hello%20world&X-E2e-Add=+added&X-E2e-Remove=!del", "http://"+appHost+"/", "http://"+appHost+":80")
	add("http-api", "web", src+"/#X-E2e-Rule=api", "http://"+appHost+"/api", "http://"+appHost+":80")
	add("tcp", "tcp", "tcp://echo."+gwNS+".svc:9000", fmt.Sprintf("tcp://:%d", tcpPort), fmt.Sprintf("tcp://:%d", tcpPort))
	add("udp", "udp", "udp://echo."+gwNS+".svc:9001", fmt.Sprintf("udp://:%d", udpPort), fmt.Sprintf("udp://:%d", udpPort))
	return e2ekit.WaitFor(ctx, time.Minute, "Gateway tunnels match", s.gatewayTunnelsMatch)
}

// gatewayTunnelsMatch checks the Gateway's WeftTunnels are exactly
// s.tunnels.
func (s *tunnelsScenario) gatewayTunnelsMatch() (bool, string, error) {
	ctx := context.Background()
	gw := &gatewayv1.Gateway{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: gwNS, Name: gwName}, gw); err != nil {
		return false, "", err
	}
	tl := &weftv1alpha1.WeftTunnelList{}
	if err := s.k.C.List(ctx, tl, client.InNamespace(gwNS), client.MatchingLabels{"gateway": gwName}); err != nil {
		return false, "", err
	}
	got := map[string]weftv1alpha1.WeftTunnel{}
	for _, t := range tl.Items {
		got[t.Name] = t
	}
	var problems []string
	for role, want := range s.tunnels {
		t, ok := got[want.name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %s missing", role, want.name))
			continue
		}
		delete(got, want.name)
		if t.Spec.SrcURL != want.src || t.Spec.DstURL != want.dst || len(t.Spec.TargetServers) != 0 {
			problems = append(problems, fmt.Sprintf("%s: spec src=%q dst=%q targets=%v, want src=%q dst=%q", role, t.Spec.SrcURL, t.Spec.DstURL, t.Spec.TargetServers, want.src, want.dst))
		}
		wantLabels := map[string]string{"app": "weft-gateway-tunnel", "gateway": gwName, "route": want.route, "created-by": "weft-operator"}
		for lk, lv := range wantLabels {
			if t.Labels[lk] != lv {
				problems = append(problems, fmt.Sprintf("%s: label %s=%q", role, lk, t.Labels[lk]))
			}
		}
		if c := metav1.GetControllerOf(&t); c == nil || c.Kind != "Gateway" || c.UID != gw.UID {
			problems = append(problems, fmt.Sprintf("%s: controller %v, want Gateway %s", role, c, gw.UID))
		}
	}
	for name, t := range got {
		problems = append(problems, fmt.Sprintf("unexpected tunnel %s (%s -> %s)", name, t.Spec.SrcURL, t.Spec.DstURL))
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "; "), nil
	}
	return true, fmt.Sprintf("%d tunnels", len(s.tunnels)), nil
}

func (s *tunnelsScenario) tunnelDeployments(ctx context.Context) error {
	for role, t := range s.tunnels {
		if err := e2ekit.WaitFor(ctx, 3*time.Minute, role+" tunnel Available", tunnelAvailable(s.k, gwNS, t.name, metav1.ConditionTrue)); err != nil {
			return err
		}
		if err := checkTunnelDeployment(ctx, s.k, gwNS, t.name, "main", s.mainConn(), t.src, t.dst); err != nil {
			return err
		}
		deps, err := tunnelDeployments(ctx, s.k, gwNS, t.name)
		if err != nil {
			return err
		}
		// e2e-gw has no server and alt lives in e2e-alt: main only.
		if want := []string{tunnelDeploymentName(t.name, "main")}; !slices.Equal(deps, want) {
			return fmt.Errorf("%s: deployments %v, want %v", role, deps, want)
		}
	}
	return nil
}

func (s *tunnelsScenario) dataHTTP(ctx context.Context) error {
	var reply *echoReply
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "GET / via "+appHost, func() (bool, string, error) {
		code, r, err := httpVia(ctx, s.at(80), appHost, "/hello", map[string]string{
			"X-E2e-Set":    "from-client",
			"X-E2e-Remove": "must-go",
		})
		if err != nil {
			return false, err.Error(), nil
		}
		reply = r
		return code == 200 && r != nil, fmt.Sprintf("status %d echo=%v", code, r != nil), nil
	}); err != nil {
		return err
	}
	log.Printf("echo saw: %+v", *reply)
	var errs []string
	if v, _ := header(reply, "X-E2e-Set"); v != setValue {
		errs = append(errs, fmt.Sprintf("X-E2e-Set=%q, want %q (set overrides the client's)", v, setValue))
	}
	if v, _ := header(reply, "X-E2e-Add"); v != "added" {
		errs = append(errs, fmt.Sprintf("X-E2e-Add=%q, want %q", v, "added"))
	}
	if v, ok := header(reply, "X-E2e-Remove"); ok {
		errs = append(errs, fmt.Sprintf("X-E2e-Remove=%q survived the remove filter", v))
	}
	if v, ok := header(reply, "X-E2e-Rule"); ok {
		errs = append(errs, fmt.Sprintf("/hello hit the /api rule (X-E2e-Rule=%q)", v))
	}
	if reply.Path != "/hello" {
		errs = append(errs, fmt.Sprintf("backend saw path %q, want /hello", reply.Path))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	// weft's "+value" only sets an absent header, so Gateway API "add"
	// does not append to a header the client already sent. Logged, not
	// asserted: it is weft's modifier semantics.
	if _, r, err := httpVia(ctx, s.at(80), appHost, "/", map[string]string{"X-E2e-Add": "client"}); err == nil && r != nil {
		v, _ := header(r, "X-E2e-Add")
		log.Printf("note: with the client sending X-E2e-Add: client the backend saw %q (Gateway API add would append)", v)
	}
	return nil
}

// routedTo returns which rule served path: "api", "root", or an error
// state.
func (s *tunnelsScenario) routedTo(ctx context.Context, path string) (string, *echoReply, error) {
	code, r, err := httpVia(ctx, s.at(80), appHost, path, nil)
	if err != nil {
		return "", nil, err
	}
	if code != 200 || r == nil {
		return "", nil, fmt.Errorf("GET %s: status %d", path, code)
	}
	if v, _ := header(r, "X-E2e-Rule"); v != "" {
		return v, r, nil
	}
	if v, _ := header(r, "X-E2e-Set"); v == setValue {
		return "root", r, nil
	}
	return "", r, fmt.Errorf("GET %s: reply matches no rule: %+v", path, *r)
}

func (s *tunnelsScenario) dataHTTPPath(ctx context.Context) error {
	for path, want := range map[string]string{"/api/thing": "api", "/api": "api", "/apix": "root", "/other/api": "root"} {
		got, r, err := s.routedTo(ctx, path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("GET %s went to the %q rule, want %q", path, got, want)
		}
		log.Printf("GET %s -> %s rule, backend path %q", path, got, r.Path)
		if path == "/api/thing" && r.Path != "/thing" {
			// weft strips the matched prefix before forwarding (Gateway
			// API PathPrefix would not). Asserted to pin down current
			// behavior: a change here is a weft semantics change.
			return fmt.Errorf("backend saw %q for /api/thing; weft strips the matched prefix, expected /thing", r.Path)
		}
	}
	code, r, err := httpVia(ctx, s.at(80), "nope.weft-e2e.test", "/", nil)
	if err != nil {
		return err
	}
	if code != 404 || r != nil {
		return fmt.Errorf("unknown host: status %d echo=%v, want weft's 404", code, r != nil)
	}
	return nil
}

func (s *tunnelsScenario) dataTCP(ctx context.Context) error {
	return e2ekit.WaitFor(ctx, time.Minute, fmt.Sprintf("TCP echo via :%d", tcpPort), func() (bool, string, error) {
		if err := tcpEcho(ctx, s.at(tcpPort), 64<<10); err != nil {
			return false, err.Error(), nil
		}
		return true, "64KiB echoed", nil
	})
}

func (s *tunnelsScenario) dataUDP(ctx context.Context) error {
	return e2ekit.WaitFor(ctx, time.Minute, fmt.Sprintf("UDP echo via :%d", udpPort), func() (bool, string, error) {
		if err := udpEcho(ctx, s.at(udpPort)); err != nil {
			return false, err.Error(), nil
		}
		return true, "datagram echoed", nil
	})
}

// sameURL compares a URL from weft's /list with one we passed weft
// (weft re-serializes it, which can change fragment escaping).
func sameURL(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host && strings.TrimSuffix(ua.Path, "/") == strings.TrimSuffix(ub.Path, "/") && ua.Fragment == ub.Fragment
}

// statusEntry finds the main server's status entry for a tunnel.
func (s *tunnelsScenario) statusEntry(ctx context.Context, ns, server string, t expectedTunnel) (*weftv1alpha1.TunnelStatus, []weftv1alpha1.TunnelStatus, error) {
	ws := &weftv1alpha1.WeftServer{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: ns, Name: server}, ws); err != nil {
		return nil, nil, err
	}
	for i := range ws.Status.Tunnels {
		e := &ws.Status.Tunnels[i]
		if sameURL(e.SrcURL, t.src) && e.DstURL == t.statusDst {
			return e, ws.Status.Tunnels, nil
		}
	}
	return nil, ws.Status.Tunnels, nil
}

func (s *tunnelsScenario) statusTunnels(ctx context.Context) error {
	return e2ekit.WaitFor(ctx, 90*time.Second, "main status lists the Gateway tunnels", func() (bool, string, error) {
		var all []weftv1alpha1.TunnelStatus
		var missing []string
		for role, t := range s.tunnels {
			e, a, err := s.statusEntry(ctx, e2ekit.OperatorNS, "main", t)
			if err != nil {
				return false, "", err
			}
			all = a
			if e == nil {
				missing = append(missing, role)
			}
		}
		if len(missing) > 0 {
			return false, fmt.Sprintf("missing %v in %+v", missing, all), nil
		}
		if len(all) != len(s.tunnels) {
			return false, fmt.Sprintf("%d entries, want %d: %+v", len(all), len(s.tunnels), all), nil
		}
		return true, fmt.Sprintf("%d entries", len(all)), nil
	})
}

func (s *tunnelsScenario) statusTxRx(ctx context.Context) error {
	tcp := s.tunnels["tcp"]
	before, _, err := s.statusEntry(ctx, e2ekit.OperatorNS, "main", tcp)
	if err != nil || before == nil {
		return fmt.Errorf("tcp status entry: %v", err)
	}
	b := *before
	const n = 256 << 10
	if err := tcpEcho(ctx, s.at(tcpPort), n); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "tcp tunnel Tx/Rx grow", func() (bool, string, error) {
		e, _, err := s.statusEntry(ctx, e2ekit.OperatorNS, "main", tcp)
		if err != nil || e == nil {
			return false, fmt.Sprintf("entry missing (%v)", err), nil
		}
		state := fmt.Sprintf("tx %d->%d rx %d->%d", b.Tx, e.Tx, b.Rx, e.Rx)
		return e.Tx > b.Tx && e.Rx > b.Rx && (e.Tx+e.Rx)-(b.Tx+b.Rx) >= n, state, nil
	})
}

// noConnLeak watches the operator's connections to the server port while
// status refreshes happen. The fixed leak opened one new keep-alive
// connection per refresh and never closed it, so after the >=4 refreshes
// this step requires the count would be >= 5; a cached client keeps one.
func (s *tunnelsScenario) noConnLeak(ctx context.Context) error {
	pid, err := e2ekit.ContainerPID(ctx, "weft-operator")
	if err != nil {
		return err
	}
	tcp := s.tunnels["tcp"]
	seenTx := map[uint64]bool{}
	var samples []int
	deadline := time.Now().Add(3*time.Minute + 10*time.Second)
	for time.Now().Before(deadline) {
		// Move the counters so every refresh writes a new status: the
		// number of distinct values seen is a lower bound on refreshes.
		if err := tcpEcho(ctx, s.at(tcpPort), 1024); err != nil {
			log.Printf("traffic: %v", err)
		}
		n, err := e2ekit.EstablishedConns(pid, 9092)
		if err != nil {
			return err
		}
		samples = append(samples, n)
		if e, _, err := s.statusEntry(ctx, e2ekit.OperatorNS, "main", tcp); err == nil && e != nil {
			seenTx[e.Tx] = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	log.Printf("operator connections to :9092 over %d samples: %v; %d distinct tx values", len(samples), samples, len(seenTx))
	if len(seenTx) < 5 {
		return fmt.Errorf("only %d distinct status tx values in 3m: too few refreshes to judge a leak", len(seenTx))
	}
	maxN := slices.Max(samples)
	if maxN > 2 {
		return fmt.Errorf("operator held up to %d connections to the server (samples %v); want <= 2", maxN, samples)
	}
	if samples[len(samples)-1] > samples[0]+1 {
		return fmt.Errorf("connections grew from %d to %d", samples[0], samples[len(samples)-1])
	}
	return nil
}

func (s *tunnelsScenario) explicitTarget(ctx context.Context) error {
	src := "tcp://echo." + gwNS + ".svc:9000"
	dst := fmt.Sprintf("tcp://:%d", pinnedPort)
	if err := s.k.C.Create(ctx, weftTunnel(altNS, "pinned", src, dst, "alt")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "pinned Available", tunnelAvailable(s.k, altNS, "pinned", metav1.ConditionTrue)); err != nil {
		return err
	}
	if err := checkTunnelDeployment(ctx, s.k, altNS, "pinned", "alt", s.altConn(altSecret), src, dst); err != nil {
		return err
	}
	deps, err := tunnelDeployments(ctx, s.k, altNS, "pinned")
	if err != nil {
		return err
	}
	if want := []string{tunnelDeploymentName("pinned", "alt")}; !slices.Equal(deps, want) {
		return fmt.Errorf("pinned deployments %v, want %v (main must not be targeted)", deps, want)
	}
	return s.waitTCP(ctx, pinnedPort, time.Minute)
}

func (s *tunnelsScenario) waitTCP(ctx context.Context, port int, timeout time.Duration) error {
	return e2ekit.WaitFor(ctx, timeout, fmt.Sprintf("TCP echo via :%d", port), func() (bool, string, error) {
		if err := tcpEcho(ctx, s.at(port), 4096); err != nil {
			return false, err.Error(), nil
		}
		return true, "echoed", nil
	})
}

func (s *tunnelsScenario) fanout(ctx context.Context) error {
	src := "tcp://echo." + gwNS + ".svc:9000"
	dst := fmt.Sprintf("tcp://:%d", fanPort)
	if err := s.k.C.Create(ctx, weftTunnel(altNS, "fan", src, dst)); err != nil {
		return err
	}
	want := []string{tunnelDeploymentName("fan", "alt"), tunnelDeploymentName("fan", "main")}
	if err := e2ekit.WaitFor(ctx, time.Minute, "fan deployments", func() (bool, string, error) {
		deps, err := tunnelDeployments(ctx, s.k, altNS, "fan")
		return slices.Equal(deps, want), fmt.Sprint(deps), err
	}); err != nil {
		return err
	}
	if err := checkTunnelDeployment(ctx, s.k, altNS, "fan", "alt", s.altConn(altSecret), src, dst); err != nil {
		return err
	}
	if err := checkTunnelDeployment(ctx, s.k, altNS, "fan", "main", s.mainConn(), src, dst); err != nil {
		return err
	}
	// Whichever of the two binds :7400 first serves it (both servers share
	// the node's ports).
	if err := s.waitTCP(ctx, fanPort, 2*time.Minute); err != nil {
		return err
	}
	if err := s.k.C.Delete(ctx, weftTunnel(altNS, "fan", "", "")); err != nil {
		return err
	}
	for _, d := range want {
		if err := e2ekit.WaitFor(ctx, time.Minute, d+" gone", s.k.Gone(&appsv1.Deployment{}, altNS, d)); err != nil {
			return err
		}
	}
	return nil
}

func (s *tunnelsScenario) connStrChange(ctx context.Context) error {
	ws := &weftv1alpha1.WeftServer{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: altNS, Name: "alt"}, ws); err != nil {
		return err
	}
	ws.Spec.ConnectionString = s.altConn(altSecret2)
	if err := s.k.C.Update(ctx, ws); err != nil {
		return err
	}
	src := "tcp://echo." + gwNS + ".svc:9000"
	dst := fmt.Sprintf("tcp://:%d", pinnedPort)
	if err := e2ekit.WaitFor(ctx, time.Minute, "server and tunnel args follow the new connectionString", func() (bool, string, error) {
		if err := checkServerWorkloads(ctx, s.k, altNS, "alt", serverArgs(altSecret2, 9093), 9093); err != nil {
			return false, err.Error(), nil
		}
		if err := checkTunnelDeployment(ctx, s.k, altNS, "pinned", "alt", s.altConn(altSecret2), src, dst); err != nil {
			return false, err.Error(), nil
		}
		return true, "updated", nil
	}); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "alt Available with the new secret", serverAvailable(s.k, altNS, "alt")); err != nil {
		return err
	}
	return s.waitTCP(ctx, pinnedPort, 3*time.Minute)
}

func (s *tunnelsScenario) pruneRouteEdit(ctx context.Context) error {
	old := s.tunnels["http-api"]
	r := &gatewayv1.HTTPRoute{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: gwNS, Name: "web"}, r); err != nil {
		return err
	}
	r.Spec.Rules[1].Matches[0].Path.Value = e2ekit.Ptr("/v2")
	if err := s.k.C.Update(ctx, r); err != nil {
		return err
	}
	dst := "http://" + appHost + "/v2"
	s.tunnels["http-api"] = expectedTunnel{name: tunnelName(gwName, "web", dst), route: "web", src: old.src, dst: dst, statusDst: old.statusDst}
	if err := e2ekit.WaitFor(ctx, time.Minute, "tunnels after the route edit", s.gatewayTunnelsMatch); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "old /api tunnel Deployment gone", s.k.Gone(&appsv1.Deployment{}, gwNS, tunnelDeploymentName(old.name, "main"))); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "/v2 tunnel Available", tunnelAvailable(s.k, gwNS, s.tunnels["http-api"].name, metav1.ConditionTrue)); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "/v2 routed to the edited rule, /api back to /", func() (bool, string, error) {
		v2, _, err := s.routedTo(ctx, "/v2/x")
		if err != nil {
			return false, err.Error(), nil
		}
		api, _, err := s.routedTo(ctx, "/api/x")
		if err != nil {
			return false, err.Error(), nil
		}
		return v2 == "api" && api == "root", fmt.Sprintf("/v2/x->%s /api/x->%s", v2, api), nil
	})
}

func (s *tunnelsScenario) pruneListenerEdit(ctx context.Context) error {
	old := s.tunnels["tcp"]
	gw := &gatewayv1.Gateway{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: gwNS, Name: gwName}, gw); err != nil {
		return err
	}
	for i := range gw.Spec.Listeners {
		if gw.Spec.Listeners[i].Name == "tcp" {
			gw.Spec.Listeners[i].Port = tcpPort2
		}
	}
	if err := s.k.C.Update(ctx, gw); err != nil {
		return err
	}
	dst := fmt.Sprintf("tcp://:%d", tcpPort2)
	s.tunnels["tcp"] = expectedTunnel{name: tunnelName(gwName, "tcp", dst), route: "tcp", src: old.src, dst: dst, statusDst: dst}
	if err := e2ekit.WaitFor(ctx, time.Minute, "tunnels after the listener edit", s.gatewayTunnelsMatch); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "old tcp tunnel Deployment gone", s.k.Gone(&appsv1.Deployment{}, gwNS, tunnelDeploymentName(old.name, "main"))); err != nil {
		return err
	}
	if err := s.waitTCP(ctx, tcpPort2, 3*time.Minute); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, fmt.Sprintf(":%d stops listening", tcpPort), func() (bool, string, error) {
		ok, state := refused(ctx, s.at(tcpPort))
		return ok, state, nil
	})
}

func (s *tunnelsScenario) serverDelete(ctx context.Context) error {
	if err := s.k.C.Delete(ctx, weftServer(altNS, "alt", "")); err != nil {
		return err
	}
	for _, o := range []struct {
		obj  client.Object
		name string
	}{
		{&appsv1.Deployment{}, "alt-server"},
		{&corev1.Service{}, "alt-server"},
		{&corev1.PersistentVolumeClaim{}, "alt-certs"},
		{&appsv1.Deployment{}, tunnelDeploymentName("pinned", "alt")},
	} {
		if err := e2ekit.WaitFor(ctx, 2*time.Minute, fmt.Sprintf("%T %s gone", o.obj, o.name), s.k.Gone(o.obj, altNS, o.name)); err != nil {
			return err
		}
	}
	return e2ekit.WaitFor(ctx, time.Minute, "pinned no longer Available", tunnelAvailable(s.k, altNS, "pinned", metav1.ConditionFalse))
}

func (s *tunnelsScenario) gatewayDelete(ctx context.Context) error {
	if err := s.k.C.Delete(ctx, &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: gwNS, Name: gwName}}); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "Gateway gone (finalizer released)", s.k.Gone(&gatewayv1.Gateway{}, gwNS, gwName)); err != nil {
		return err
	}
	for role, t := range s.tunnels {
		if err := e2ekit.WaitFor(ctx, 2*time.Minute, role+" tunnel gone", s.k.Gone(&weftv1alpha1.WeftTunnel{}, gwNS, t.name)); err != nil {
			return err
		}
		if err := e2ekit.WaitFor(ctx, 2*time.Minute, role+" tunnel Deployment gone", s.k.Gone(&appsv1.Deployment{}, gwNS, tunnelDeploymentName(t.name, "main"))); err != nil {
			return err
		}
	}
	// And the server stops serving the hostname.
	return e2ekit.WaitFor(ctx, 2*time.Minute, appHost+" no longer served", func() (bool, string, error) {
		code, r, err := httpVia(ctx, s.at(80), appHost, "/", nil)
		if err != nil {
			return true, err.Error(), nil
		}
		return r == nil, fmt.Sprintf("status %d echo=%v", code, r != nil), nil
	})
}

func (s *tunnelsScenario) diagnostics(ctx context.Context) {
	for _, ns := range []string{e2ekit.OperatorNS, gwNS, altNS} {
		logPods(ctx, s.k, ns, log.Printf)
	}
	for _, ns := range []string{e2ekit.OperatorNS, altNS} {
		l := &weftv1alpha1.WeftServerList{}
		if s.k.C.List(ctx, l, client.InNamespace(ns)) == nil {
			for _, w := range l.Items {
				log.Printf("weftserver %s/%s %s conditions=%+v tunnels=%+v", w.Namespace, w.Name, w.Spec.ConnectionString, w.Status.Conditions, w.Status.Tunnels)
			}
		}
	}
	tl := &weftv1alpha1.WeftTunnelList{}
	if s.k.C.List(ctx, tl) == nil {
		for _, t := range tl.Items {
			log.Printf("wefttunnel %s/%s %s -> %s targets=%v %s", t.Namespace, t.Name, t.Spec.SrcURL, t.Spec.DstURL, t.Spec.TargetServers, e2ekit.CondString(e2ekit.Condition(t.Status.Conditions, "Available")))
		}
	}
	gwl := &gatewayv1.GatewayList{}
	if s.k.C.List(ctx, gwl) == nil {
		for _, g := range gwl.Items {
			log.Printf("gateway %s/%s finalizers=%v conditions=%+v", g.Namespace, g.Name, g.Finalizers, g.Status.Conditions)
		}
	}
}
