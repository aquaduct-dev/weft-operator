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
	// ID is the server-assigned identifier. Stored on DNSRecord status so
	// operators can correlate cluster records to server-side entries.
	ID string
	// Name is the fully-qualified domain. Always equal to the path
	// parameter for calls that include a {domain} segment; included on
	// the struct so a future List endpoint can return populated Name.
	Name string
}

// DomainAPIClient is a distinct interface from APIClient because the
// DNSRecord reconciler is an independent consumer — it should be
// fakeable without having to stub out bastion methods.
type DomainAPIClient interface {
	// GetDomain returns the record for `name` or ErrDomainNotFound on 404.
	// Any other error (network, 5xx, auth) is surfaced to the caller so
	// the reconciler can retry and surface a meaningful status.
	GetDomain(ctx context.Context, token, name string) (*Domain, error)

	// PutDomain registers `name`. Idempotent — the server treats a PUT
	// on an existing record as an update. The caller is allowed to PUT
	// over a record they didn't originally create ("clobber"), but is
	// expected to DeleteDomain on teardown regardless.
	PutDomain(ctx context.Context, token, name string) (*Domain, error)

	// PatchDomain asserts our write access over an existing `name`. The
	// request body is minimal on purpose — we don't know our own user_id
	// locally, so we let the server's auth layer decide: a 2xx means we
	// own (or just took over) the record; a 401/403/409 means someone
	// else holds it, surfaced as ErrDomainForeign so the reconciler can
	// set a ForeignOwned condition. A 404 between GET and PATCH is a
	// race — the record was deleted under us — surfaced as
	// ErrDomainNotFound so the caller can retry the create path.
	PatchDomain(ctx context.Context, token, name string) (*Domain, error)

	// DeleteDomain removes `name`. A 404 is treated as success (the
	// record is already gone, which is the goal). All other errors are
	// returned so the reconciler keeps the finalizer and retries.
	DeleteDomain(ctx context.Context, token, name string) error

	// LookupDomain hits GET /domain/lookup?domain={name} and returns the
	// A records. Empty slice with nil error means "looked up but no
	// records found" — which is a transient condition, not an error.
	LookupDomain(ctx context.Context, token, name string) ([]string, error)
}

// apiDomain is the wire type for /domain endpoints.
type apiDomain struct {
	ID         string `json:"id"`
	DomainName string `json:"domain_name"`
	UserID     string `json:"user_id,omitempty"`
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
	return &Domain{ID: d.ID, Name: d.DomainName}, nil
}

func (c *HTTPAPIClient) PutDomain(ctx context.Context, token, name string) (*Domain, error) {
	body, err := json.Marshal(apiDomain{DomainName: name})
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
	return &Domain{ID: d.ID, Name: d.DomainName}, nil
}

func (c *HTTPAPIClient) PatchDomain(ctx context.Context, token, name string) (*Domain, error) {
	// The server's PATCH accepts {id, domain_name, user_id}. We send
	// only domain_name: we don't know our own user_id, and the id is
	// the path parameter. Effectively a "confirm I can write" probe —
	// the server resolves the caller's identity from the JWT and
	// either accepts or rejects based on current ownership.
	body, err := json.Marshal(apiDomain{DomainName: name})
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
	return &Domain{ID: d.ID, Name: d.DomainName}, nil
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
