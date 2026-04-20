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

package aquaducttaas_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
)

// These tests pin the HTTPAPIClient to the aquaduct.dev API contract:
//   POST  /api/auth/token-exchange    Exchange access token (body) for a JWT
//   GET   /api/bastion                List bastions (JWT Bearer)
//   PATCH /api/bastion/{id}           Update a bastion (JWT Bearer)
//
// The long-lived aqt_-prefixed token is NOT accepted by /api/bastion
// directly — the bastion endpoints require a JWT. HTTPAPIClient performs the
// exchange and caches the JWT in memory, re-minting it on a 401 so rotation
// heals automatically.

// mockAquaductServer builds an httptest.Server that models the two-hop flow:
// /api/auth/token-exchange accepts the access token in a JSON body and
// returns a JWT, and the bastion endpoints only accept that JWT. Fields
// capture what the last request looked like so assertions can check headers
// and paths.
type mockAquaductServer struct {
	*httptest.Server

	accessToken string // the long-lived token the client is expected to present
	jwt         string // the JWT we mint in response

	listCount    atomic.Int32
	loginCount   atomic.Int32
	suspendCount atomic.Int32

	// listHandler / suspendHandler let individual tests override behavior
	// once auth has been validated. If nil, default success responses apply.
	listHandler    http.HandlerFunc
	suspendHandler http.HandlerFunc
}

