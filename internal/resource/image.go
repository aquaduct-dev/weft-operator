package resource

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// DefaultWeftImage is the weft image the operator runs (tunnel clients,
// servers, probes and reflector proxies) unless WEFT_IMAGE overrides it.
const DefaultWeftImage = "ghcr.io/aquaduct-dev/weft:latest"

// WeftImageOr returns image, or DefaultWeftImage when it is empty.
func WeftImageOr(image string) string {
	if image == "" {
		return DefaultWeftImage
	}
	return image
}

// PullPolicy mirrors the API server's default: always re-pull a floating
// :latest (or untagged) image, otherwise use what the node has. Setting it
// explicitly keeps CreateOrUpdate idempotent; Always for a pinned tag would
// also make the pod unrunnable on airgapped nodes.
func PullPolicy(image string) corev1.PullPolicy {
	if strings.Contains(image, "@") {
		return corev1.PullIfNotPresent
	}
	name := image[strings.LastIndex(image, "/")+1:]
	if i := strings.LastIndex(name, ":"); i < 0 || name[i+1:] == "latest" {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}
