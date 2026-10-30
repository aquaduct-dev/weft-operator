// scenario_dns.go — the DNSRecord steps of the "aquaduct" scenario.
package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

func domainPath(domain string) string { return "/api/domain/" + domain }

func (s *scenario) createRecord(ctx context.Context, name, domain string, targets []string) error {
	return s.k.C.Create(ctx, &weftv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{Namespace: taasNS, Name: name},
		Spec: weftv1alpha1.DNSRecordSpec{
			DomainName:       domain,
			AquaductTaaSRef:  corev1.LocalObjectReference{Name: taasName},
			TargetBastionIDs: targets,
		},
	})
}

// checkRegistered checks the first calls for domain after mark were
// GET -> 404 then PUT -> 201 with bastion_ids want.
func checkRegistered(reqs []fakeRequest, mark int, domain string, want []string) error {
	got := since(reqs, mark, "", domainPath(domain))
	if len(got) < 2 || got[0].Method != "GET" || got[0].Status != 404 || got[1].Method != "PUT" || got[1].Status != 201 {
		return fmt.Errorf("%s: want GET -> 404 then PUT -> 201, got %s", domain, describe(got))
	}
	return expectBody(got[1], want)
}

func (s *scenario) checkRegister(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.createRecord(ctx, "app", "app.e2e.test", nil); err != nil {
		return err
	}
	dr, err := s.waitRecord(ctx, 2*time.Minute, "app", "Registered and Ready", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionTrue, "Registered") && readyRecord(dr)
	})
	if err != nil {
		return err
	}
	if !slices.Contains(dr.Finalizers, recordFinalizer) {
		return fmt.Errorf("finalizers %v lack %s", dr.Finalizers, recordFinalizer)
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	if err := checkRegistered(reqs, m, "app.e2e.test", activeIDs); err != nil {
		return err
	}
	if lk := since(reqs, m, "GET", "/api/domain/lookup"); len(lk) == 0 || lk[0].Query != "domain=app.e2e.test" {
		return fmt.Errorf("no lookup for app.e2e.test: %s", describe(lk))
	}
	ds, err := s.fakeDomains(ctx)
	if err != nil {
		return err
	}
	d, ok := ds["app.e2e.test"]
	if !ok || d.Owner != meOwner {
		return fmt.Errorf("fake has no own app.e2e.test: %+v", ds)
	}
	if dr.Status.DomainID != fmt.Sprint(d.ID) {
		return fmt.Errorf("status.domainID %q, fake id %d", dr.Status.DomainID, d.ID)
	}
	if !slices.Equal(dr.Status.AppliedBastionIDs, activeIDs) {
		return fmt.Errorf("appliedBastionIDs %v, want %v", dr.Status.AppliedBastionIDs, activeIDs)
	}
	want := s.activeIPs()
	if !slices.Equal(dr.Status.ExpectedIPs, want) || !slices.Equal(dr.Status.ResolvedIPs, want) {
		return fmt.Errorf("expectedIPs %v resolvedIPs %v, want both %v", dr.Status.ExpectedIPs, dr.Status.ResolvedIPs, want)
	}
	if dr.Status.ClobberedPreexisting {
		return fmt.Errorf("a new domain was marked clobberedPreexisting")
	}
	return nil
}

func (s *scenario) checkMismatched(ctx context.Context) error {
	if err := s.setLookup(ctx, "app.e2e.test", []string{"203.0.113.9"}); err != nil {
		return err
	}
	if err := s.touchRecord(ctx, "app"); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, time.Minute, "app", "Mismatched", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Resolved", metav1.ConditionFalse, "Mismatched") &&
			condIs(dr.Status.Conditions, "Ready", metav1.ConditionFalse, "Mismatched") &&
			condIs(dr.Status.Conditions, "Registered", metav1.ConditionTrue, "") &&
			slices.Equal(dr.Status.ResolvedIPs, []string{"203.0.113.9"})
	}); err != nil {
		return err
	}
	// Once DNS is right again the 30s not-ready requeue heals it, no touch.
	if err := s.setLookup(ctx, "app.e2e.test", nil); err != nil {
		return err
	}
	_, err := s.waitRecord(ctx, 2*time.Minute, "app", "Ready again on its own", readyRecord)
	return err
}

