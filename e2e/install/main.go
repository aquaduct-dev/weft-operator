// Command installe2e is the guest side of the installation and lifecycle
// e2e tests (//e2e/install:crds_test, :chart_test): it runs on the
// firecracker microVM next to k3s and drives the scenario named by
// --scenario ("crds", crds_scenario.go; "chart", chart_scenario.go).
package main

import (
	"os"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

func main() {
	os.Exit(e2ekit.Main(e2ekit.Scenarios{
		"crds":  crdsSteps,
		"chart": chartSteps,
	}))
}
