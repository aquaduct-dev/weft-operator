/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aquaducttaas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ErrDomainNotFound is returned by DomainAPIClient.GetDomain when
// aquaduct.dev reports the record doesn't exist. The DNSRecord reconciler
// uses this to decide between PUT (create) and an unexpected transport
// error — a regular error means "can't tell, try again later", a
// not-found means "safe to create".
var ErrDomainNotFound = errors.New("aquaduct.dev: domain not found")

// ErrDomainForeign is returned by PatchDomain when the server refuses
// the write because the record is owned by a different user (403/401)
// or a concurrent writer won a conflict (409). The reconciler treats
// this as a durable condition to surface on status — retrying won't
// help without operator intervention, but we still requeue so a later
// ownership change (e.g. the other party releases the record) heals
// automatically.
var ErrDomainForeign = errors.New("aquaduct.dev: domain is owned by a different user")

// Domain mirrors the database.Domain shape from the aquaduct.dev OpenAPI
// spec. Only fields consumed by the reconciler are modeled here; the
// server may return additional fields (timestamps, metadata) that this
// decoder silently ignores.
type Domain struct {
	// ID is the server-assigned identifier as a decimal string. The wire
	// format is int64; converting to string at this boundary keeps
	// DNSRecord status fields stringly-typed and avoids leaking the
	// numeric form to operators reading kubectl describe output.
	ID string
	// Name is the fully-qualified domain. Always equal to the path
	// parameter for calls that include a {domain} segment; included on
	// the struct so a future List endpoint can return populated Name.
	Name string
	// BastionIDs are the bastions the server has bound this domain to.
	// On PUT/PATCH this is the set the server actually applied (which
	// may equal what the caller asked for, or — when the caller omitted
	// the field — the full fan-out set the server picked).
	BastionIDs []string
	// IPs are the resolved A-record IPs for those bastions, returned by
	// the server as a debugging convenience. Empty when no bastions are
	// associated; not authoritative DNS state (use LookupDomain for
	// what the world actually sees).
	IPs []string
}

// DomainAPIClient is a distinct interface from APIClient because the
// DNSRecord reconciler is an independent consumer — it should be
// fakeable without having to stub out bastion methods.
//
// The bastionIDs parameter on Put/Patch follows the server's tri-state
// PATCH convention:
//   - nil          -> field omitted; server preserves existing value (PATCH)
//                     or applies "fan out to all caller's bastions" (PUT).
//   - &[]          -> explicit empty list; server clears all bastion bindings
//                     and tears down the cloudflare records.
//   - &[ids...]    -> explicit list; server applies exactly these bastions.
type DomainAPIClient interface {
	// GetDomain returns the record for `name` or ErrDomainNotFound on 404.
	// Any other error (network, 5xx, auth) is surfaced to the caller so
	// the reconciler can retry and surface a meaningful status.
	GetDomain(ctx context.Context, token, name string) (*Domain, error)

	// PutDomain registers `name` with the requested bastion association.
	// Idempotent — the server treats a PUT on an existing record as an
	// update. The caller is allowed to PUT over a record they didn't
	// originally create ("clobber"), but is expected to DeleteDomain on
	// teardown regardless.
	PutDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*Domain, error)

	// PatchDomain asserts our write access over an existing `name` and
	// optionally updates the bastion association. With bastionIDs nil,
	// the request is a "confirm I can write" probe (no field changes);
	// the server's auth layer decides: a 2xx means we own (or just took
	// over) the record; a 401/403/409 means someone else holds it,
	// surfaced as ErrDomainForeign so the reconciler can set a
	// ForeignOwned condition. A 404 between GET and PATCH is a race —
	// the record was deleted under us — surfaced as ErrDomainNotFound
	// so the caller can retry the create path.
	PatchDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*Domain, error)

	// DeleteDomain removes `name`. A 404 is treated as success (the
	// record is already gone, which is the goal). All other errors are
	// returned so the reconciler keeps the finalizer and retries.
	DeleteDomain(ctx context.Context, token, name string) error

	// LookupDomain hits GET /domain/lookup?domain={name} and returns the
	// A records. Empty slice with nil error means "looked up but no
	// records found" — which is a transient condition, not an error.
	LookupDomain(ctx context.Context, token, name string) ([]string, error)
}

// apiDomain is the response wire type for /domain endpoints. Mirrors
// database.Domain on the server side as of PR #3 — id is int64, and
// bastion_ids/ips are populated after the server applies cloudflare
// state.
type apiDomain struct {
	ID         int64    `json:"id"`
	DomainName string   `json:"domain_name"`
	UserID     string   `json:"user_id,omitempty"`
	BastionIDs []string `json:"bastion_ids"`
	IPs        []string `json:"ips,omitempty"`
}

