package weftserver

import (
	"testing"

	weftclient "github.com/aquaduct-dev/weft/src/client"
)

type countingClient struct{}

func (countingClient) ListTunnels() (map[string]weftclient.TunnelInfo, error) { return nil, nil }

// Every weft client owns a Transport that holds its connection open, so the
// reconciler must reuse one per server rather than build one per refresh.
func TestWeftClientForReusesClientPerConnString(t *testing.T) {
	made := map[string]int{}
	r := &WeftServerReconciler{ClientFactory: func(url, _ string) (WeftClient, error) {
		made[url]++
		return countingClient{}, nil
	}}

	for range 100 {
		if _, err := r.weftClientFor("weft://s@a:9092"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.weftClientFor("weft://s@b:9092"); err != nil {
		t.Fatal(err)
	}
	if made["weft://s@a:9092"] != 1 || made["weft://s@b:9092"] != 1 {
		t.Fatalf("want one client per connection string, made %v", made)
	}

	r.forgetWeftClient("weft://s@a:9092")
	if _, err := r.weftClientFor("weft://s@a:9092"); err != nil {
		t.Fatal(err)
	}
	if made["weft://s@a:9092"] != 2 {
		t.Fatalf("forgotten client was not rebuilt, made %v", made)
	}
}
