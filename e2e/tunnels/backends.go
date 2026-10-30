// backends.go — the in-pod helpers (backends behind the tunnels) and the
// node-side clients the scenario uses to push real traffic through them.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// echoReply is what the HTTP echo backend answers: the request as it
// arrived, after every proxy on the way had its say.
type echoReply struct {
	Method  string              `json:"method"`
	Host    string              `json:"host"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

func runEcho(args []string) int {
	fs := flag.NewFlagSet("echo", flag.ContinueOnError)
	httpAddr := fs.String("http", ":8080", "HTTP echo listen address")
	tcpAddr := fs.String("tcp", ":9000", "TCP echo listen address")
	udpAddr := fs.String("udp", ":9001", "UDP echo listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	tl, err := net.Listen("tcp", *tcpAddr)
	if err != nil {
		log.Printf("echo: tcp listen: %v", err)
		return 1
	}
	go func() {
		for {
			c, err := tl.Accept()
			if err != nil {
				log.Printf("echo: tcp accept: %v", err)
				return
			}
			go func() {
				defer c.Close()
				n, _ := io.Copy(c, c)
				log.Printf("echo: tcp %s echoed %d bytes", c.RemoteAddr(), n)
			}()
		}
	}()

	uc, err := net.ListenPacket("udp", *udpAddr)
	if err != nil {
		log.Printf("echo: udp listen: %v", err)
		return 1
	}
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, err := uc.ReadFrom(buf)
			if err != nil {
				log.Printf("echo: udp read: %v", err)
				return
			}
			log.Printf("echo: udp %s %d bytes", addr, n)
			uc.WriteTo(buf[:n], addr)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("echo: http %s %s host=%s from %s", r.Method, r.URL.Path, r.Host, r.RemoteAddr)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(echoReply{Method: r.Method, Host: r.Host, Path: r.URL.Path, Headers: r.Header})
	})
	log.Printf("echo: http %s tcp %s udp %s", *httpAddr, *tcpAddr, *udpAddr)
	if err := http.ListenAndServe(*httpAddr, mux); err != nil {
		log.Printf("echo: http: %v", err)
		return 1
	}
	return 0
}

func runACMEStandin(args []string) int {
	fs := flag.NewFlagSet("acme-standin", flag.ContinueOnError)
	addr := fs.String("listen", ":80", "listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	log.Printf("acme-standin: answering 404 on %s", *addr)
	err := http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("acme-standin: %s %s host=%s from %s", r.Method, r.URL.String(), r.Host, r.RemoteAddr)
		http.NotFound(w, r)
	}))
	log.Printf("acme-standin: %v", err)
	return 1
}

// --- node-side clients ------------------------------------------------------

// httpVia sends GET http://<addr><path> with the given Host header and extra
// request headers, and returns the status and the echo backend's reply (nil
// when the body is not an echo reply, e.g. weft's own 404).
func httpVia(ctx context.Context, addr, host, path string, headers map[string]string) (int, *echoReply, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// A fresh transport per request: no connection reuse across tunnel
	// changes, so each probe really goes through the current route.
	resp, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var er echoReply
	if json.Unmarshal(body, &er) != nil || er.Method == "" {
		return resp.StatusCode, nil, nil
	}
	return resp.StatusCode, &er, nil
}

// tcpEcho writes n random bytes to addr and checks they come back intact.
func tcpEcho(ctx context.Context, addr string, n int) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		c.SetDeadline(dl)
	}
	payload := make([]byte, n)
	rand.Read(payload)
	// No CloseWrite: weft's TCP proxy tears down both directions when one
	// side half-closes, so read the echo back on the open connection.
	errc := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		errc <- err
	}()
	got := make([]byte, n)
	read, err := io.ReadFull(c, got)
	if werr := <-errc; werr != nil {
		return fmt.Errorf("write: %w", werr)
	}
	if err != nil {
		return fmt.Errorf("read after %d of %d bytes: %w", read, n, err)
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("echoed %d bytes differ from what was sent", n)
	}
	return nil
}

// udpEcho sends one datagram to addr and waits for the same bytes back.
func udpEcho(ctx context.Context, addr string) error {
	c, err := net.Dial("udp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	payload := make([]byte, 512)
	rand.Read(payload)
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write(payload); err != nil {
		return err
	}
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	if err != nil {
		return err
	}
	if !bytes.Equal(buf[:n], payload) {
		return fmt.Errorf("udp echo returned %d different bytes", n)
	}
	return nil
}

// refused reports whether a TCP dial to addr is actively refused (nothing
// listens there any more).
func refused(ctx context.Context, addr string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return strings.Contains(err.Error(), "connection refused"), err.Error()
	}
	c.Close()
	return false, "still accepting"
}

// header returns the first value of h[name] ("" if absent).
func header(r *echoReply, name string) (string, bool) {
	v, ok := r.Headers[http.CanonicalHeaderKey(name)]
	if !ok || len(v) == 0 {
		return "", false
	}
	return strings.Join(v, ","), true
}