// apiDomainPayload is the request wire type for PUT/PATCH. Distinct from
// apiDomain because BastionIDs needs pointer semantics to encode the
// nil-vs-empty distinction on PATCH (preserve existing vs explicitly
// clear). domain_name and bastion_ids are the only writable fields.
type apiDomainPayload struct {
	DomainName string    `json:"domain_name,omitempty"`
	BastionIDs *[]string `json:"bastion_ids,omitempty"`
}

// toDomain converts the wire format to the operator-facing Domain. ID
// is int64 on the wire; we render it as a decimal string so it can drop
// straight into status.domainID (a string field) without further coercion.
func (a *apiDomain) toDomain() *Domain {
	return &Domain{
		ID:         strconv.FormatInt(a.ID, 10),
		Name:       a.DomainName,
		BastionIDs: a.BastionIDs,
		IPs:        a.IPs,
	}
}

// ipLookupResponse is the wire type for GET /domain/lookup.
// The server returns an object with an ips array, per the openapi spec's
// domain.IPAddressResponse schema.
type ipLookupResponse struct {
	IPs []string `json:"ips"`
}

func (c *HTTPAPIClient) GetDomain(ctx context.Context, token, name string) (*Domain, error) {
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+apiBasePath+"/domain/"+url.PathEscape(name), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrDomainNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aquaduct.dev API returned %d getting domain %q: %s", resp.StatusCode, name, string(body))
	}

	var d apiDomain
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode domain response: %w", err)
	}
	return d.toDomain(), nil
}

func (c *HTTPAPIClient) PutDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*Domain, error) {
	body, err := json.Marshal(apiDomainPayload{DomainName: name, BastionIDs: bastionIDs})
	if err != nil {
		return nil, err
	}
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.Endpoint+apiBasePath+"/domain/"+url.PathEscape(name), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aquaduct.dev API returned %d creating domain %q: %s", resp.StatusCode, name, string(respBody))
	}

	var d apiDomain
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode domain response: %w", err)
	}
	// Some servers return 204 No Content on PUT; if the body was empty
	// but status was 2xx, synthesize the name from the request so callers
	// still get a populated Domain.
	if d.DomainName == "" {
		d.DomainName = name
	}
	return d.toDomain(), nil
}

func (c *HTTPAPIClient) PatchDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*Domain, error) {
	// With bastionIDs nil, the body carries only domain_name — that's
	// effectively a "confirm I can write" probe, since domain_name on
	// the path is also the primary key. The server resolves the
	// caller's identity from the JWT and either accepts or rejects
	// based on current ownership; with bastionIDs non-nil it also
	// re-applies the cloudflare records to match.
	body, err := json.Marshal(apiDomainPayload{DomainName: name, BastionIDs: bastionIDs})
	if err != nil {
		return nil, err
	}
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.Endpoint+apiBasePath+"/domain/"+url.PathEscape(name), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrDomainNotFound
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict:
		// authedRequest already retried once on 401 (stale JWT). If we
		// still get here, the server genuinely refuses the write —
		// either the record is owned by someone else or the token
		// doesn't have permission to claim it.
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w (status %d): %s", ErrDomainForeign, resp.StatusCode, string(respBody))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aquaduct.dev API returned %d patching domain %q: %s", resp.StatusCode, name, string(respBody))
	}

	var d apiDomain
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode domain response: %w", err)
	}
	if d.DomainName == "" {
		d.DomainName = name
	}
	return d.toDomain(), nil
}

func (c *HTTPAPIClient) DeleteDomain(ctx context.Context, token, name string) error {
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.Endpoint+apiBasePath+"/domain/"+url.PathEscape(name), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	// 401/403/409 on DELETE means the record is foreign-owned. The
	// finalizer's job is "leave the world clean of our claims" — and
	// if the record was never ours to begin with (externally-managed
	// case), that's already satisfied. Surface it as ErrDomainForeign
	// so the caller can distinguish from a transient error and skip
	// to finalizer removal.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusConflict {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w (status %d): %s", ErrDomainForeign, resp.StatusCode, string(respBody))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aquaduct.dev API returned %d deleting domain %q: %s", resp.StatusCode, name, string(respBody))
	}
	return nil
}

func (c *HTTPAPIClient) LookupDomain(ctx context.Context, token, name string) ([]string, error) {
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		u := c.Endpoint + apiBasePath + "/domain/lookup?domain=" + url.QueryEscape(name)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aquaduct.dev API returned %d looking up %q: %s", resp.StatusCode, name, string(body))
	}

	var r ipLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode lookup response: %w", err)
	}
	return r.IPs, nil
}
