// scenario.go — the "aquaduct" scenario: AquaductTaaS, DNSRecord and
// Gateway AutoDNS driven against the in-cluster fake aquaduct.dev (fake.go).
// The operator's real HTTP client talks to the fake through ClusterIP
// Services: spec.apiEndpoint points at fake-aquaduct and the chart's
// extraEnv points AUTHZ_ENDPOINT at fake-authz (the same pod, so the log
// shows which host each call went to).
//
// Steps run in order and build on each other; this file has the setup and
// the AquaductTaaS steps, scenario_dns.go the DNSRecord steps and
// scenario_gateway.go the Gateway and deletion steps.
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

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
	fakeImage = "ghcr.io/aquaduct-dev/weft-operator-e2e-aquaduct:e2e"
	weftImage = "ghcr.io/aquaduct-dev/weft:e2e"

	fakeNS   = "aquaduct-fake"
	taasNS   = "e2e-taas"
	appsNS   = "e2e-apps"
	taasName = "aquaduct"

	secretName  = "aquaduct-token"
	secretKey   = "token"
	accessToken = "aqt_e2e-long-lived-access-token"

	// The fake's two names: the API (spec.apiEndpoint) and the token
	// exchange (AUTHZ_ENDPOINT in e2e-values.yaml).
	apiHost     = "fake-aquaduct.aquaduct-fake.svc.cluster.local"
	authzHost   = "fake-authz.aquaduct-fake.svc.cluster.local"
	apiEndpoint = "http://" + apiHost

	operatorManifest = "/etc/e2e/weft-operator.yaml"
	gatewayAPICRDs   = "/etc/e2e/gateway-api-crds.yaml"

	exchangePath = "/api/auth/access-token"
	bastionPath  = "/api/bastion"

	// Finalizers and labels the operator stamps.
	taasFinalizer    = "weft.aquaduct.dev/suspend-on-delete"
	secretFinalizer  = "weft.aquaduct.dev/aquaducttaas-token-in-use"
	recordFinalizer  = "weft.aquaduct.dev/unregister-on-delete"
	gatewayFinalizer = "weft.aquaduct.dev/cleanup-dnsrecords"
	ownerLabel       = "weft.aquaduct.dev/aquaducttaas"
	bastionIDAnno    = "weft.aquaduct.dev/bastion-id"

	// bastion-a's connection secret; a real weft server runs with it.
	bastionASecret = "e2ebastionasecret"
)

type scenario struct {
	k       *e2ekit.Kit
	fakeURL string
	nodeIP  string
	touches int
}

func newScenario(k *e2ekit.Kit) *scenario {
	s := &scenario{k: k}
	k.Diagnostics = s.diagnostics
	return s
}

