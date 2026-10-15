// Package pki holds the small amount of X.509 machinery the operator needs:
// a self-signed CA, serving certificates signed by it, and the checks that
// decide when a serving certificate must be reissued. It is shared by the
// WeftLocalDNSReflector controller (proxy certificates) and the trust
// injection webhook (its own serving certificate). Key material is returned
// to callers as PEM and never logged here.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// CAValidity is how long a generated CA is valid.
	CAValidity = 10 * 365 * 24 * time.Hour
	// LeafValidity is how long a serving certificate is valid.
	LeafValidity = 365 * 24 * time.Hour
	// LeafRenewBefore is how long before expiry a serving certificate is reissued.
	LeafRenewBefore = 30 * 24 * time.Hour
	// clockSkew backdates NotBefore so a host with a slightly slow clock
	// does not reject a certificate minted seconds ago.
	clockSkew = 5 * time.Minute
)

// NewCA returns a fresh self-signed ECDSA P-256 CA as PEM.
func NewCA(commonName string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"weft-operator"}},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(CAValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("self-sign CA: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal CA key: %w", err)
	}
	return pemBlock("CERTIFICATE", der), pemBlock("EC PRIVATE KEY", keyDER), nil
}

// IssueLeaf signs a serving certificate for dnsNames with the given CA.
func IssueLeaf(caCertPEM, caKeyPEM []byte, dnsNames []string, now time.Time) (certPEM, keyPEM []byte, err error) {
	if len(dnsNames) == 0 {
		return nil, nil, fmt.Errorf("issue serving certificate: no DNS names")
	}
	caCert, caKey, err := parseCA(caCertPEM, caKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate serving key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	sans := SortedLower(dnsNames)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: sans[0]},
		DNSNames:     sans,
		NotBefore:    now.Add(-clockSkew),
		NotAfter:     now.Add(LeafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sign serving certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal serving key: %w", err)
	}
	return pemBlock("CERTIFICATE", der), pemBlock("EC PRIVATE KEY", keyDER), nil
}

// RenewalReason returns why the serving certificate must be reissued, or ""
// if it is still good for dnsNames.
func RenewalReason(leafPEM, caCertPEM []byte, dnsNames []string, now time.Time) string {
	leaf, err := ParseCert(leafPEM)
	if err != nil {
		return "serving certificate missing or unparseable"
	}
	ca, err := ParseCert(caCertPEM)
	if err != nil {
		return "CA certificate unparseable"
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		return "serving certificate is not signed by the current CA"
	}
	if !slices.Equal(SortedLower(leaf.DNSNames), SortedLower(dnsNames)) {
		return fmt.Sprintf("SANs %v differ from %v", leaf.DNSNames, SortedLower(dnsNames))
	}
	if now.After(leaf.NotAfter.Add(-LeafRenewBefore)) {
		return fmt.Sprintf("expires %s, inside the %s renewal window", leaf.NotAfter.Format(time.RFC3339), LeafRenewBefore)
	}
	return ""
}

// Fingerprint is a short stable digest of PEM data, used in annotations so a
// changed certificate or bundle rolls the pods that consume it.
func Fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// ParseCert parses the first CERTIFICATE block of certPEM.
func ParseCert(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no CERTIFICATE PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// CountCerts returns how many CERTIFICATE blocks parse out of a PEM bundle.
func CountCerts(bundle []byte) int {
	n := 0
	for rest := bundle; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return n
		}
		if block.Type == "CERTIFICATE" {
			if _, err := x509.ParseCertificate(block.Bytes); err == nil {
				n++
			}
		}
	}
}

// SortedLower lowercases, sorts and de-duplicates names.
func SortedLower(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.ToLower(s))
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cert, err := ParseCert(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("parse CA key: no PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA key: %w", err)
	}
	return cert, key, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

func pemBlock(kind string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
}
