package weftgateway

import (
	"net/url"
	"testing"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// weft reads the src URL fragment after url.Parse decoded it, so what
// matters is the decoded value.
func TestHTTPFiltersToFragment_ValuesSurviveURLDecoding(t *testing.T) {
	frag := httpFiltersToFragment([]gatewayv1.HTTPRouteFilter{{
		Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
		RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
			Set:    []gatewayv1.HTTPHeader{{Name: "X-Set", Value: "hello world"}, {Name: "X-Pct", Value: "100%#1"}},
			Add:    []gatewayv1.HTTPHeader{{Name: "X-Add", Value: "a b"}},
			Remove: []string{"X-Del"},
		},
	}})
	if want := "/#X-Set=hello%20world&X-Pct=100%25%231&X-Add=+a%20b&X-Del=!del"; frag != want {
		t.Fatalf("fragment %q, want %q", frag, want)
	}
	u, err := url.Parse("http://svc.ns.svc:80" + frag)
	if err != nil {
		t.Fatal(err)
	}
	if want := "X-Set=hello world&X-Pct=100%#1&X-Add=+a b&X-Del=!del"; u.Fragment != want {
		t.Fatalf("decoded fragment %q, want %q", u.Fragment, want)
	}
}

func TestParentRefsSelectListener(t *testing.T) {
	gw := &gatewayv1.Gateway{}
	gw.Name, gw.Namespace = "gw", "ns"
	a := gatewayv1.Listener{Name: "a"}
	b := gatewayv1.Listener{Name: "b"}
	sec := func(s string) *gatewayv1.SectionName { v := gatewayv1.SectionName(s); return &v }
	nsp := func(s string) *gatewayv1.Namespace { v := gatewayv1.Namespace(s); return &v }
	for _, tc := range []struct {
		name string
		refs []gatewayv1.ParentReference
		a, b bool
	}{
		{"unpinned", []gatewayv1.ParentReference{{Name: "gw"}}, true, true},
		{"pinned to a", []gatewayv1.ParentReference{{Name: "gw", SectionName: sec("a")}}, true, false},
		{"pinned to both", []gatewayv1.ParentReference{{Name: "gw", SectionName: sec("a")}, {Name: "gw", SectionName: sec("b")}}, true, true},
		{"other gateway", []gatewayv1.ParentReference{{Name: "other"}}, false, false},
		{"other namespace", []gatewayv1.ParentReference{{Name: "gw", Namespace: nsp("elsewhere")}}, false, false},
	} {
		if got := parentRefsSelectListener("ns", tc.refs, gw, a); got != tc.a {
			t.Errorf("%s: listener a = %v, want %v", tc.name, got, tc.a)
		}
		if got := parentRefsSelectListener("ns", tc.refs, gw, b); got != tc.b {
			t.Errorf("%s: listener b = %v, want %v", tc.name, got, tc.b)
		}
	}
}
