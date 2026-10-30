// fetch.go — the "fetch" mode, run inside a pod: a plain-HTTP GET of a
// reflected URL through the default resolver, for routes whose tunnel
// publishes an http:// destination (served on the proxy's HTTP port). It
// passes only for the registry's /v2/ answer.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"
)

func runFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	target := fs.String("url", "", "http URL to GET")
	expectIP := fs.String("expect-ip", "", "IP the URL's host must resolve to")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	u, err := url.Parse(*target)
	if err != nil || u.Scheme != "http" {
		log.Printf("fetch: FAIL: want an http URL, got %q", *target)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, u.Hostname())
	log.Printf("fetch: lookup %s -> %v (err=%v)", u.Hostname(), addrs, err)
	if err != nil {
		return 1
	}
	if *expectIP != "" && !slices.Contains(addrs, *expectIP) {
		log.Printf("fetch: FAIL: %s resolved to %v, want %s", u.Hostname(), addrs, *expectIP)
		return 1
	}
	resp, err := http.Get(*target)
	if err != nil {
		log.Printf("fetch: FAIL: GET: %v", err)
		return 1
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Printf("fetch: GET %s -> %d api=%q", *target, resp.StatusCode, resp.Header.Get("Docker-Distribution-Api-Version"))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		log.Printf("fetch: FAIL: unexpected response")
		return 1
	}
	log.Printf("fetch: OK")
	return 0
}