func (s *scenario) checkTargetPinned(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.createRecord(ctx, "pinned", "pinned.e2e.test", []string{"b-2"}); err != nil {
		return err
	}
	dr, err := s.waitRecord(ctx, 2*time.Minute, "pinned", "Ready", readyRecord)
	if err != nil {
		return err
	}
	if !slices.Equal(dr.Status.ExpectedIPs, []string{"127.0.0.2"}) {
		return fmt.Errorf("expectedIPs %v, want [127.0.0.2]", dr.Status.ExpectedIPs)
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	if err := checkRegistered(reqs, m, "pinned.e2e.test", []string{"b-2"}); err != nil {
		return err
	}

	if err := s.createRecord(ctx, "unknown", "unknown.e2e.test", []string{"b-404"}); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, time.Minute, "unknown", "UnknownBastion", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionFalse, "UnknownBastion") &&
			condIs(dr.Status.Conditions, "Ready", metav1.ConditionFalse, "UnknownBastion")
	}); err != nil {
		return err
	}
	if reqs, err = s.requests(ctx); err != nil {
		return err
	}
	if got := since(reqs, m, "", domainPath("unknown.e2e.test")); len(got) != 0 {
		return fmt.Errorf("unknown target still called the API: %s", describe(got))
	}
	dr = &weftv1alpha1.DNSRecord{}
	dr.Namespace, dr.Name = taasNS, "unknown"
	if err := s.k.C.Delete(ctx, dr); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, time.Minute, "DNSRecord unknown gone", s.k.Gone(&weftv1alpha1.DNSRecord{}, taasNS, "unknown"))
}

func (s *scenario) seedDomain(ctx context.Context, domain, owner string, ids []string) error {
	return s.fakeDo(ctx, "POST", "/_fake/domains", fakeDomain{DomainName: domain, Owner: owner, BastionIDs: ids}, nil)
}