func newMockAquaductServer(accessToken, jwt string) *mockAquaductServer {
	m := &mockAquaductServer{accessToken: accessToken, jwt: jwt}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/auth/token-exchange", func(w http.ResponseWriter, r *http.Request) {
		m.loginCount.Add(1)
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad body"}`, http.StatusBadRequest)
			return
		}
		if body.Token != m.accessToken {
			http.Error(w, `{"error":"invalid access token"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": m.jwt, "expires_in": 3600})
	})

	mux.HandleFunc("/api/bastion", func(w http.ResponseWriter, r *http.Request) {
		m.listCount.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+m.jwt {
			http.Error(w, `{"error":"invalid jwt"}`, http.StatusUnauthorized)
			return
		}
		if m.listHandler != nil {
			m.listHandler(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"uuid-1","name":"home","ip":"1.2.3.4","connection_secret":"sec","suspended":false}]`))
	})

	mux.HandleFunc("/api/bastion/", func(w http.ResponseWriter, r *http.Request) {
		m.suspendCount.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+m.jwt {
			http.Error(w, `{"error":"invalid jwt"}`, http.StatusUnauthorized)
			return
		}
		if m.suspendHandler != nil {
			m.suspendHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"suspended":true}`))
	})

	m.Server = httptest.NewServer(mux)
	return m
}

var _ = Describe("HTTPAPIClient", func() {
	It("Exchanges the access token at /api/auth/token-exchange, then lists bastions with the JWT", func(ctx context.Context) {
		m := newMockAquaductServer("access-token", "signed.jwt.here")
		defer m.Close()

		client := aquaducttaas.NewHTTPAPIClient(m.URL)
		servers, err := client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred())
		Expect(servers).To(HaveLen(1))
		Expect(servers[0].ID).To(Equal("uuid-1"))
		Expect(servers[0].ConnectionString).To(Equal("weft://sec@1.2.3.4:9092"))
		Expect(m.loginCount.Load()).To(Equal(int32(1)))
		Expect(m.listCount.Load()).To(Equal(int32(1)))

		By("A second call reuses the cached JWT — no extra exchange round trip")
		_, err = client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred())
		Expect(m.loginCount.Load()).To(Equal(int32(1)))
		Expect(m.listCount.Load()).To(Equal(int32(2)))
	})

	It("Re-exchanges proactively when expires_in has elapsed — no 401 round-trip needed", func(ctx context.Context) {
		m := newMockAquaductServer("access-token", "jwt-1")
		defer m.Close()

		client := aquaducttaas.NewHTTPAPIClient(m.URL)
		nowVal := time.Unix(1700000000, 0)
		client.Now = func() time.Time { return nowVal }

		_, err := client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred())
		Expect(m.loginCount.Load()).To(Equal(int32(1)))
		Expect(m.listCount.Load()).To(Equal(int32(1)))

		By("Advancing past expires_in=3600 minus grace invalidates the cache")
		nowVal = nowVal.Add(3600 * time.Second)

		_, err = client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred())
		Expect(m.loginCount.Load()).To(Equal(int32(2)),
			"client must have re-exchanged without waiting for a 401")
		Expect(m.listCount.Load()).To(Equal(int32(2)),
			"the list call should have succeeded on the first try with the fresh JWT")
	})

	It("Re-exchanges the access token on 401, so expired/rotated JWTs self-heal", func(ctx context.Context) {
		m := newMockAquaductServer("access-token", "jwt-v2")
		defer m.Close()

		client := aquaducttaas.NewHTTPAPIClient(m.URL)

		By("Priming the cache with an expired JWT that the server will reject")
		// We reach into the client via a pair of real calls: first seed the
		// cache, then rotate what the server considers valid, then list
		// again and confirm the client re-logged-in.
		_, err := client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred())
		loginsBefore := m.loginCount.Load()

		m.jwt = "jwt-v3" // server now rejects the cached JWT

		_, err = client.ListExternalServers(ctx, "access-token")
		Expect(err).NotTo(HaveOccurred(), "client should re-login and retry once on 401")
		Expect(m.loginCount.Load()).To(Equal(loginsBefore+1),
			"exactly one extra exchange after the 401, not repeated retries")
	})

	It("Returns an error if token-exchange itself fails", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		_, err := aquaducttaas.NewHTTPAPIClient(srv.URL).ListExternalServers(ctx, "bogus")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("/auth/token-exchange"))
		Expect(err.Error()).To(ContainSubstring("401"))
	})

	It("Returns a non-2xx list response as an error", func(ctx context.Context) {
		m := newMockAquaductServer("tok", "jwt")
		m.listHandler = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"server down"}`, http.StatusInternalServerError)
		}
		defer m.Close()

		_, err := aquaducttaas.NewHTTPAPIClient(m.URL).ListExternalServers(ctx, "tok")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("500"))
	})

	It("Returns an error on invalid JSON bodies", func(ctx context.Context) {
		m := newMockAquaductServer("tok", "jwt")
		m.listHandler = func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}
		defer m.Close()

		_, err := aquaducttaas.NewHTTPAPIClient(m.URL).ListExternalServers(ctx, "tok")
		Expect(err).To(HaveOccurred())
	})

	It("Falls back to the default endpoint when none is provided", func() {
		client := aquaducttaas.NewHTTPAPIClient("")
		Expect(client.Endpoint).To(Equal(aquaducttaas.DefaultAPIEndpoint))
	})

	It("Suspends via PATCH /api/bastion/{id} with {\"suspended\":true} and JWT auth", func(ctx context.Context) {
		var gotMethod, gotPath, gotCT string
		var gotBody map[string]any
		m := newMockAquaductServer("tok", "jwt")
		m.suspendHandler = func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotCT = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"suspended":true}`))
		}
		defer m.Close()

		err := aquaducttaas.NewHTTPAPIClient(m.URL).SuspendServer(ctx, "tok", "uuid-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(gotMethod).To(Equal(http.MethodPatch))
		Expect(gotPath).To(Equal("/api/bastion/uuid-1"))
		Expect(gotCT).To(Equal("application/json"))
		Expect(gotBody).To(HaveKeyWithValue("suspended", true))
	})

	It("Returns an error when SuspendServer gets a non-2xx response", func(ctx context.Context) {
		m := newMockAquaductServer("tok", "jwt")
		m.suspendHandler = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
		defer m.Close()

		err := aquaducttaas.NewHTTPAPIClient(m.URL).SuspendServer(ctx, "tok", "uuid-missing")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("404"))
		Expect(err.Error()).To(ContainSubstring("uuid-missing"))
	})
})
