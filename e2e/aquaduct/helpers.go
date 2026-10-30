// helpers.go — the scenario's handle on the fake API (its /_fake control
// endpoints, reached through the Service's ClusterIP from the node) and
// small Kubernetes helpers shared by the steps.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

// fakeDo calls the fake's control API: in (if non-nil) is the JSON body,
// out (if non-nil) receives the JSON response.
func (s *scenario) fakeDo(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, s.fakeURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fake %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fake %s %s: %d %s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// requests returns the fake's request log.
func (s *scenario) requests(ctx context.Context) ([]fakeRequest, error) {
	var reqs []fakeRequest
	err := s.fakeDo(ctx, http.MethodGet, "/_fake/requests", nil, &reqs)
	return reqs, err
}

// mark returns the current end of the request log, for "since" filters.
func (s *scenario) mark(ctx context.Context) (int, error) {
	reqs, err := s.requests(ctx)
	return len(reqs), err
}

// since returns the logged requests after seq that match method (empty:
// any) and path (exact).
func since(reqs []fakeRequest, seq int, method, path string) []fakeRequest {
	var out []fakeRequest
	for _, r := range reqs {
		if r.Seq <= seq || (method != "" && r.Method != method) || (path != "" && r.Path != path) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// waitRequests polls the request log until cond is satisfied.
func (s *scenario) waitRequests(ctx context.Context, timeout time.Duration, what string, cond func([]fakeRequest) (bool, string)) ([]fakeRequest, error) {
	var last []fakeRequest
	err := e2ekit.WaitFor(ctx, timeout, what, func() (bool, string, error) {
		reqs, err := s.requests(ctx)
		if err != nil {
			return false, err.Error(), nil
		}
		last = reqs
		ok, state := cond(reqs)
		return ok, state, nil
	})
	return last, err
}

func describe(reqs []fakeRequest) string {
	var b strings.Builder
	for _, r := range reqs {
		fmt.Fprintf(&b, "#%d %s %s -> %d; ", r.Seq, r.Method, r.Path, r.Status)
	}
	return b.String()
}

// bodyBastionIDs decodes a PUT/PATCH body's bastion_ids. present is false
// when the field is absent or null (the server then keeps its current set).
func bodyBastionIDs(body string) (ids []string, present bool, err error) {
	var p struct {
		BastionIDs *[]string `json:"bastion_ids"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, false, err
	}
	if p.BastionIDs == nil {
		return nil, false, nil
	}
	return *p.BastionIDs, true, nil
}

// expectBody checks that r's body carries exactly want as bastion_ids.
func expectBody(r fakeRequest, want []string) error {
	ids, present, err := bodyBastionIDs(r.Body)
	if err != nil {
		return fmt.Errorf("#%d %s %s: body %q: %w", r.Seq, r.Method, r.Path, r.Body, err)
	}
	if !present {
		return fmt.Errorf("#%d %s %s: bastion_ids absent or null in %s, want %v", r.Seq, r.Method, r.Path, r.Body, want)
	}
	if !slices.Equal(ids, want) {
		return fmt.Errorf("#%d %s %s: bastion_ids %v, want %v", r.Seq, r.Method, r.Path, ids, want)
	}
	return nil
}

// touch bumps an annotation, which re-enqueues the object's controller
// (none of these controllers filter on generation). It is how a user
// forces an AquaductTaaS resync or a DNSRecord re-check before the
// 5-minute resync.
func (s *scenario) touch(ctx context.Context, obj client.Object) error {
	s.touches++
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"e2e.aquaduct.dev/touch":"%d"}}}`, s.touches)
	if err := s.k.C.Patch(ctx, obj, client.RawPatch(types.MergePatchType, []byte(patch))); err != nil {
		return fmt.Errorf("touch %s: %w", obj.GetName(), err)
	}
	log.Printf("touched %T %s/%s (%d)", obj, obj.GetNamespace(), obj.GetName(), s.touches)
	return nil
}

func (s *scenario) touchTaaS(ctx context.Context) error {
	return s.touch(ctx, &weftv1alpha1.AquaductTaaS{ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: taasName}})
}

func (s *scenario) touchRecord(ctx context.Context, name string) error {
	return s.touch(ctx, &weftv1alpha1.DNSRecord{ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: name}})
}

func (s *scenario) getTaaS(ctx context.Context) (*weftv1alpha1.AquaductTaaS, error) {
	t := &weftv1alpha1.AquaductTaaS{}
	err := s.k.C.Get(ctx, types.NamespacedName{Namespace: taasNS, Name: taasName}, t)
	return t, err
}

func (s *scenario) getRecord(ctx context.Context, name string) (*weftv1alpha1.DNSRecord, error) {
	dr := &weftv1alpha1.DNSRecord{}
	err := s.k.C.Get(ctx, types.NamespacedName{Namespace: taasNS, Name: name}, dr)
	return dr, err
}

// condIs reports whether condition t has status st and (if non-empty) reason.
func condIs(conds []metav1.Condition, t string, st metav1.ConditionStatus, reason string) bool {
	c := e2ekit.Condition(conds, t)
	return c != nil && c.Status == st && (reason == "" || c.Reason == reason)
}

// taasAvailable waits for the TaaS's Available condition.
func (s *scenario) taasAvailable(ctx context.Context, timeout time.Duration, st metav1.ConditionStatus, reason string) (*weftv1alpha1.AquaductTaaS, error) {
	var t *weftv1alpha1.AquaductTaaS
	err := e2ekit.WaitFor(ctx, timeout, fmt.Sprintf("AquaductTaaS Available=%s/%s", st, reason), func() (bool, string, error) {
		var err error
		t, err = s.getTaaS(ctx)
		if err != nil {
			return false, "", err
		}
		c := e2ekit.Condition(t.Status.Conditions, "Available")
		state := e2ekit.CondString(c)
		if c != nil {
			state += " " + c.Message
		}
		return condIs(t.Status.Conditions, "Available", st, reason), state, nil
	})
	return t, err
}

// recordConds is a DNSRecord's Registered/Resolved/Ready summary.
func recordConds(dr *weftv1alpha1.DNSRecord) string {
	var parts []string
	for _, t := range []string{"Registered", "Resolved", "Ready"} {
		c := e2ekit.Condition(dr.Status.Conditions, t)
		parts = append(parts, e2ekit.CondString(c))
		if c != nil && c.Status == metav1.ConditionFalse {
			parts[len(parts)-1] += "(" + c.Message + ")"
		}
	}
	return strings.Join(parts, " ")
}

// waitRecord waits until check accepts the DNSRecord.
func (s *scenario) waitRecord(ctx context.Context, timeout time.Duration, name, what string, check func(*weftv1alpha1.DNSRecord) bool) (*weftv1alpha1.DNSRecord, error) {
	var dr *weftv1alpha1.DNSRecord
	err := e2ekit.WaitFor(ctx, timeout, "DNSRecord "+name+" "+what, func() (bool, string, error) {
		var err error
		dr, err = s.getRecord(ctx, name)
		if err != nil {
			return false, err.Error(), client.IgnoreNotFound(err)
		}
		return check(dr), recordConds(dr), nil
	})
	return dr, err
}

func readyRecord(dr *weftv1alpha1.DNSRecord) bool {
	return condIs(dr.Status.Conditions, "Ready", metav1.ConditionTrue, "Ready")
}

// setBastions replaces the fake's bastion list.
func (s *scenario) setBastions(ctx context.Context, bs []fakeBastion) error {
	return s.fakeDo(ctx, http.MethodPost, "/_fake/bastions", bs, nil)
}

// setLookup overrides (ips non-nil) or restores (nil) a domain's lookup.
func (s *scenario) setLookup(ctx context.Context, domain string, ips []string) error {
	return s.fakeDo(ctx, http.MethodPost, "/_fake/lookup", lookupOverride{Domain: domain, IPs: ips}, nil)
}

func (s *scenario) fakeDomains(ctx context.Context) (map[string]fakeDomain, error) {
	var ds map[string]fakeDomain
	err := s.fakeDo(ctx, http.MethodGet, "/_fake/domains", nil, &ds)
	return ds, err
}
