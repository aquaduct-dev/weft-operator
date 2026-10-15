// routes.go — derives the `weft proxy` src→dst pairs a WeftLocalDNSReflector
// serves from the cluster's WeftTunnels. A tunnel already says "public URL X
// is backed by in-cluster URL Y"; the reflector serves the same mapping on an
// in-cluster port, so routing is defined once (usually by an HTTPRoute on a
// weft Gateway) and reused here. Pure functions only, so it is unit-testable.
package weftlocaldnsreflector

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

const (
	// Ports the proxy container listens on. Unprivileged so the weft image
	// needs no NET_BIND_SERVICE; the Service maps 443/80 onto them.
	proxyHTTPSPort = 8443
	proxyHTTPPort  = 8080
)

// routeSet is the result of matching spec hostnames against tunnels.
type routeSet struct {
	// Routes are sorted by Destination for a stable proxy command line.
	Routes []weftv1alpha1.ReflectedRoute
	// Unrouted are spec hostnames no tunnel serves.
	Unrouted []string
	// Skipped explains tunnels that matched a hostname but could not be
	// reflected, keyed by namespace/name. Logged and surfaced in status.
	Skipped map[string]string
}

// RoutedHostnames returns the distinct hostnames with at least one route.
func (s routeSet) RoutedHostnames() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range s.Routes {
		if !seen[r.Hostname] {
			seen[r.Hostname] = true
			out = append(out, r.Hostname)
		}
	}
	sort.Strings(out)
	return out
}

// HasScheme reports whether any route listens with the given scheme.
func (s routeSet) HasScheme(scheme string) bool {
	for _, r := range s.Routes {
		if strings.HasPrefix(r.Destination, scheme+"://") {
			return true
		}
	}
	return false
}

// computeRoutes maps every http/https WeftTunnel whose dstURL host is one of
// hostnames onto a proxy route. Tunnels are visited in namespace/name order so
// that when two tunnels claim the same destination the choice is stable; the
// loser is recorded in Skipped rather than handed to weft proxy, which would
// refuse to start on a conflicting route.
func computeRoutes(hostnames []string, tunnels []weftv1alpha1.WeftTunnel) routeSet {
	wanted := map[string]bool{}
	for _, h := range hostnames {
		wanted[strings.ToLower(h)] = true
	}

	sorted := append([]weftv1alpha1.WeftTunnel(nil), tunnels...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})

	set := routeSet{Skipped: map[string]string{}}
	byDestination := map[string]bool{}
	routed := map[string]bool{}
	for _, t := range sorted {
		key := t.Namespace + "/" + t.Name
		dst, err := url.Parse(t.Spec.DstURL)
		if err != nil {
			continue
		}
		host := strings.ToLower(dst.Hostname())
		if !wanted[host] {
			continue
		}
		route, reason := reflectTunnel(host, t.Spec.SrcURL, dst)
		if reason != "" {
			set.Skipped[key] = reason
			continue
		}
		if byDestination[route.Destination] {
			set.Skipped[key] = fmt.Sprintf("destination %s already served by an earlier tunnel", route.Destination)
			continue
		}
		byDestination[route.Destination] = true
		route.Tunnel = key
		routed[host] = true
		set.Routes = append(set.Routes, route)
	}

	sort.Slice(set.Routes, func(i, j int) bool { return set.Routes[i].Destination < set.Routes[j].Destination })
	for _, h := range hostnames {
		if !routed[strings.ToLower(h)] {
			set.Unrouted = append(set.Unrouted, h)
		}
	}
	sort.Strings(set.Unrouted)
	return set
}

// reflectTunnel converts one tunnel into a route, or returns why it can't be.
// The destination keeps the tunnel's path and fragment (weft encodes header
// and query matchers there) but moves to the proxy's in-cluster port.
func reflectTunnel(host, srcURL string, dst *url.URL) (weftv1alpha1.ReflectedRoute, string) {
	var port int
	switch dst.Scheme {
	case "https":
		port = proxyHTTPSPort
	case "http":
		port = proxyHTTPPort
	default:
		return weftv1alpha1.ReflectedRoute{}, fmt.Sprintf("dst scheme %q is not http/https", dst.Scheme)
	}

	src, err := url.Parse(srcURL)
	if err != nil {
		return weftv1alpha1.ReflectedRoute{}, fmt.Sprintf("unparseable srcURL: %v", err)
	}
	if src.Scheme != "http" && src.Scheme != "https" {
		return weftv1alpha1.ReflectedRoute{}, fmt.Sprintf("src scheme %q is not http/https", src.Scheme)
	}
	// A loopback source is relative to the tunnel's own pod and means
	// nothing from the proxy pod.
	if isLoopback(src.Hostname()) {
		return weftv1alpha1.ReflectedRoute{}, fmt.Sprintf("src host %q is loopback", src.Hostname())
	}

	out := *dst
	out.Host = net.JoinHostPort(host, strconv.Itoa(port))
	if out.Path == "" {
		out.Path = "/"
	}
	return weftv1alpha1.ReflectedRoute{
		Hostname:    host,
		Source:      srcURL,
		Destination: out.String(),
	}, ""
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// proxyArgs renders the `weft proxy` command line for a route set.
func proxyArgs(routes []weftv1alpha1.ReflectedRoute, certPath, keyPath string) []string {
	args := []string{"proxy", "--tls-cert=" + certPath, "--tls-key=" + keyPath}
	for _, r := range routes {
		args = append(args, r.Source, r.Destination)
	}
	return args
}