func (s *scenario) checkClobber(ctx context.Context) error {
	if err := s.seedDomain(ctx, "pre.e2e.test", meOwner, []string{"b-2"}); err != nil {
		return err
	}
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.createRecord(ctx, "pre", "pre.e2e.test", nil); err != nil {
		return err
	}
	dr, err := s.waitRecord(ctx, 2*time.Minute, "pre", "Ready and clobberedPreexisting", func(dr *weftv1alpha1.DNSRecord) bool {
		return readyRecord(dr) && dr.Status.ClobberedPreexisting
	})
	if err != nil {
		return err
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	got := since(reqs, m, "", domainPath("pre.e2e.test"))
	if len(got) < 2 || got[0].Method != "GET" || got[0].Status != 200 || got[1].Method != "PATCH" || got[1].Status != 200 {
		return fmt.Errorf("want GET -> 200 then PATCH -> 200, got %s", describe(got))
	}
	if puts := since(reqs, m, "PUT", domainPath("pre.e2e.test")); len(puts) != 0 {
		return fmt.Errorf("pre-existing domain was PUT: %s", describe(puts))
	}
	if err := expectBody(got[1], activeIDs); err != nil {
		return err
	}
	ds, err := s.fakeDomains(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(ds["pre.e2e.test"].BastionIDs, activeIDs) {
		return fmt.Errorf("fake pre.e2e.test bastions %v, want %v", ds["pre.e2e.test"].BastionIDs, activeIDs)
	}
	if dr.Status.DomainID != fmt.Sprint(ds["pre.e2e.test"].ID) {
		return fmt.Errorf("domainID %q, want the seeded %d", dr.Status.DomainID, ds["pre.e2e.test"].ID)
	}
	return nil
}

func (s *scenario) checkForeign(ctx context.Context) error {
	// Someone else's domain: aquaduct.dev refuses even the GET (403).
	if err := s.seedDomain(ctx, "foreign.e2e.test", "someone-else", []string{}); err != nil {
		return err
	}
	if err := s.setLookup(ctx, "foreign.e2e.test", []string{"198.51.100.7"}); err != nil {
		return err
	}
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.createRecord(ctx, "foreign", "foreign.e2e.test", nil); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, 2*time.Minute, "foreign", "ForeignOwned", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionFalse, "ForeignOwned") &&
			condIs(dr.Status.Conditions, "Ready", metav1.ConditionFalse, "Mismatched")
	}); err != nil {
		return err
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	got := since(reqs, m, "", domainPath("foreign.e2e.test"))
	if len(got) == 0 || got[0].Method != "GET" || got[0].Status != 403 {
		return fmt.Errorf("want GET -> 403, got %s", describe(got))
	}
	for _, r := range got {
		if r.Method == "PUT" || r.Method == "PATCH" {
			return fmt.Errorf("wrote to a foreign domain: %s", describe(got))
		}
	}
	// ... that already points at our bastions.
	if err := s.setLookup(ctx, "foreign.e2e.test", s.activeIPs()); err != nil {
		return err
	}
	if _, err = s.waitRecord(ctx, 2*time.Minute, "foreign", "ExternallyManaged and Ready", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionFalse, "ExternallyManaged") && readyRecord(dr)
	}); err != nil {
		return err
	}

	// A domain we can read but someone else wins the write to (409).
	if err := s.seedDomain(ctx, "contested.e2e.test", meOwner, activeIDs); err != nil {
		return err
	}
	if err := s.fakeDo(ctx, "POST", "/_fake/fault", fakeFault{Method: "PATCH", Path: domainPath("contested.e2e.test"), Status: 409, Count: -1}, nil); err != nil {
		return err
	}
	if err := s.setLookup(ctx, "contested.e2e.test", []string{"198.51.100.8"}); err != nil {
		return err
	}
	if m, err = s.mark(ctx); err != nil {
		return err
	}
	if err := s.createRecord(ctx, "contested", "contested.e2e.test", nil); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, 2*time.Minute, "contested", "ForeignOwned after a 409", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionFalse, "ForeignOwned")
	}); err != nil {
		return err
	}
	if reqs, err = s.requests(ctx); err != nil {
		return err
	}
	got = since(reqs, m, "", domainPath("contested.e2e.test"))
	if len(got) < 2 || got[0].Method != "GET" || got[0].Status != 200 || got[1].Method != "PATCH" || got[1].Status != 409 {
		return fmt.Errorf("want GET -> 200 then PATCH -> 409, got %s", describe(got))
	}
	// The other writer goes away: the 30s requeue takes the record back.
	if err := s.fakeDo(ctx, "POST", "/_fake/fault", fakeFault{Method: "PATCH", Path: domainPath("contested.e2e.test"), Count: 0}, nil); err != nil {
		return err
	}
	if err := s.setLookup(ctx, "contested.e2e.test", nil); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, 2*time.Minute, "contested", "Registered again", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Registered", metav1.ConditionTrue, "Registered") && readyRecord(dr)
	}); err != nil {
		return err
	}
	return s.deleteRecord(ctx, "contested")
}

