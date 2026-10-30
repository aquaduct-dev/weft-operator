// scenario_gateway.go — the Gateway AutoDNS steps and the AquaductTaaS
// deletion order of the "aquaduct" scenario.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

const gatewayClass = "weft-gateway-class"

// recordName is the operator's DNSRecord name for a Gateway hostname.
func recordName(gw, host string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s", appsNS, gw, host)))
	return fmt.Sprintf("gw-%s-%s", gw, hex.EncodeToString(h[:])[:10])
}

func listener(name string, proto gatewayv1.ProtocolType, port int32, host string) gatewayv1.Listener {
	l := gatewayv1.Listener{Name: gatewayv1.SectionName(name), Protocol: proto, Port: gatewayv1.PortNumber(port)}
	if host != "" {
		h := gatewayv1.Hostname(host)
		l.Hostname = &h
	}
	return l
}

func gateway(name string, listeners ...gatewayv1.Listener) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: appsNS, Name: name},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: gatewayClass, Listeners: listeners},
	}
}

// checkGatewayRecord waits for the fan-out DNSRecord of gw's host to be
// Ready and checks how the Gateway stamped it.
func (s *scenario) checkGatewayRecord(ctx context.Context, mark int, gw, host string) error {
	name := recordName(gw, host)
	dr, err := s.waitRecord(ctx, 2*time.Minute, name, "Ready", readyRecord)
	if err != nil {
		return err
	}
	if dr.Spec.DomainName != host || dr.Spec.AquaductTaaSRef.Name != taasName || dr.Spec.TargetBastionIDs != nil {
		return fmt.Errorf("%s spec %+v, want domain %s, taas %s, no targets", name, dr.Spec, host, taasName)
	}
	want := map[string]string{
		"weft.aquaduct.dev/gateway-namespace": appsNS,
		"weft.aquaduct.dev/gateway-name":      gw,
		"created-by":                          "weft-operator",
	}
	for k, v := range want {
		if dr.Labels[k] != v {
			return fmt.Errorf("%s label %s=%q, want %q", name, k, dr.Labels[k], v)
		}
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	return checkRegistered(reqs, mark, host, activeIDs)
}

func (s *scenario) checkGatewayRecords(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	gw := gateway("web",
		listener("http-a", gatewayv1.HTTPProtocolType, 80, "a.gw.e2e.test"),
		listener("http-b", gatewayv1.HTTPProtocolType, 8080, "b.gw.e2e.test"),
		listener("no-host", gatewayv1.HTTPProtocolType, 8081, ""))
	if err := s.k.C.Create(ctx, gw); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, time.Minute, "Gateway web has the DNS cleanup finalizer", func() (bool, string, error) {
		if err := s.k.C.Get(ctx, client.ObjectKeyFromObject(gw), gw); err != nil {
			return false, "", err
		}
		return slices.Contains(gw.Finalizers, gatewayFinalizer), fmt.Sprint(gw.Finalizers), nil
	}); err != nil {
		return err
	}
	for _, host := range []string{"a.gw.e2e.test", "b.gw.e2e.test"} {
		if err := s.checkGatewayRecord(ctx, m, "web", host); err != nil {
			return err
		}
	}
	return s.gatewayRecordCount(ctx, "web", 2)
}

// gatewayRecordCount checks how many DNSRecords carry gw's labels.
func (s *scenario) gatewayRecordCount(ctx context.Context, gw string, want int) error {
	list := &weftv1alpha1.DNSRecordList{}
	if err := s.k.C.List(ctx, list, client.MatchingLabels{"weft.aquaduct.dev/gateway-name": gw}); err != nil {
		return err
	}
	if len(list.Items) != want {
		var names []string
		for _, dr := range list.Items {
			names = append(names, dr.Namespace+"/"+dr.Name)
		}
		return fmt.Errorf("Gateway %s has DNSRecords %v, want %d", gw, names, want)
	}
	return nil
}

func (s *scenario) checkL4HostnameRejected(ctx context.Context) error {
	gw := gateway("l4", listener("tcp", gatewayv1.TCPProtocolType, 5432, "db.gw.e2e.test"))
	err := s.k.C.Create(ctx, gw, client.DryRunAll)
	if err == nil {
		return fmt.Errorf("a TCP listener with a hostname was accepted; the L4 bastion pinning path is now reachable and needs its own test")
	}
	if !strings.Contains(err.Error(), "hostname must not be specified") {
		return fmt.Errorf("rejected for another reason: %w", err)
	}
	return nil
}

func (s *scenario) checkListenerPruned(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	gw := &gatewayv1.Gateway{}
	if err := s.k.C.Get(ctx, types.NamespacedName{Namespace: appsNS, Name: "web"}, gw); err != nil {
		return err
	}
	gw.Spec.Listeners = slices.DeleteFunc(gw.Spec.Listeners, func(l gatewayv1.Listener) bool { return l.Name == "http-b" })
	if err := s.k.C.Update(ctx, gw); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "b.gw.e2e.test record gone", s.k.Gone(&weftv1alpha1.DNSRecord{}, taasNS, recordName("web", "b.gw.e2e.test"))); err != nil {
		return err
	}
	if _, err := s.waitRequests(ctx, 30*time.Second, "DELETE b.gw.e2e.test", func(reqs []fakeRequest) (bool, string) {
		d := since(reqs, m, "DELETE", domainPath("b.gw.e2e.test"))
		return len(d) == 1 && d[0].Status == 204, describe(d)
	}); err != nil {
		return err
	}
	if _, err := s.getRecord(ctx, recordName("web", "a.gw.e2e.test")); err != nil {
		return fmt.Errorf("a.gw.e2e.test record: %w", err)
	}
	return s.gatewayRecordCount(ctx, "web", 1)
}

