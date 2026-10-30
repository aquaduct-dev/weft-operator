// fake.go — the "fake" mode: an in-memory stand-in for aquaduct.dev (the
// /api/bastion and /api/domain endpoints) and its authz issuer (the
// /api/auth/access-token exchange), served over plain HTTP from a pod.
//
// It behaves like the real service where the operator can tell: API calls
// need a JWT this fake minted (anything else is a 401), the exchange needs
// the configured access token, PUT/PATCH bodies decode bastion_ids with Go's
// pointer-slice semantics (absent or null = keep the current set, [] = fan
// out to every non-suspended bastion), and a domain owned by another user
// is refused (403) to GET, PATCH and DELETE, as the real handlers in
// aquaduct-dev's domain package do. A 409 on PATCH (a concurrent writer)
// can be injected as a fault.
//
// /_fake/... is the scenario's control and introspection API: replace the
// bastion list, seed domains, override lookups, inject faults, revoke every
// minted JWT, and read the ordered request log. Every request is also
// printed to stdout.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// fakeBastion is the /api/bastion wire shape.
type fakeBastion struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	IP               string `json:"ip"`
	ConnectionSecret string `json:"connection_secret"`
	Suspended        bool   `json:"suspended"`
}

// fakeDomain is a registered domain. Owner "me" is the access token's user;
// anything else is foreign.
type fakeDomain struct {
	ID         int64    `json:"id"`
	DomainName string   `json:"domain_name"`
	Owner      string   `json:"owner"`
	BastionIDs []string `json:"bastion_ids"`
}

// fakeRequest is one entry of the request log.
type fakeRequest struct {
	Seq    int    `json:"seq"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query,omitempty"`
	Host   string `json:"host"`
	Auth   string `json:"auth,omitempty"`
	Body   string `json:"body,omitempty"`
	Status int    `json:"status"`
}

// fakeFault makes the next Count requests matching Method+Path answer
// Status (Count < 0: until cleared).
type fakeFault struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Count  int    `json:"count"`
}

// lookupOverride sets (or, with IPs nil, clears) what /api/domain/lookup
// returns for Domain.
type lookupOverride struct {
	Domain string   `json:"domain"`
	IPs    []string `json:"ips"`
}

const meOwner = "me"

type fake struct {
	accessToken string

	mu        sync.Mutex
	jwtSeq    int
	validJWTs map[string]bool
	bastions  []fakeBastion
	domains   map[string]*fakeDomain
	nextID    int64
	lookups   map[string][]string
	faults    []*fakeFault
	log       []fakeRequest
}

func runFake(args []string) int {
	fs := flag.NewFlagSet("fake", flag.ExitOnError)
	addr := fs.String("listen", ":8080", "listen address")
	token := fs.String("access-token", "", "the only access token the exchange accepts")
	_ = fs.Parse(args)
	f := &fake{
		accessToken: *token,
		validJWTs:   map[string]bool{},
		domains:     map[string]*fakeDomain{},
		nextID:      100,
		lookups:     map[string][]string{},
	}
	log.Printf("fake aquaduct.dev listening on %s", *addr)
	srv := &http.Server{Addr: *addr, Handler: f, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("fake: %v", err)
		return 1
	}
	return 0
}

