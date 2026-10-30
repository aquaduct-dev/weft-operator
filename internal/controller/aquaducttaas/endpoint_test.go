package aquaducttaas_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
)

// fakeAPI answers the token exchange and records every other request path.
type fakeAPI struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newFakeAPI(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *fakeAPI {
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/access-token" {
			_, _ = w.Write([]byte(`{"access_token":"jwt","expires_in":3600}`))
			return
		}
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// AquaductTaaS.spec.apiEndpoint reaches the shared client through the
// context, and overrides the client's default endpoint for that call only.
func TestWithEndpointOverridesTheDefault(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }
	def := newFakeAPI(t, ok)
	override := newFakeAPI(t, ok)
	c := aquaducttaas.NewHTTPAPIClient(def.URL, def.URL)

	if _, err := c.ListExternalServers(aquaducttaas.WithEndpoint(context.Background(), override.URL+"/"), "tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListExternalServers(aquaducttaas.WithEndpoint(context.Background(), ""), "tok"); err != nil {
		t.Fatal(err)
	}
	if got := override.requests(); len(got) != 1 || got[0] != "GET /api/bastion" {
		t.Errorf("override endpoint got %v, want one GET /api/bastion (trailing slash trimmed)", got)
	}
	if got := def.requests(); len(got) != 1 {
		t.Errorf("default endpoint got %v, want exactly the call with an empty override", got)
	}
}

// A 2xx with no body (204 No Content) from PUT or PATCH is a success.
func TestDomainWritesAcceptAnEmptyBody(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	c := aquaducttaas.NewHTTPAPIClient(api.URL, api.URL)
	ids := []string{"b1"}

	d, err := c.PutDomain(context.Background(), "tok", "a.example.com", &ids)
	if err != nil {
		t.Fatalf("PutDomain with 204: %v", err)
	}
	if d.Name != "a.example.com" {
		t.Errorf("PutDomain name = %q, want it synthesized from the request", d.Name)
	}
	if _, err := c.PatchDomain(context.Background(), "tok", "a.example.com", &ids); err != nil {
		t.Fatalf("PatchDomain with 204: %v", err)
	}
}

// aquaduct.dev owner-checks GET /domain/{name}: another user's domain is a
// 403, which must read as foreign-owned rather than a transient API error.
func TestGetDomainForbiddenIsForeign(t *testing.T) {
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "forbidden", http.StatusForbidden) })
	c := aquaducttaas.NewHTTPAPIClient(api.URL, api.URL)
	if _, err := c.GetDomain(context.Background(), "tok", "theirs.example.com"); !errors.Is(err, aquaducttaas.ErrDomainForeign) {
		t.Fatalf("GetDomain on a 403 = %v, want ErrDomainForeign", err)
	}
}
