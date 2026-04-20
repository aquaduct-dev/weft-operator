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
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	// DefaultAPIEndpoint is used when AquaductTaaS.Spec.APIEndpoint is empty.
	// The aquaduct.dev OpenAPI spec (published at
	// https://aquaduct.dev/api/openapi.json) has basePath `/api`, so all
	// requests go to {Endpoint}/api/...
	DefaultAPIEndpoint = "https://aquaduct.dev"
	apiBasePath        = "/api"

	// defaultBastionPort is appended when we build the weft:// connection
	// string from the IP + connection_secret fields aquaduct.dev returns.
	// The OpenAPI spec doesn't expose a port; 8080 matches the WeftServer
	// reconciler's default bind port.
	defaultBastionPort = 8080
)

// ExternalServer describes a cloud-hosted bastion that aquaduct.dev has
// provisioned on the user's behalf. Each one is mirrored into the cluster as a
// WeftServer with Location=External.
type ExternalServer struct {
	// ID is the opaque identifier aquaduct.dev uses in API paths (e.g. for
	// suspend). Store it on the mirrored WeftServer as an annotation so the
	// deletion path can find it without re-listing.
	ID string

	// Name is the human-friendly name; used as the WeftServer object name.
	Name string

	// ConnectionString is weft://<connection_secret>@<ip>:<port>, ready to
	// drop straight into WeftServer.Spec.ConnectionString.
	ConnectionString string

	// Suspended mirrors database.Bastion.suspended. Currently informational;
	// the reconciler still materializes a WeftServer for suspended bastions
	// so the rest of the cluster's view stays consistent.
	Suspended bool
}

// APIClient is the subset of the aquaduct.dev API that the reconciler needs.
// A minimal interface keeps tests simple and lets the operator swap in a
// fake for development without a real aquaduct.dev account.
type APIClient interface {
	// ListExternalServers returns every cloud-hosted WeftServer the caller's
	// token has access to. The token is the long-lived access token from the
	// AquaductTaaS Secret; the implementation exchanges it for a short-lived
	// JWT via /api/login before calling the bastion endpoints.
	ListExternalServers(ctx context.Context, token string) ([]ExternalServer, error)

	// SuspendServer pauses the bastion identified by `id` via
	// PATCH /api/bastion/{id} with `{"suspended": true}`. Must be idempotent
	// so a retry after a partial failure re-suspends already-suspended
	// servers without error.
	SuspendServer(ctx context.Context, token, id string) error
}

// HTTPAPIClient is the default production APIClient. It talks to the
// aquaduct.dev REST API described in https://aquaduct.dev/api/openapi.json.
//
// Authentication is a two-step exchange: the long-lived access token from the
// AquaductTaaS Secret is swapped at /api/login for a short-lived JWT, and the
// JWT is what `/api/bastion` actually accepts. JWTs are cached in memory and
// re-minted on 401 (so rotation and expiry are self-healing).
type HTTPAPIClient struct {
	Endpoint string
	HTTP     *http.Client

	mu   sync.Mutex
	jwts map[string]string // access-token -> cached JWT
}

// NewHTTPAPIClient constructs an HTTPAPIClient with sensible defaults. If
// endpoint is empty, DefaultAPIEndpoint is used.
func NewHTTPAPIClient(endpoint string) *HTTPAPIClient {
	if endpoint == "" {
		endpoint = DefaultAPIEndpoint
	}
	return &HTTPAPIClient{
		Endpoint: endpoint,
		HTTP:     http.DefaultClient,
		jwts:     map[string]string{},
	}
}

// apiBastion mirrors the subset of database.Bastion we consume. Fields we
// don't use (created_at, user_id, region, dns, ...) are intentionally omitted.
type apiBastion struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	IP               string `json:"ip"`
	ConnectionSecret string `json:"connection_secret"`
	Suspended        bool   `json:"suspended"`
}

// loginResponse matches auth.LoginResponse in the OpenAPI spec.
type loginResponse struct {
	Token string `json:"token"`
}

// login exchanges an access token for a JWT. Per the spec, GET /api/login
// accepts token auth either as a Bearer header or a body field; we use
// Bearer so the token never ends up in request logs as a body parameter.
func (c *HTTPAPIClient) login(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+apiBasePath+"/login", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("aquaduct.dev /login returned %d: %s", resp.StatusCode, string(body))
	}
	var lr loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if lr.Token == "" {
		return "", fmt.Errorf("aquaduct.dev /login returned empty token")
	}
	return lr.Token, nil
}

// getJWT returns a cached JWT for the given access token, minting a new one
// via login() if there isn't one. The caller must invalidate via clearJWT
// after a 401 so stale JWTs don't get reused forever.
func (c *HTTPAPIClient) getJWT(ctx context.Context, accessToken string) (string, error) {
	c.mu.Lock()
	if jwt, ok := c.jwts[accessToken]; ok {
		c.mu.Unlock()
		return jwt, nil
	}
	c.mu.Unlock()

	jwt, err := c.login(ctx, accessToken)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.jwts[accessToken] = jwt
	c.mu.Unlock()
	return jwt, nil
}

func (c *HTTPAPIClient) clearJWT(accessToken string) {
	c.mu.Lock()
	delete(c.jwts, accessToken)
	c.mu.Unlock()
}

// authedRequest runs `build(jwt)` against a fresh JWT; if the response comes
// back 401 the cached JWT is dropped and the call is retried once (so tokens
// rotated on the server heal without human intervention). The returned
// response is the one the caller should Read/Close.
func (c *HTTPAPIClient) authedRequest(
	ctx context.Context,
	accessToken string,
	build func(jwt string) (*http.Request, error),
) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		jwt, err := c.getJWT(ctx, accessToken)
		if err != nil {
			return nil, err
		}
		req, err := build(jwt)
		if err != nil {
			return nil, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			c.clearJWT(accessToken)
			continue
		}
		return resp, nil
	}
	// Unreachable: the loop either returns or re-iterates; two iterations
	// are the max.
	return nil, fmt.Errorf("unreachable")
}

func (c *HTTPAPIClient) ListExternalServers(ctx context.Context, token string) ([]ExternalServer, error) {
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+apiBasePath+"/bastion", nil)
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
		return nil, fmt.Errorf("aquaduct.dev API returned %d listing bastions: %s", resp.StatusCode, string(body))
	}

	var bastions []apiBastion
	if err := json.NewDecoder(resp.Body).Decode(&bastions); err != nil {
		return nil, fmt.Errorf("decode bastion list: %w", err)
	}

	out := make([]ExternalServer, 0, len(bastions))
	for _, b := range bastions {
		out = append(out, ExternalServer{
			ID:               b.ID,
			Name:             b.Name,
			ConnectionString: fmt.Sprintf("weft://%s@%s:%d", b.ConnectionSecret, b.IP, defaultBastionPort),
			Suspended:        b.Suspended,
		})
	}
	return out, nil
}

func (c *HTTPAPIClient) SuspendServer(ctx context.Context, token, id string) error {
	body, err := json.Marshal(map[string]any{"suspended": true})
	if err != nil {
		return err
	}
	resp, err := c.authedRequest(ctx, token, func(jwt string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.Endpoint+apiBasePath+"/bastion/"+id, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aquaduct.dev API returned %d suspending %q: %s", resp.StatusCode, id, string(respBody))
	}
	return nil
}