// recorder captures the status code a handler wrote.
type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	if strings.HasPrefix(r.URL.Path, "/_fake/") {
		f.control(rec, r, body)
		log.Printf("control %s %s -> %d", r.Method, r.URL.Path, rec.status)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.api(rec, r, body)
	entry := fakeRequest{
		Seq:    len(f.log) + 1,
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Host:   r.Host,
		Auth:   strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		Body:   string(body),
		Status: rec.status,
	}
	f.log = append(f.log, entry)
	log.Printf("#%d %s %s?%s host=%s auth=%s body=%s -> %d",
		entry.Seq, entry.Method, entry.Path, entry.Query, entry.Host, entry.Auth, entry.Body, entry.Status)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// api serves the aquaduct.dev and authz endpoints. Caller holds f.mu.
func (f *fake) api(w http.ResponseWriter, r *http.Request, body []byte) {
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	path := r.URL.Path

	if path == "/api/auth/access-token" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST only")
			return
		}
		if bearer == "" || bearer != f.accessToken {
			writeErr(w, http.StatusUnauthorized, "unknown access token")
			return
		}
		f.jwtSeq++
		jwt := fmt.Sprintf("jwt-%d", f.jwtSeq)
		f.validJWTs[jwt] = true
		writeJSON(w, http.StatusOK, map[string]any{"access_token": jwt, "token": jwt, "expires_in": 3600})
		return
	}

	if !f.validJWTs[bearer] {
		writeErr(w, http.StatusUnauthorized, "invalid or expired JWT")
		return
	}
	for _, flt := range f.faults {
		if flt.Count != 0 && flt.Method == r.Method && flt.Path == path {
			if flt.Count > 0 {
				flt.Count--
			}
			writeErr(w, flt.Status, "injected fault")
			return
		}
	}

	switch {
	case path == "/api/bastion" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, f.bastions)

	case strings.HasPrefix(path, "/api/bastion/") && r.Method == http.MethodPatch:
		id := strings.TrimPrefix(path, "/api/bastion/")
		var req struct {
			Suspended *bool `json:"suspended"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		for i := range f.bastions {
			if f.bastions[i].ID == id {
				if req.Suspended != nil {
					f.bastions[i].Suspended = *req.Suspended
				}
				writeJSON(w, http.StatusOK, f.bastions[i])
				return
			}
		}
		writeErr(w, http.StatusNotFound, "no such bastion")

	case path == "/api/domain/lookup" && r.Method == http.MethodGet:
		name := r.URL.Query().Get("domain")
		ips, ok := f.lookups[name]
		if !ok {
			ips = f.derivedIPs(name)
		}
		if ips == nil {
			ips = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ips": ips})

	case strings.HasPrefix(path, "/api/domain/"):
		f.domain(w, r, strings.TrimPrefix(path, "/api/domain/"), body)

	default:
		writeErr(w, http.StatusNotFound, "no such endpoint")
	}
}

// derivedIPs is what DNS would hold for a domain: the IPs of the bastions
// it is bound to. Caller holds f.mu.
func (f *fake) derivedIPs(name string) []string {
	d, ok := f.domains[name]
	if !ok {
		return nil
	}
	var ips []string
	for _, id := range d.BastionIDs {
		for _, b := range f.bastions {
			if b.ID == id && b.IP != "" {
				ips = append(ips, b.IP)
			}
		}
	}
	slices.Sort(ips)
	return ips
}

// resolveIDs is the set a PUT/PATCH applies: the ids asked for, or every
// non-suspended bastion for an empty list. Caller holds f.mu.
func (f *fake) resolveIDs(ids []string) []string {
	if len(ids) == 0 {
		return f.activeIDs()
	}
	return append([]string{}, ids...)
}

// activeIDs is the server's default fan-out set: every non-suspended
// bastion, with or without an IP. Caller holds f.mu.
func (f *fake) activeIDs() []string {
	ids := []string{}
	for _, b := range f.bastions {
		if !b.Suspended {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

func (f *fake) domainJSON(d *fakeDomain) map[string]any {
	ids := d.BastionIDs
	if ids == nil {
		ids = []string{}
	}
	return map[string]any{
		"id":          d.ID,
		"domain_name": d.DomainName,
		"user_id":     d.Owner,
		"bastion_ids": ids,
		"ips":         f.derivedIPs(d.DomainName),
	}
}

// domain serves GET/PUT/PATCH/DELETE /api/domain/{name}. Caller holds f.mu.
func (f *fake) domain(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	d, exists := f.domains[name]
	// Pointer semantics, like the real (Go) server: absent or null keeps
	// the existing set, [] clears it.
	var payload struct {
		DomainName string    `json:"domain_name"`
		BastionIDs *[]string `json:"bastion_ids"`
	}
	if r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if err := json.Unmarshal(body, &payload); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	switch r.Method {
	case http.MethodGet:
		if !exists {
			writeErr(w, http.StatusNotFound, "domain not found")
			return
		}
		if d.Owner != meOwner {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		writeJSON(w, http.StatusOK, f.domainJSON(d))
	case http.MethodPut:
		if exists && d.Owner != meOwner {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if !exists {
			f.nextID++
			d = &fakeDomain{ID: f.nextID, DomainName: name, Owner: meOwner}
			f.domains[name] = d
		}
		var ids []string
		if payload.BastionIDs != nil {
			ids = *payload.BastionIDs
		}
		d.BastionIDs = f.resolveIDs(ids)
		writeJSON(w, http.StatusCreated, f.domainJSON(d))
	case http.MethodPatch:
		if !exists {
			writeErr(w, http.StatusNotFound, "domain not found")
			return
		}
		if d.Owner != meOwner {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if payload.BastionIDs != nil {
			d.BastionIDs = f.resolveIDs(*payload.BastionIDs)
		}
		writeJSON(w, http.StatusOK, f.domainJSON(d))
	case http.MethodDelete:
		if !exists {
			writeErr(w, http.StatusNotFound, "domain not found")
			return
		}
		if d.Owner != meOwner {
			writeErr(w, http.StatusForbidden, "domain owned by another user")
			return
		}
		delete(f.domains, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "unsupported method")
	}
}

// control serves /_fake/...; these calls are not logged as API requests.
func (f *fake) control(w http.ResponseWriter, r *http.Request, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	decode := func(v any) bool {
		if err := json.Unmarshal(body, v); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return false
		}
		return true
	}
	switch r.URL.Path {
	case "/_fake/requests":
		writeJSON(w, http.StatusOK, f.log)
	case "/_fake/bastions":
		if r.Method == http.MethodPost {
			var bs []fakeBastion
			if !decode(&bs) {
				return
			}
			f.bastions = bs
		}
		writeJSON(w, http.StatusOK, f.bastions)
	case "/_fake/domains":
		if r.Method == http.MethodPost {
			var d fakeDomain
			if !decode(&d) {
				return
			}
			f.nextID++
			d.ID = f.nextID
			f.domains[d.DomainName] = &d
		}
		writeJSON(w, http.StatusOK, f.domains)
	case "/_fake/lookup":
		var o lookupOverride
		if !decode(&o) {
			return
		}
		if o.IPs == nil {
			delete(f.lookups, o.Domain)
		} else {
			f.lookups[o.Domain] = o.IPs
		}
		writeJSON(w, http.StatusOK, f.lookups)
	case "/_fake/fault":
		var flt fakeFault
		if !decode(&flt) {
			return
		}
		f.faults = slices.DeleteFunc(f.faults, func(x *fakeFault) bool {
			return x.Method == flt.Method && x.Path == flt.Path
		})
		if flt.Count != 0 {
			f.faults = append(f.faults, &flt)
		}
		writeJSON(w, http.StatusOK, f.faults)
	case "/_fake/revoke":
		f.validJWTs = map[string]bool{}
		writeJSON(w, http.StatusOK, map[string]string{"revoked": "all"})
	default:
		writeErr(w, http.StatusNotFound, "no such control endpoint")
	}
}
