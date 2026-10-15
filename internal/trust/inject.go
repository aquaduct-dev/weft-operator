// inject.go — the pod mutation that makes a workload trust the reflector
// CAs with no change to its image or manifest. Every container gets the
// trust bundle mounted (subPath) over the standard system bundle paths:
// Debian/Ubuntu/Alpine/distroless (/etc/ssl/certs/ca-certificates.crt),
// RHEL/Fedora (/etc/pki/tls/certs/ca-bundle.crt) and LibreSSL
// (/etc/ssl/cert.pem). That covers everything that reads the system store:
// Go, OpenSSL and GnuTLS (curl, git), Python's ssl module.
//
// The container's environment is deliberately left untouched, so runtimes
// that ship their own CA list and ignore the system store are not covered:
// Node.js (bundled CAs), Python requests (certifi) and Java (cacerts).
//
// Containers that mount something of their own at (or above) a bundle path
// are left to manage that path. The mutation is idempotent so webhook
// reinvocation is safe.
package trust

import (
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// OptOutKey as a pod label or annotation with value "false" skips
	// injection (and exempts the pod from the reflector's trust gate).
	OptOutKey = "weft.aquaduct.dev/inject-trust"
	// InjectedAnnotation records the CA fingerprints a pod was given.
	InjectedAnnotation = "weft.aquaduct.dev/trusted-cas"

	// VolumeName is the injected ConfigMap volume.
	VolumeName = "weft-trust-bundle"
)

// SystemBundlePaths are overlaid with the bundle in every container.
var SystemBundlePaths = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/cert.pem",
}

// OptedOut reports whether the pod asked not to be injected.
func OptedOut(pod *corev1.Pod) bool {
	return pod.Labels[OptOutKey] == "false" || pod.Annotations[OptOutKey] == "false"
}

// IsWindows reports whether the pod targets Windows nodes, where none of the
// Linux trust paths apply.
func IsWindows(pod *corev1.Pod) bool {
	if pod.Spec.OS != nil && pod.Spec.OS.Name == corev1.Windows {
		return true
	}
	return pod.Spec.NodeSelector["kubernetes.io/os"] == "windows"
}

// InjectPod mutates pod to trust bundle. It returns false if the pod is
// opted out, targets Windows, or already carries everything.
func InjectPod(pod *corev1.Pod, bundle Bundle) bool {
	if OptedOut(pod) || IsWindows(pod) || bundle.Empty() {
		return false
	}
	changed := false
	if !hasVolume(pod, VolumeName) {
		mode := int32(0o444)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: VolumeName,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName},
				DefaultMode:          &mode,
			}},
		})
		changed = true
	}
	for i := range pod.Spec.InitContainers {
		changed = injectContainer(&pod.Spec.InitContainers[i]) || changed
	}
	for i := range pod.Spec.Containers {
		changed = injectContainer(&pod.Spec.Containers[i]) || changed
	}
	want := FingerprintList(bundle.Fingerprints)
	if pod.Annotations[InjectedAnnotation] != want {
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[InjectedAnnotation] = want
		changed = true
	}
	return changed
}

func injectContainer(c *corev1.Container) bool {
	changed := false
	for _, p := range SystemBundlePaths {
		if mountCovers(c, p) {
			continue
		}
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: VolumeName, MountPath: p, SubPath: BundleKey, ReadOnly: true})
		changed = true
	}
	return changed
}

// mountCovers reports whether c already mounts something at p or at one of
// its parent directories. Our own mount at p counts too (idempotence).
func mountCovers(c *corev1.Container, p string) bool {
	for _, m := range c.VolumeMounts {
		mp := strings.TrimRight(m.MountPath, "/")
		if mp == "" {
			mp = "/"
		}
		if mp == p || mp == "/" || strings.HasPrefix(p, mp+"/") {
			return true
		}
	}
	return false
}

func hasVolume(pod *corev1.Pod, name string) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

// TrustsCA reports whether a pod's injected annotation includes fingerprint.
// annotations may come from a full Pod or from metadata-only listings.
func TrustsCA(annotations map[string]string, fingerprint string) bool {
	return slices.Contains(strings.Split(annotations[InjectedAnnotation], ","), fingerprint)
}
