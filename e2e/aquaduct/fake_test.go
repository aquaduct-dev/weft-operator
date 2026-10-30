package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"testing"

	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
)

// TestFakeSpeaksTheClientsProtocol runs the operator's real HTTP client
// against the fake, so a VM run cannot fail on a fake that disagrees with
// the client about the wire format.
func TestFakeSpeaksTheClientsProtocol(t *testing.T) {
	f := &fake{accessToken: "tok", validJWTs: map[string]bool{}, domains: map[string]*fakeDomain{}, lookups: map[string][]string{}}
	f.bastions = []fakeBastion{
		{ID: "b-1", Name: "a", IP: "10.0.0.1", ConnectionSecret: "s1"},
		{ID: "b-2", Name: "b", IP: "10.0.0.2", ConnectionSecret: "s2", Suspended: true},
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := aquaducttaas.NewHTTPAPIClient(srv.URL, srv.URL)
	ctx := context.Background()

	if _, err := c.ListExternalServers(ctx, "wrong"); err == nil {
		t.Fatal("a wrong access token was accepted")
	}
	servers, err := c.ListExternalServers(ctx, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ConnectionString != "weft://s1@10.0.0.1:9092" || !servers[1].Suspended {
		t.Fatalf("servers %+v", servers)
	}
	if _, err := c.GetDomain(ctx, "tok", "x.test"); !errors.Is(err, aquaducttaas.ErrDomainNotFound) {
		t.Fatalf("GetDomain on a missing domain: %v", err)
	}
	ids := []string{"b-1"}
	d, err := c.PutDomain(ctx, "tok", "x.test", &ids)
	if err != nil || !slices.Equal(d.BastionIDs, ids) {
		t.Fatalf("PutDomain: %+v %v", d, err)
	}
	ips, err := c.LookupDomain(ctx, "tok", "x.test")
	if err != nil || !slices.Equal(ips, []string{"10.0.0.1"}) {
		t.Fatalf("LookupDomain: %v %v", ips, err)
	}
	// [] fans out to every non-suspended bastion; null keeps the set.
	f.domains["x.test"].BastionIDs = []string{"b-2"}
	empty := []string{}
	if d, err = c.PatchDomain(ctx, "tok", "x.test", &empty); err != nil || !slices.Equal(d.BastionIDs, []string{"b-1"}) {
		t.Fatalf("PatchDomain to []: %+v %v", d, err)
	}
	var none []string
	f.domains["x.test"].BastionIDs = []string{"b-2"}
	if d, err = c.PatchDomain(ctx, "tok", "x.test", &none); err != nil || !slices.Equal(d.BastionIDs, []string{"b-2"}) {
		t.Fatalf("PatchDomain with null: %+v %v", d, err)
	}

	f.domains["theirs.test"] = &fakeDomain{ID: 1, DomainName: "theirs.test", Owner: "other"}
	if _, err := c.GetDomain(ctx, "tok", "theirs.test"); !errors.Is(err, aquaducttaas.ErrDomainForeign) {
		t.Fatalf("GetDomain on a foreign domain: %v", err)
	}
	if _, err := c.PatchDomain(ctx, "tok", "theirs.test", &ids); !errors.Is(err, aquaducttaas.ErrDomainForeign) {
		t.Fatalf("PatchDomain on a foreign domain: %v", err)
	}
	if err := c.DeleteDomain(ctx, "tok", "theirs.test"); !errors.Is(err, aquaducttaas.ErrDomainForeign) {
		t.Fatalf("DeleteDomain on a foreign domain: %v", err)
	}
	if err := c.DeleteDomain(ctx, "tok", "x.test"); err != nil {
		t.Fatal(err)
	}

	// Revoked JWTs: one re-exchange, then success.
	f.validJWTs = map[string]bool{}
	if err := c.SuspendServer(ctx, "tok", "b-1"); err != nil {
		t.Fatal(err)
	}
	if !f.bastions[0].Suspended {
		t.Fatal("b-1 not suspended")
	}
	var exchanges int
	for _, r := range f.log {
		if r.Path == "/api/auth/access-token" && r.Status == 200 {
			exchanges++
		}
	}
	if exchanges != 2 {
		t.Fatalf("%d successful exchanges, want 2", exchanges)
	}
}
