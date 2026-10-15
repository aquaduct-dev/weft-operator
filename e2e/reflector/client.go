// client.go — the "client" mode, run inside a pod in the e2e cluster. It
// behaves like an ordinary workload that knows nothing about the reflector:
// it uses the default resolver and Go's default TLS config, so its HTTPS
// request only verifies if the trust webhook put the reflector CA into the
// system trust store, and it only connects if cluster DNS was overridden.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

func runClient(args []string) int {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	host := fs.String("host", "", "reflected hostname")
	expectIP := fs.String("expect-ip", "", "IP the hostname must resolve to")
	expectUnresolvable := fs.Bool("expect-unresolvable", false, "succeed only if the hostname does NOT resolve")
	expectUntrusted := fs.Bool("expect-untrusted", false, "succeed only if the hostname resolves but its certificate is NOT trusted (control run)")
	pushRepo := fs.String("push", "", "repository (e.g. e2e/self) to push this binary to as an image, tag 1")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, *host)
	log.Printf("client: lookup %s -> %v (err=%v)", *host, addrs, err)
	if *expectUnresolvable {
		if err == nil {
			log.Printf("client: FAIL: %s resolved to %v before the reflector existed", *host, addrs)
			return 1
		}
		log.Printf("client: OK: %s does not resolve", *host)
		return 0
	}
	if err != nil {
		log.Printf("client: FAIL: lookup: %v", err)
		return 1
	}
	if *expectIP != "" && !slices.Contains(addrs, *expectIP) {
		log.Printf("client: FAIL: %s resolved to %v, want %s", *host, addrs, *expectIP)
		return 1
	}

	// Default transport: system roots only. No InsecureSkipVerify, no
	// custom CA — that is the point of the test.
	base := "https://" + *host
	resp, err := http.Get(base + "/v2/")
	if *expectUntrusted {
		var unknownCA x509.UnknownAuthorityError
		if err != nil && errors.As(err, &unknownCA) {
			log.Printf("client: OK: certificate rejected without the reflector CA: %v", err)
			return 0
		}
		log.Printf("client: FAIL: expected an unknown-authority error, got err=%v", err)
		return 1
	}
	if err != nil {
		log.Printf("client: FAIL: GET /v2/: %v", err)
		return 1
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Printf("client: GET /v2/ -> %d, verified peer %s, api=%q", resp.StatusCode, resp.TLS.PeerCertificates[0].Subject.CommonName, resp.Header.Get("Docker-Distribution-Api-Version"))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		log.Printf("client: FAIL: unexpected /v2/ response")
		return 1
	}

	if *pushRepo != "" {
		if err := pushSelf(base, *pushRepo); err != nil {
			log.Printf("client: FAIL: push: %v", err)
			return 1
		}
		log.Printf("client: pushed %s/%s:1", *host, *pushRepo)
	}
	log.Printf("client: OK")
	return 0
}

// pushSelf pushes a one-layer image whose only file is this binary at /e2e,
// with entrypoint `/e2e sleep`, using the registry HTTP API directly.
func pushSelf(base, repo string) error {
	self, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		return err
	}
	var layerTar bytes.Buffer
	tw := tar.NewWriter(&layerTar)
	if err := tw.WriteHeader(&tar.Header{Name: "e2e", Mode: 0o755, Size: int64(len(self)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	tw.Write(self)
	tw.Close()
	diffID := digest(layerTar.Bytes())
	var layerGz bytes.Buffer
	gz := gzip.NewWriter(&layerGz)
	gz.Write(layerTar.Bytes())
	gz.Close()

	config, _ := json.Marshal(map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"config":       map[string]any{"Entrypoint": []string{"/e2e", "sleep"}},
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{diffID}},
	})
	for _, blob := range [][]byte{layerGz.Bytes(), config} {
		if err := pushBlob(base, repo, blob); err != nil {
			return err
		}
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": digest(config), "size": len(config)},
		"layers": []map[string]any{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": digest(layerGz.Bytes()), "size": layerGz.Len(),
		}},
	})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/v2/%s/manifests/1", base, repo), bytes.NewReader(manifest))
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	return expect(http.DefaultClient.Do(req))(http.StatusCreated)
}

func pushBlob(base, repo string, blob []byte) error {
	resp, err := http.Post(fmt.Sprintf("%s/v2/%s/blobs/uploads/", base, repo), "", nil)
	if err := expect(resp, err)(http.StatusAccepted); err != nil {
		return fmt.Errorf("start upload: %w", err)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return err
	}
	b, _ := url.Parse(base)
	upload := b.ResolveReference(loc)
	q := upload.Query()
	q.Set("digest", digest(blob))
	upload.RawQuery = q.Encode()
	req, _ := http.NewRequest(http.MethodPut, upload.String(), bytes.NewReader(blob))
	req.Header.Set("Content-Type", "application/octet-stream")
	if err := expect(http.DefaultClient.Do(req))(http.StatusCreated); err != nil {
		return fmt.Errorf("upload %s: %w", digest(blob), err)
	}
	return nil
}

func expect(resp *http.Response, err error) func(int) error {
	return func(code int) error {
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != code {
			return fmt.Errorf("%s %s: status %d, want %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, code, strings.TrimSpace(string(body)))
		}
		return nil
	}
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
