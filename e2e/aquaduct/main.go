// Command aquaducte2e is the guest side of //e2e/aquaduct:aquaduct_test. It
// is one static binary with two modes:
//
//   - (default) scenario: runs on the firecracker microVM next to k3s and
//     drives the "aquaduct" scenario (scenario*.go) through e2ekit;
//   - "fake": runs in a pod as the in-cluster fake aquaduct.dev API and
//     authz token exchange (fake.go).
package main

import (
	"os"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "fake" {
		os.Exit(runFake(os.Args[2:]))
	}
	os.Exit(e2ekit.Main(e2ekit.Scenarios{
		"aquaduct": func(k *e2ekit.Kit) []e2ekit.Step { return newScenario(k).steps() },
	}))
}