func (s *scenario) checkNoTargets(ctx context.Context) error {
	if _, err := s.syncTo(ctx, s.bastions(true)); err != nil {
		return err
	}
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.touchRecord(ctx, "app"); err != nil {
		return err
	}
	dr, err := s.waitRecord(ctx, time.Minute, "app", "Resolved=True/NoTargets", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Resolved", metav1.ConditionTrue, "NoTargets") && readyRecord(dr)
	})
	if err != nil {
		return err
	}
	if len(dr.Status.ExpectedIPs) != 0 {
		return fmt.Errorf("expectedIPs %v with every bastion suspended", dr.Status.ExpectedIPs)
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	patches := since(reqs, m, "PATCH", domainPath("app.e2e.test"))
	if len(patches) == 0 {
		return fmt.Errorf("no PATCH for app.e2e.test after the suspension")
	}
	// An absent/null bastion_ids would make the server keep pointing the
	// domain at the suspended bastions; it has to be an explicit [].
	if err := expectBody(patches[0], []string{}); err != nil {
		return err
	}

	// Records left behind with nothing to point at.
	if err := s.setLookup(ctx, "app.e2e.test", []string{s.nodeIP}); err != nil {
		return err
	}
	if err := s.touchRecord(ctx, "app"); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, time.Minute, "app", "UnexpectedRecords", func(dr *weftv1alpha1.DNSRecord) bool {
		return condIs(dr.Status.Conditions, "Resolved", metav1.ConditionFalse, "UnexpectedRecords") &&
			condIs(dr.Status.Conditions, "Ready", metav1.ConditionFalse, "UnexpectedRecords")
	}); err != nil {
		return err
	}

	// Bastions back: the record fans out to them again.
	if err := s.setLookup(ctx, "app.e2e.test", nil); err != nil {
		return err
	}
	if _, err := s.syncTo(ctx, s.bastions(false), "bastion-a", "bastion-b"); err != nil {
		return err
	}
	if m, err = s.mark(ctx); err != nil {
		return err
	}
	if err := s.touchRecord(ctx, "app"); err != nil {
		return err
	}
	if _, err := s.waitRecord(ctx, time.Minute, "app", "Ready with both bastions", func(dr *weftv1alpha1.DNSRecord) bool {
		return readyRecord(dr) && slices.Equal(dr.Status.ExpectedIPs, s.activeIPs())
	}); err != nil {
		return err
	}
	if reqs, err = s.requests(ctx); err != nil {
		return err
	}
	patches = since(reqs, m, "PATCH", domainPath("app.e2e.test"))
	if len(patches) == 0 {
		return fmt.Errorf("no PATCH for app.e2e.test after the bastions returned")
	}
	return expectBody(patches[0], activeIDs)
}

func (s *scenario) checkDomainImmutable(ctx context.Context) error {
	dr, err := s.getRecord(ctx, "app")
	if err != nil {
		return err
	}
	dr.Spec.DomainName = "renamed.e2e.test"
	err = s.k.C.Update(ctx, dr)
	if err == nil {
		return fmt.Errorf("spec.domainName update was accepted")
	}
	if !strings.Contains(err.Error(), "domainName is immutable") {
		return fmt.Errorf("update rejected for another reason: %w", err)
	}
	return nil
}

// deleteRecord deletes a DNSRecord and waits for it to go.
func (s *scenario) deleteRecord(ctx context.Context, name string) error {
	dr := &weftv1alpha1.DNSRecord{}
	dr.Namespace, dr.Name = taasNS, name
	if err := s.k.C.Delete(ctx, dr); err != nil {
		return err
	}
	return e2ekit.WaitFor(ctx, 2*time.Minute, "DNSRecord "+name+" gone", s.k.Gone(&weftv1alpha1.DNSRecord{}, taasNS, name))
}

func (s *scenario) checkDelete(ctx context.Context) error {
	m, err := s.mark(ctx)
	if err != nil {
		return err
	}
	if err := s.deleteRecord(ctx, "pinned"); err != nil {
		return err
	}
	if err := s.deleteRecord(ctx, "foreign"); err != nil {
		return err
	}
	reqs, err := s.requests(ctx)
	if err != nil {
		return err
	}
	if d := since(reqs, m, "DELETE", domainPath("pinned.e2e.test")); len(d) != 1 || d[0].Status != 204 {
		return fmt.Errorf("pinned: want one DELETE -> 204, got %s", describe(d))
	}
	if d := since(reqs, m, "DELETE", domainPath("foreign.e2e.test")); len(d) != 1 || d[0].Status != 403 {
		return fmt.Errorf("foreign: want one DELETE -> 403, got %s", describe(d))
	}
	ds, err := s.fakeDomains(ctx)
	if err != nil {
		return err
	}
	if _, ok := ds["pinned.e2e.test"]; ok {
		return fmt.Errorf("pinned.e2e.test still registered")
	}
	if d, ok := ds["foreign.e2e.test"]; !ok || d.Owner == meOwner {
		return fmt.Errorf("the foreign domain changed: %+v", d)
	}
	return nil
}
