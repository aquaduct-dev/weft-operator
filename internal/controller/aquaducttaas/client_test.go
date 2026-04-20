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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
)

// These tests pin the HTTPAPIClient to the contract published in
// https://aquaduct.dev/api/openapi.json — specifically the `bastions` tag:
//   GET   /api/bastion                       List bastions
//   PATCH /api/bastion/{bastion-id}          Update a bastion (suspend/resume)
// Both require Bearer auth. The List response is a JSON array of
// database.Bastion objects.

var _ = Describe("HTTPAPIClient", func() {
	It("Lists bastions from GET /api/bastion and maps them to ExternalServers", func(ctx context.Context) {
		var gotAuth, gotPath, gotAccept string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotPath = r.URL.Path
			gotAccept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			// Shape matches database.Bastion in the OpenAPI spec.
			_, _ = w.Write([]byte(`[
				{"id":"uuid-1","name":"home","ip":"1.2.3.4","connection_secret":"sec-home","suspended":false},
				{"id":"uuid-2","name":"backup","ip":"5.6.7.8","connection_secret":"sec-backup","suspended":true}
			]`))
		}))
		defer srv.Close()

		client := aquaducttaas.NewHTTPAPIClient(srv.URL)
		servers, err := client.ListExternalServers(ctx, "mytoken")
		Expect(err).NotTo(HaveOccurred())
		Expect(gotAuth).To(Equal("Bearer mytoken"))
		Expect(gotPath).To(Equal("/api/bastion"))
		Expect(gotAccept).To(Equal("application/json"))

		Expect(servers).To(HaveLen(2))
		Expect(servers[0].ID).To(Equal("uuid-1"))
		Expect(servers[0].Name).To(Equal("home"))
		Expect(servers[0].ConnectionString).To(Equal("weft://sec-home@1.2.3.4:8080"))
		Expect(servers[0].Suspended).To(BeFalse())

		Expect(servers[1].ID).To(Equal("uuid-2"))
		Expect(servers[1].Suspended).To(BeTrue(),
			"suspended bastions must still be surfaced — the reconciler decides what to do with them")
	})

	It("Returns an error on non-2xx list responses", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		client := aquaducttaas.NewHTTPAPIClient(srv.URL)
		_, err := client.ListExternalServers(ctx, "mytoken")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("401"))
	})

	It("Returns an error on invalid JSON", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		client := aquaducttaas.NewHTTPAPIClient(srv.URL)
		_, err := client.ListExternalServers(ctx, "mytoken")
		Expect(err).To(HaveOccurred())
	})

	It("Falls back to the default endpoint when none is provided", func() {
		client := aquaducttaas.NewHTTPAPIClient("")
		Expect(client.Endpoint).To(Equal(aquaducttaas.DefaultAPIEndpoint))
	})

	It("Suspends via PATCH /api/bastion/{id} with {\"suspended\":true}", func(ctx context.Context) {
		var gotMethod, gotPath, gotAuth, gotCT string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotCT = r.Header.Get("Content-Type")
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotBody)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"uuid-1","suspended":true}`))
		}))
		defer srv.Close()

		err := aquaducttaas.NewHTTPAPIClient(srv.URL).SuspendServer(ctx, "tok", "uuid-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(gotMethod).To(Equal(http.MethodPatch))
		Expect(gotPath).To(Equal("/api/bastion/uuid-1"))
		Expect(gotAuth).To(Equal("Bearer tok"))
		Expect(gotCT).To(Equal("application/json"))
		Expect(gotBody).To(HaveKeyWithValue("suspended", true))
	})

	It("Returns an error when SuspendServer gets a non-2xx response", func(ctx context.Context) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := aquaducttaas.NewHTTPAPIClient(srv.URL).SuspendServer(ctx, "tok", "uuid-missing")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("404"))
		Expect(err.Error()).To(ContainSubstring("uuid-missing"))
	})
})