func (s *scenario) steps() []e2ekit.Step {
	return []e2ekit.Step{
		{ID: "aq.operator-ready", Label: "operator installed from the chart with AUTHZ_ENDPOINT and the GatewayClass", Fn: s.installOperator},
		{ID: "aq.fake-ready", Label: "fake aquaduct.dev API and a real weft server (bastion-a) running", Fn: s.deployFake},

		{ID: "taas.secret-missing", Label: "TaaS gets its finalizer and Available=False/SecretError while the Secret is missing", Fn: s.checkSecretMissing},
		{ID: "taas.synced", Label: "one External WeftServer per active bastion; Secret pinned; status synced", Fn: s.checkSynced},
		{ID: "taas.weftserver-available", Label: "the mirrored WeftServer for the real weft server goes Available", Fn: s.checkWeftServerAvailable},
		{ID: "taas.token-cached", Label: "access token exchanged once at AUTHZ_ENDPOINT and the JWT reused", Fn: s.checkTokenCached},
		{ID: "taas.token-reexchange", Label: "a 401 drops the JWT: exactly one re-exchange and a successful retry", Fn: s.checkTokenReexchange},
		{ID: "taas.api-error", Label: "a failing bastion list gives Available=False/APIError, then recovers", Fn: s.checkAPIError},
		{ID: "taas.prune", Label: "a bastion removed upstream has its WeftServer pruned, and comes back", Fn: s.checkPrune},

		{ID: "dns.register", Label: "new DNSRecord: GET 404 then PUT with all active bastion ids; Registered", Fn: s.checkRegister},
		{ID: "dns.mismatched", Label: "lookup with other IPs gives Resolved=False/Mismatched, healing on requeue", Fn: s.checkMismatched},
		{ID: "dns.target-pinned", Label: "targetBastionIDs pins the PUT body; an unknown id gives UnknownBastion", Fn: s.checkTargetPinned},
		{ID: "dns.clobber", Label: "a pre-existing own domain is PATCHed and marked clobberedPreexisting", Fn: s.checkClobber},
		{ID: "dns.foreign", Label: "409 on PATCH gives ForeignOwned, or ExternallyManaged once it resolves", Fn: s.checkForeign},
		{ID: "dns.no-targets", Label: "all bastions suspended: PATCH clears bastion_ids, NoTargets; stray records UnexpectedRecords", Fn: s.checkNoTargets},
		{ID: "dns.domain-immutable", Label: "spec.domainName cannot be changed", Fn: s.checkDomainImmutable},
		{ID: "dns.delete", Label: "deleting sends DELETE and drops the finalizer, also for a foreign (403) domain", Fn: s.checkDelete},

		{ID: "gw.records", Label: "Gateway listener hostnames get fan-out DNSRecords in the TaaS namespace", Fn: s.checkGatewayRecords},
		{ID: "gw.l4-hostname-rejected", Label: "the API server rejects a hostname on a TCP listener (L4 pinning unreachable)", Fn: s.checkL4HostnameRejected},
		{ID: "gw.listener-pruned", Label: "removing a listener hostname deletes its DNSRecord and domain", Fn: s.checkListenerPruned},
		{ID: "gw.deleted", Label: "deleting the Gateway deletes its DNSRecords before it goes", Fn: s.checkGatewayDeleted},
		{ID: "gw.late", Label: "a second Gateway's record is live when the TaaS is deleted", Fn: s.checkLateGateway},

		{ID: "taas.secret-pinned", Label: "deleting the token Secret does not complete while the TaaS exists", Fn: s.checkSecretPinned},
		{ID: "taas.delete-order", Label: "TaaS delete: domains DELETEd, then bastions suspended, then Secret and TaaS gone, WeftServers GC'd", Fn: s.checkDeleteOrder},
		{ID: "gw.delete-without-taas", Label: "a Gateway deleted after its TaaS still goes away", Fn: s.checkGatewayDeleteWithoutTaaS},
	}
}

// The fake's bastions. b-1 has the node's IP and a real weft server;
// b-2's IP refuses connections; b-3 is suspended and b-4 has no IP, so
// neither may be mirrored.
func (s *scenario) bastions(suspendActive bool) []fakeBastion {
	return []fakeBastion{
		{ID: "b-1", Name: "bastion-a", IP: s.nodeIP, ConnectionSecret: bastionASecret, Suspended: suspendActive},
		{ID: "b-2", Name: "bastion-b", IP: "127.0.0.2", ConnectionSecret: "e2ebastionbsecret", Suspended: suspendActive},
		{ID: "b-3", Name: "bastion-c", IP: "192.0.2.3", ConnectionSecret: "e2ebastioncsecret", Suspended: true},
		{ID: "b-4", Name: "bastion-d", IP: "", ConnectionSecret: "e2ebastiondsecret"},
	}
}

var activeIDs = []string{"b-1", "b-2"}

// activeIPs are the expected A records of a fan-out record, sorted.
func (s *scenario) activeIPs() []string {
	ips := []string{s.nodeIP, "127.0.0.2"}
	sort.Strings(ips)
	return ips
}

