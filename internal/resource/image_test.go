package resource

import "testing"

func TestPullPolicy(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/aquaduct-dev/weft:latest":     "Always",
		"ghcr.io/aquaduct-dev/weft":            "Always",
		"localhost:5000/weft":                  "Always",
		"ghcr.io/aquaduct-dev/weft:e2e":        "IfNotPresent",
		"localhost:5000/weft:v1":               "IfNotPresent",
		"ghcr.io/aquaduct-dev/weft@sha256:abc": "IfNotPresent",
	} {
		if got := string(PullPolicy(image)); got != want {
			t.Errorf("PullPolicy(%q) = %s, want %s", image, got, want)
		}
	}
}

func TestWeftImageOr(t *testing.T) {
	if got := WeftImageOr(""); got != DefaultWeftImage {
		t.Errorf("WeftImageOr(\"\") = %q", got)
	}
	if got := WeftImageOr("weft:e2e"); got != "weft:e2e" {
		t.Errorf("WeftImageOr(weft:e2e) = %q", got)
	}
}