func (s *scenario) checkGatewayDeleted(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.k.C.Delete(ctx, gateway("web")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "Gateway web gone", s.k.Gone(&gatewayv1.Gateway{}, appsNS, "web")); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "a.gw.e2e.test record gone", s.k.Gone(&weftv1alpha1.DNSRecord{}, taasNS, recordName("web", "a.gw.e2e.test"))); err != nil {
		return err
	}
	_, err = s.waitRequests(ctx, 30*time.Second, "DELETE a.gw.e2e.test", func(reqs []fakeRequest) (bool, string) {
		d := since(reqs, m, "DELETE", domainPath("a.gw.e2e.test"))
		return len(d) == 1 && d[0].Status == 204, describe(d)
	})
	return err
}

func (s *scenario) checkLateGateway(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.k.C.Create(ctx, gateway("late", listener("http", gatewayv1.HTTPProtocolType, 80, "late.gw.e2e.test"))); err != nil {
		return err
	}
	return s.checkGatewayRecord(ctx, m, "late", "late.gw.e2e.test")
}

func (s *scenario) checkSecretPinned(ctx context.Context) error {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: secretName}}
	if err := s.k.C.Delete(ctx, secret); err != nil {
		return err
	}
	return e2ekit.Holds(ctx, 10*time.Second, "token Secret held by its finalizer", func() (bool, string, error) {
		if err := s.k.C.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
			return false, err.Error(), nil
		}
		return secret.DeletionTimestamp != nil && slices.Contains(secret.Finalizers, secretFinalizer),
			fmt.Sprintf("deletion=%v finalizers=%v", secret.DeletionTimestamp, secret.Finalizers), nil
	})
}

func (s *scenario) checkDeleteOrder(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.k.C.Delete(ctx, &weftv1alpha1.AquaductTaaS{ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: taasName}}); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 4*time.Minute, "AquaductTaaS gone", s.k.Gone(&weftv1alpha1.AquaductTaaS{}, taasNS, taasName)); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 30*time.Second, "token Secret gone", s.k.Gone(&corev1.Secret{}, taasNS, secretName)); err != nil {
		return err
	}
	if err := e2ekit.WaitFor(ctx, 2*time.Minute, "mirrored WeftServers garbage-collected", func() (bool, string, error) {
		list := &weftv1alpha1.WeftServerList{}
		if err := s.k.C.List(ctx, list, client.InNamespace(taasNS)); err != nil {
			return false, "", err
		}
		return len(list.Items) == 0, fmt.Sprintf("%d left", len(list.Items)), nil
	}); err != nil {
		return err
	}
	// Nothing may bring a record back once the TaaS is gone: it could
	// never be unregistered.
	if err := e2ekit.Holds(ctx, 15*time.Second, "no DNSRecords left", func() (bool, string, error) {
		list := &weftv1alpha1.DNSRecordList{}
		if err := s.k.C.List(ctx, list, client.InNamespace(taasNS)); err != nil {
			return false, "", err
		}
		var names []string
		for _, dr := range list.Items {
			names = append(names, dr.Name)
		}
		return len(names) == 0, fmt.Sprint(names), nil
	}); err != nil {
		return err
	}

	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	got := since(reqs, m, "", "")
	var deleted, suspended []string
	lastDelete, firstSuspend := 0, 0
	for _, r := range got {
		switch {
		case r.Method == "DELETE" && strings.HasPrefix(r.Path, "/api/domain/"):
			if r.Status != 204 {
				return fmt.Errorf("#%d DELETE %s -> %d", r.Seq, r.Path, r.Status)
			}
			deleted = append(deleted, strings.TrimPrefix(r.Path, "/api/domain/"))
			lastDelete = r.Seq
		case r.Method == "PATCH" && strings.HasPrefix(r.Path, "/api/bastion/"):
			var body struct{ Suspended *bool }
			if err := json.Unmarshal([]byte(r.Body), &body); err != nil || body.Suspended == nil || !*body.Suspended || r.Status != 200 {
				return fmt.Errorf("#%d PATCH %s body=%s -> %d, want suspended=true -> 200", r.Seq, r.Path, r.Body, r.Status)
			}
			suspended = append(suspended, strings.TrimPrefix(r.Path, "/api/bastion/"))
			if firstSuspend == 0 {
				firstSuspend = r.Seq
			}
		case (r.Method == "PUT" || r.Method == "PATCH") && strings.HasPrefix(r.Path, "/api/domain/") && firstSuspend != 0:
			return fmt.Errorf("#%d %s %s after bastions were suspended", r.Seq, r.Method, r.Path)
		}
	}
	sort.Strings(deleted)
	sort.Strings(suspended)
	if want := []string{"app.e2e.test", "late.gw.e2e.test", "pre.e2e.test"}; !slices.Equal(deleted, want) {
		return fmt.Errorf("domains DELETEd %v, want %v; log: %s", deleted, want, describe(got))
	}
	if !slices.Equal(suspended, activeIDs) {
		return fmt.Errorf("bastions suspended %v, want %v; log: %s", suspended, activeIDs, describe(got))
	}
	if lastDelete > firstSuspend {
		return fmt.Errorf("domain DELETE #%d after the first suspend #%d: %s", lastDelete, firstSuspend, describe(got))
	}
	ds, err := s.fakeDomains(ctx)
	if err != nil {
		return err
	}
	for name, d := range ds {
		if d.Owner == meOwner {
			return fmt.Errorf("own domain %s still registered", name)
		}
	}
	return nil
}

func (s *scenario) checkGatewayDeleteWithoutTaaS(ctx context.Context) error {
	if err := s.k.C.Delete(ctx, gateway("late")); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "Gateway late gone", s.k.Gone(&gatewayv1.Gateway{}, appsNS, "late"))
}