func (s *scenario) installOperator(ctx context.Context) error {
	ip, err := s.k.NodeIP(ctx)
	if err != nil {
		return err
	}
	s.nodeIP = ip
	for _, ns := range []string{e2ekit.OperatorNS, fakeNS, taasNS, appsNS} {
		if err := s.k.C.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return s.k.InstallOperator(ctx, e2ekit.OperatorInstall{CRDs: gatewayAPICRDs, Manifest: operatorManifest})
}

func (s *scenario) deployFake(ctx context.Context) error {
	labels := map[string]string{"app": "fake-aquaduct"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: fakeNS, Name: "fake-aquaduct"},
		Spec: appsv1.DeploymentSpec{
			Replicas: e2ekit.Ptr(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: e2ekit.Ptr(false),
					Containers: []corev1.Container{{
						Name:            "fake",
						Image:           fakeImage,
						ImagePullPolicy: corev1.PullNever,
						Args:            []string{"fake", "--listen=:8080", "--access-token=" + accessToken},
						Ports:           []corev1.ContainerPort{{ContainerPort: 8080}},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/_fake/requests", Port: intstr.FromInt(8080)},
						}},
					}},
				},
			},
		},
	}
	if err := s.k.C.Create(ctx, dep); err != nil {
		return err
	}
	for _, name := range []string{"fake-aquaduct", "fake-authz"} {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: fakeNS, Name: name},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(8080)}},
			},
		}
		if err := s.k.C.Create(ctx, svc); err != nil {
			return err
		}
	}
	// A real weft server for bastion-a, on the node's IP like a bastion.
	weft := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: fakeNS, Name: "bastion-a-weft"},
		Spec: corev1.PodSpec{
			HostNetwork:                  true,
			AutomountServiceAccountToken: e2ekit.Ptr(false),
			Containers: []corev1.Container{{
				Name:            "weft",
				Image:           weftImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            []string{"server", "--connection-secret=" + bastionASecret, "--bind-ip=" + s.nodeIP, "--port=9092"},
			}},
		},
	}
	if err := s.k.C.Create(ctx, weft); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "fake aquaduct.dev available", s.k.DeploymentAvailable(fakeNS, "fake-aquaduct")); err != nil {
		return err
	}
	svc := &corev1.Service{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: fakeNS, Name: "fake-aquaduct"}, svc); err != nil {
		return err
	}
	s.fakeURL = "http://" + svc.Spec.ClusterIP
	if err := e2ekit.WaitFor(ctx, time.Minute, "fake reachable from the node", func() (bool, string, error) {
		_, err := s.requests(ctx)
		return err == nil, fmt.Sprint(err), nil
	}); err != nil {
		return err
	}
	if err := s.setBastions(ctx, s.bastions(false)); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "weft server running", func() (bool, string, error) {
		p := &corev1.Pod{}
		if err := s.k.C.Get(ctx, client.ObjectKeyFromObject(weft), p); err != nil {
			return false, "", err
		}
		return p.Status.Phase == corev1.PodRunning, string(p.Status.Phase) + " " + e2ekit.ContainerState(p), nil
	})
}

func (s *scenario) checkSecretMissing(ctx context.Context) error {
	taas := &weftv1alpha1.AquaductTaaS{
		ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: taasName},
		Spec: weftv1alpha1.AquaductTaaSSpec{
			AccessTokenSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  secretKey,
			},
			APIEndpoint: apiEndpoint,
		},
	}
	if err := s.k.C.Create(ctx, taas); err != nil {
		return err
	}
	t, err := s.taasAvailable(ctx, 2*time.Minute, metav1.ConditionFalse, "SecretError")
	if err != nil {
		return err
	}
	if !slices.Contains(t.Finalizers, taasFinalizer) {
		return fmt.Errorf("finalizers %v lack %s", t.Finalizers, taasFinalizer)
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	if len(reqs) != 0 {
		return fmt.Errorf("API called without a token: %s", describe(reqs))
	}
	return nil
}

func (s *scenario) checkSynced(ctx context.Context) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: secretName},
		Data:       map[string][]byte{secretKey: []byte(accessToken)},
	}
	if err := s.k.C.Create(ctx, secret); err != nil {
		return err
	}
	t, err := s.taasAvailable(ctx, 2*time.Minute, metav1.ConditionTrue, "Synced")
	if err != nil {
		return err
	}
	if err := s.k.C.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		return err
	}
	if !slices.Contains(secret.Finalizers, secretFinalizer) {
		return fmt.Errorf("secret finalizers %v lack %s", secret.Finalizers, secretFinalizer)
	}
	if want := []string{"bastion-a", "bastion-b"}; !slices.Equal(t.Status.SyncedServers, want) {
		return fmt.Errorf("syncedServers %v, want %v", t.Status.SyncedServers, want)
	}
	var ids []string
	for _, b := range t.Status.Bastions {
		ids = append(ids, b.ID)
		want := s.bastions(false)[len(ids)-1]
		if b.Name != want.Name || b.IP != want.IP || b.Suspended != want.Suspended {
			return fmt.Errorf("status.bastions[%d] %+v, want %+v", len(ids)-1, b, want)
		}
	}
	if !slices.Equal(ids, []string{"b-1", "b-2", "b-3", "b-4"}) {
		return fmt.Errorf("status.bastions ids %v, want all four in id order", ids)
	}
	if t.Status.LastSyncTime == nil {
		return fmt.Errorf("lastSyncTime not set")
	}
	return s.checkMirrored(ctx, t, map[string]fakeBastion{"bastion-a": s.bastions(false)[0], "bastion-b": s.bastions(false)[1]})
}

