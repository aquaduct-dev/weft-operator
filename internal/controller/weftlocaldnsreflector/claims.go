// claims.go — decides which hostnames each WeftLocalDNSReflector actually
// serves. Two reflectors must never serve the same hostname: each would write
// a CoreDNS server block for the same zone, and CoreDNS refuses to load a
// config with duplicate zones (a cluster-wide DNS outage), while their node
// agents would fight over /etc/hosts and containerd certs.d. The rules:
//
//   - an explicit `hostnames` entry beats `allHostnames`;
//   - among explicit claims on one hostname, the oldest reflector wins
//     (creation time, then name);
//   - among `allHostnames` reflectors, likewise the oldest wins.
//
// Losers report the hostname in status.conflictingHostnames.
package weftlocaldnsreflector

import (
	"net"
	"net/url"
	"sort"
	"strings"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

// placeholderCertSuffix forms the SAN used when a reflector claims no hostnames
// yet; it still needs a CA and a loadable serving certificate.
const placeholderCertSuffix = ".weft-local-dns-reflector.invalid"

// resolveClaims returns the hostnames self claims and the ones it loses to
// another reflector. all is every reflector in the cluster (self included).
func resolveClaims(self *weftv1alpha1.WeftLocalDNSReflector, all []weftv1alpha1.WeftLocalDNSReflector, tunnels []weftv1alpha1.WeftTunnel) (claimed, conflicting []string) {
	live := make([]weftv1alpha1.WeftLocalDNSReflector, 0, len(all))
	for _, r := range all {
		if r.DeletionTimestamp.IsZero() && r.Name != self.Name {
			live = append(live, r)
		}
	}
	live = append(live, *self)
	sort.SliceStable(live, func(i, j int) bool { return older(&live[i], &live[j]) })

	// First explicit claim (by age) owns each hostname.
	explicitOwner := map[string]string{}
	for _, r := range live {
		if r.Spec.AllHostnames {
			continue
		}
		for _, h := range r.Spec.Hostnames {
			h = strings.ToLower(h)
			if _, taken := explicitOwner[h]; !taken {
				explicitOwner[h] = r.Name
			}
		}
	}

	if !self.Spec.AllHostnames {
		for _, h := range sortedUnique(self.Spec.Hostnames) {
			if explicitOwner[h] == self.Name {
				claimed = append(claimed, h)
			} else {
				conflicting = append(conflicting, h)
			}
		}
		return claimed, conflicting
	}

	// allHostnames: only the oldest such reflector claims anything.
	firstAll := ""
	for _, r := range live {
		if r.Spec.AllHostnames {
			firstAll = r.Name
			break
		}
	}
	for _, h := range tunnelHostnames(tunnels) {
		if _, explicit := explicitOwner[h]; explicit || firstAll != self.Name {
			conflicting = append(conflicting, h)
			continue
		}
		claimed = append(claimed, h)
	}
	return claimed, conflicting
}

// tunnelHostnames lists the distinct, valid http/https dstURL hostnames of
// tunnels, lowercased and sorted.
func tunnelHostnames(tunnels []weftv1alpha1.WeftTunnel) []string {
	var hosts []string
	for _, t := range tunnels {
		u, err := url.Parse(t.Spec.DstURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		h := strings.ToLower(u.Hostname())
		// IP literals match the hostname pattern but are not DNS names.
		if len(h) > 253 || !hostnameRE.MatchString(h) || net.ParseIP(h) != nil {
			continue
		}
		hosts = append(hosts, h)
	}
	return sortedUnique(hosts)
}

// certNames are the serving certificate SANs for a set of claimed hostnames.
func certNames(refl *weftv1alpha1.WeftLocalDNSReflector, claimed []string) []string {
	if len(claimed) == 0 {
		return []string{refl.Name + placeholderCertSuffix}
	}
	return claimed
}

func older(a, b *weftv1alpha1.WeftLocalDNSReflector) bool {
	ta, tb := a.CreationTimestamp.Time, b.CreationTimestamp.Time
	if !ta.Equal(tb) {
		// A zero time (not yet persisted) sorts last.
		if ta.IsZero() {
			return false
		}
		if tb.IsZero() {
			return true
		}
		return ta.Before(tb)
	}
	return a.Name < b.Name
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(s)
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
