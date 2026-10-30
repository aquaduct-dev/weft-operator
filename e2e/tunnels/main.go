// Command tunnelse2e is the guest side of the tunnel e2e tests
// (//e2e/tunnels:tunnels_test, :prober_test). It is one static binary with
// three modes:
//
//   - (default) scenario: runs on the firecracker microVM next to k3s and
//     drives the scenario named by --scenario ("tunnels" or "prober") through
//     e2ekit;
//   - "echo": the in-cluster backend behind the tunnels: an HTTP server that
//     answers with the request it received as JSON, a TCP echo and a UDP
//     echo (backends.go);
//   - "acme-standin": a hostNetwork listener on :80 that answers every
//     request 404, which is what `weft probe` expects from a node that can
//     serve ACME HTTP-01 challenges.
package main

import (
	"log"
	"os"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "echo":
			os.Exit(runEcho(os.Args[2:]))
		case "acme-standin":
			os.Exit(runACMEStandin(os.Args[2:]))
		}
	}
	os.Exit(e2ekit.Main(e2ekit.Scenarios{
		"tunnels": tunnelsSteps,
		"prober":  proberSteps,
	}))
}