// checkMirrored checks the TaaS's WeftServers are exactly want, each a
// correct mirror.
func (s *scenario) checkMirrored(ctx context.Context, t *weftv1alpha1.AquaductTaaS, want map[string]fakeBastion) error {
	list := &weftv1alpha1.WeftServerList{}
	if err := s.k.C.List(ctx, list, client.InNamespace(taasNS), client.MatchingLabels{ownerLabel: taasName}); err != nil {
		return err
	}
	all := &weftv1alpha1.WeftServerList{}
	if err := s.k.C.List(ctx, all, client.InNamespace(taasNS)); err != nil {
		return err
	}
	if len(list.Items) != len(want) || len(all.Items) != len(want) {
		var names []string
		for _, ws := range all.Items {
			names = append(names, ws.Name)
		}
		return fmt.Errorf("WeftServers %v (%d labelled), want %d", names, len(list.Items), len(want))
	}
	for _, ws := range list.Items {
		b, ok := want[ws.Name]
		if !ok {
			return fmt.Errorf("unexpected WeftServer %s", ws.Name)
		}
		if ws.Annotations[bastionIDAnno] != b.ID {
			return fmt.Errorf("%s: bastion-id annotation %q, want %q", ws.Name, ws.Annotations[bastionIDAnno], b.ID)
		}
		if ws.Spec.Location != weftv1alpha1.WeftServerLocationExternal {
			return fmt.Errorf("%s: location %q, want External", ws.Name, ws.Spec.Location)
		}
		if cs := fmt.Sprintf("weft://%s@%s:9092", b.ConnectionSecret, b.IP); ws.Spec.ConnectionString != cs {
			return fmt.Errorf("%s: connectionString %q, want %q", ws.Name, ws.Spec.ConnectionString, cs)
		}
		ref := metav1.GetControllerOf(&ws)
		if ref == nil || ref.UID != t.UID || ref.Kind != "AquaductTaaS" {
			return fmt.Errorf("%s: controller ref %+v, want the AquaductTaaS", ws.Name, ref)
		}
	}
	return nil
}

func (s *scenario) checkWeftServerAvailable(ctx context.Context) error {
	if err := e2ekit.WaitFor(ctx, 3*time.Minute, "WeftServer bastion-a Available", func() (bool, string, error) {
		ws := &weftv1alpha1.WeftServer{}
		if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: taasNS, Name: "bastion-a"}, ws); err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(ws.Status.Conditions, "Available")
		state := e2ekit.CondString(c)
		if c != nil {
			state += " " + c.Message
		}
		return condIs(ws.Status.Conditions, "Available", metav1.ConditionTrue, ""), state, nil
	}); err != nil {
		return err
	}
	// The control: bastion-b's address refuses, so it must not be.
	return e2ekit.WaitFor(ctx, 2*time.Minute, "WeftServer bastion-b not Available", func() (bool, string, error) {
		ws := &weftv1alpha1.WeftServer{}
		if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: taasNS, Name: "bastion-b"}, ws); err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(ws.Status.Conditions, "Available")
		return condIs(ws.Status.Conditions, "Available", metav1.ConditionFalse, ""), e2ekit.CondString(c), nil
	})
}

// waitBastionList touches the TaaS and waits for a GET /api/bastion after
// mark that ended with status.
func (s *scenario) waitBastionList(ctx context.Context, mark, status int) ([]fakeRequest, error) {
	if err := s.touchTaaS(ctx); err != nil {
		return nil, err
	}
	return s.waitRequests(ctx, time.Minute, fmt.Sprintf("GET /api/bastion -> %d after #%d", status, mark), func(reqs []fakeRequest) (bool, string) {
		got := since(reqs, mark, "GET", bastionPath)
		for _, r := range got {
			if r.Status == status {
				return true, describe(got)
			}
		}
		return false, describe(got)
	})
}

