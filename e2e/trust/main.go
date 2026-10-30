// Command truste2e is the guest side of //e2e/trust:webhook_test. It runs on
// the firecracker microVM next to k3s and drives the trust-injection webhook
// scenario (webhook.go) through the API server. With "sleep" as its first
// argument it just blocks, so the image of this binary can be a long-running
// pod with a normal (alpine) system trust store.
package main

import (
	"log"
	"os"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sleep" {
		log.Printf("sleep: running")
		select {}
	}
	os.Exit(e2ekit.Main(e2ekit.Scenarios{"webhook": webhookSteps}))
}
