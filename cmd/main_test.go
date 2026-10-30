package main

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestMissingRouteKinds(t *testing.T) {
	for name, tc := range map[string]struct {
		resources []*metav1.APIResourceList
		want      map[string]bool
	}{
		"standard channel (no v1alpha2)": {
			want: map[string]bool{"TCPRoute": true, "TLSRoute": true, "UDPRoute": true},
		},
		"only TLSRoute served": {
			resources: []*metav1.APIResourceList{{
				GroupVersion: "gateway.networking.k8s.io/v1alpha2",
				APIResources: []metav1.APIResource{{Kind: "TLSRoute", Name: "tlsroutes"}},
			}},
			want: map[string]bool{"TCPRoute": true, "UDPRoute": true},
		},
		"experimental channel": {
			resources: []*metav1.APIResourceList{{
				GroupVersion: "gateway.networking.k8s.io/v1alpha2",
				APIResources: []metav1.APIResource{
					{Kind: "TCPRoute", Name: "tcproutes"},
					{Kind: "TLSRoute", Name: "tlsroutes"},
					{Kind: "UDPRoute", Name: "udproutes"},
				},
			}},
			want: map[string]bool{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{Resources: tc.resources}}
			got, err := missingRouteKinds(d)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("missingRouteKinds = %v, want %v", got, tc.want)
			}
		})
	}
}