func (s *scenario) checkTokenCached(ctx context.Context) error {
	for i := 0; i < 2; i++ {
		m, err := s.mark(ctx)
		if err != nil {
			return err
		}
		if _, err := s.waitBastionList(ctx, m, 200); err != nil {
			return err
		}
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	exchanges := since(reqs, 0, "POST", exchangePath)
	if len(exchanges) != 1 {
		return fmt.Errorf("%d token exchanges, want 1: %s", len(exchanges), describe(exchanges))
	}
	ex := exchanges[0]
	if ex.Host != authzHost || ex.Auth != accessToken || ex.Status != 200 {
		return fmt.Errorf("exchange #%d host=%s auth=%s status=%d, want host %s with the access token", ex.Seq, ex.Host, ex.Auth, ex.Status, authzHost)
	}
	lists := since(reqs, 0, "GET", bastionPath)
	if len(lists) < 3 {
		return fmt.Errorf("only %d bastion lists: %s", len(lists), describe(lists))
	}
	for _, r := range reqs {
		if r.Path == exchangePath {
			continue
		}
		if r.Host != apiHost || r.Auth != "jwt-1" || r.Status != 200 {
			return fmt.Errorf("#%d %s %s host=%s auth=%s status=%d, want host %s with the cached jwt-1", r.Seq, r.Method, r.Path, r.Host, r.Auth, r.Status, apiHost)
		}
	}
	log.Printf("%d bastion lists on one exchange", len(lists))
	return nil
}

func (s *scenario) checkTokenReexchange(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.fakeDo(ctx, "POST", "/_fake/revoke", nil, nil); err != nil {
		return err
	}
	if _, err := s.waitBastionList(ctx, m, 200); err != nil {
		return err
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	// A sync already in flight may still have used jwt-1 successfully
	// before the revoke; start at the first 401.
	got := since(reqs, m, "", "")
	for len(got) > 0 && got[0].Status == 200 && got[0].Auth == "jwt-1" {
		got = got[1:]
	}
	if len(got) < 3 {
		return fmt.Errorf("want 401, exchange, retry; got %s", describe(got))
	}
	first, ex, retry := got[0], got[1], got[2]
	if first.Path != bastionPath || first.Status != 401 || first.Auth != "jwt-1" {
		return fmt.Errorf("first call after revoke: %s auth=%s, want GET /api/bastion -> 401 with jwt-1", describe(got[:1]), first.Auth)
	}
	if ex.Path != exchangePath || ex.Status != 200 || ex.Host != authzHost {
		return fmt.Errorf("after the 401: %s, want the token exchange", describe(got[1:2]))
	}
	if retry.Path != bastionPath || retry.Status != 200 || retry.Auth != "jwt-2" {
		return fmt.Errorf("retry: %s auth=%s, want GET /api/bastion -> 200 with jwt-2", describe(got[2:3]), retry.Auth)
	}
	// The new JWT is cached again: another sync, still one exchange.
	m2, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if _, err := s.waitBastionList(ctx, m2, 200); err != nil {
		return err
	}
	reqs, err = s.requests(ctx)
	if err != nil {
		return err
	}
	if ex := since(reqs, m, "POST", exchangePath); len(ex) != 1 {
		return fmt.Errorf("%d exchanges after the revoke, want exactly 1: %s", len(ex), describe(ex))
	}
	for _, r := range since(reqs, retry.Seq, "", "") {
		if r.Auth != "jwt-2" || r.Status != 200 {
			return fmt.Errorf("#%d %s %s auth=%s status=%d after the re-exchange, want jwt-2 and 200", r.Seq, r.Method, r.Path, r.Auth, r.Status)
		}
	}
	return nil
}

func (s *scenario) checkAPIError(ctx context.Context) error {
	if err := s.fakeDo(ctx, "POST", "/_fake/fault", fakeFault{Method: "GET", Path: bastionPath, Status: 500, Count: -1}, nil); err != nil {
		return err
	}
	if err := s.touchTaaS(ctx); err != nil {
		return err
	}
	t, err := s.taasAvailable(ctx, time.Minute, metav1.ConditionFalse, "APIError")
	if err != nil {
		return err
	}
	if c := e2ekit.Condition(t.Status.Conditions, "Available"); !strings.Contains(c.Message, "500") {
		return fmt.Errorf("APIError message %q does not mention the 500", c.Message)
	}
	// Mirrors survive a failed list.
	if err := s.checkMirrored(ctx, t, map[string]fakeBastion{"bastion-a": s.bastions(false)[0], "bastion-b": s.bastions(false)[1]}); err != nil {
		return err
	}
	if err := s.fakeDo(ctx, "POST", "/_fake/fault", fakeFault{Method: "GET", Path: bastionPath, Count: 0}, nil); err != nil {
		return err
	}
	if err := s.touchTaaS(ctx); err != nil {
		return err
	}
	_, err = s.taasAvailable(ctx, time.Minute, metav1.ConditionTrue, "Synced")
	return err
}

// syncTo sets the fake's bastions, resyncs and waits for the TaaS to
// mirror exactly mirrored.
func (s *scenario) syncTo(ctx context.Context, bs []fakeBastion, mirrored ...string) (*weftv1alpha1.AquaductTaaS, error) {
	if err := s.setBastions(ctx, bs); err != nil {
		return nil, err
	}
	if err := s.touchTaaS(ctx); err != nil {
		return nil, err
	}
	var t *weftv1alpha1.AquaductTaaS
	err := e2ekit.WaitFor(ctx, 2*time.Minute, fmt.Sprintf("TaaS mirrors %v", mirrored), func() (bool, string, error) {
		var err error
		t, err = s.getTaaS(ctx)
		if err != nil {
			return false, "", err
		}
		list := &weftv1alpha1.WeftServerList{}
		if err := s.k.C.List(ctx, list, client.InNamespace(taasNS), client.MatchingLabels{ownerLabel: taasName}); err != nil {
			return false, "", err
		}
		var names []string
		for _, ws := range list.Items {
			names = append(names, ws.Name)
		}
		sort.Strings(names)
		ok := slices.Equal(t.Status.SyncedServers, mirrored) && slices.Equal(names, mirrored) && len(t.Status.Bastions) == len(bs)
		return ok, fmt.Sprintf("syncedServers=%v weftservers=%v bastions=%d", t.Status.SyncedServers, names, len(t.Status.Bastions)), nil
	})
	return t, err
}

func (s *scenario) checkPrune(ctx context.Context) error {
	all := s.bastions(false)
	without := []fakeBastion{all[0], all[2], all[3]}
	if _, err := s.syncTo(ctx, without, "bastion-a"); err != nil {
		return err
	}
	t, err := s.syncTo(ctx, all, "bastion-a", "bastion-b")
	if err != nil {
		return err
	}
	return s.checkMirrored(ctx, t, map[string]fakeBastion{"bastion-a": all[0], "bastion-b": all[1]})
}

// diagnostics prints the fake's request log and the weft objects.
func (s *scenario) diagnostics(ctx context.Context) {
	if s.fakeURL != "" {
		if reqs, err := s.requests(ctx); err == nil {
			for _, r := range reqs {
				log.Printf("fake #%d %s %s?%s host=%s auth=%s body=%s -> %d", r.Seq, r.Method, r.Path, r.Query, r.Host, r.Auth, r.Body, r.Status)
			}
		}
		if ds, err := s.fakeDomains(ctx); err == nil {
			log.Printf("fake domains: %+v", ds)
		}
	}
	if t, err := s.getTaaS(ctx); err == nil {
		log.Printf("taas: finalizers=%v deletion=%v status=%+v", t.Finalizers, t.DeletionTimestamp, t.Status)
	}
	drs := &weftv1alpha1.DNSRecordList{}
	if err := s.k.C.List(ctx, drs); err == nil {
		for _, dr := range drs.Items {
			log.Printf("dnsrecord %s/%s domain=%s targets=%v finalizers=%v deletion=%v %s expected=%v resolved=%v",
				dr.Namespace, dr.Name, dr.Spec.DomainName, dr.Spec.TargetBastionIDs, dr.Finalizers, dr.DeletionTimestamp, recordConds(&dr), dr.Status.ExpectedIPs, dr.Status.ResolvedIPs)
		}
	}
	wss := &weftv1alpha1.WeftServerList{}
	if err := s.k.C.List(ctx, wss); err == nil {
		for _, ws := range wss.Items {
			log.Printf("weftserver %s/%s %s conds=%v", ws.Namespace, ws.Name, ws.Spec.ConnectionString, ws.Status.Conditions)
		}
	}
}
