// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewClientWithConfig_TCPPlusTLSRequiresCertMaterial pins the
// fail-closed behavior for tcp+tls:// hosts when no TLS material is
// configured. tcp+tls:// is an *explicit* TLS opt-in scheme; without
// DOCKER_CERT_PATH / DOCKER_TLS_VERIFY (or the equivalent ClientConfig
// fields), Go's stdlib defaults would silently dial TLS against the
// system CA pool with no client cert — operators believing they have
// mTLS would be getting unauthenticated connections.
//
// See https://github.com/netresearch/ofelia/issues/627 for the
// silent-mTLS-downgrade analysis (sibling of the silent-plain-TCP-downgrade
// closed by #612 / #625).
//
// Not parallel: mutates DOCKER_HOST / DOCKER_TLS_VERIFY / DOCKER_CERT_PATH
// via t.Setenv to prevent ambient env from leaking in.
func TestNewClientWithConfig_TCPPlusTLSRequiresCertMaterial(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	_, err := NewClientWithConfig(&ClientConfig{Host: "tcp+tls://127.0.0.1:0"})
	if err == nil {
		t.Fatal("expected error for tcp+tls:// without cert material, got nil")
	}
	if !errors.Is(err, ErrTCPTLSRequiresCertMaterial) {
		t.Fatalf("expected error to wrap ErrTCPTLSRequiresCertMaterial, got %v", err)
	}
	// Error message should point operators at the docs.
	if !strings.Contains(err.Error(), "TROUBLESHOOTING") {
		t.Errorf("expected error to reference docs/TROUBLESHOOTING.md, got %q", err.Error())
	}
}

// TestNewClientWithConfig_TCPPlusTLSAcceptsCertMaterial is the positive
// counterpart: when cert material IS present (via ClientConfig.TLSCertPath),
// construction must NOT fail with ErrTCPTLSRequiresCertMaterial. We use port
// 0 so no real network connection is attempted; an unrelated dial /
// negotiate error is acceptable.
func TestNewClientWithConfig_TCPPlusTLSAcceptsCertMaterial(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	certPath := writeFakeTLSMaterial(t)

	_, err := NewClientWithConfig(&ClientConfig{
		Host:             "tcp+tls://127.0.0.1:0",
		TLSCertPath:      certPath,
		NegotiateTimeout: unreachableNegotiateTimeout,
	})
	if errors.Is(err, ErrTCPTLSRequiresCertMaterial) {
		t.Fatalf("cert material was provided via ClientConfig.TLSCertPath, must not return ErrTCPTLSRequiresCertMaterial: %v", err)
	}
	// Any other error (dial / negotiate failure against 127.0.0.1:0) is fine
	// - this test only pins that the cert-material gate does not fire.
}

// TestNewClientWithConfig_TCPPlusTLSAcceptsEnvCertPath confirms the gate
// also recognizes DOCKER_CERT_PATH as a source of cert material, mirroring
// hasTLSMaterial's precedence (config > env).
func TestNewClientWithConfig_TCPPlusTLSAcceptsEnvCertPath(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")

	certPath := writeFakeTLSMaterial(t)
	t.Setenv("DOCKER_CERT_PATH", certPath)

	_, err := NewClientWithConfig(&ClientConfig{
		Host:             "tcp+tls://127.0.0.1:0",
		NegotiateTimeout: unreachableNegotiateTimeout,
	})
	if errors.Is(err, ErrTCPTLSRequiresCertMaterial) {
		t.Fatalf("DOCKER_CERT_PATH was set, must not return ErrTCPTLSRequiresCertMaterial: %v", err)
	}
}

// writeFakeTLSMaterial creates a temp directory with self-signed ca.pem,
// cert.pem, key.pem so resolveTLSConfig can build a *tls.Config without
// returning the (nil, nil) sentinel. Reused across both positive cases.
func writeFakeTLSMaterial(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ofelia-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	for name, data := range map[string][]byte{
		"ca.pem":   certPEM,
		"cert.pem": certPEM,
		"key.pem":  keyPEM,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestNewClientWithConfig_TCPPlusTLSRejectsUnreadableCertPath addresses the
// security review of #646: hasTLSMaterial only checks string presence, so
// DOCKER_CERT_PATH=/does/not/exist (typo, broken volume mount, secrets not
// yet populated) used to pass the gate and downstream applyDockerTLS would
// silently fall back to default TLS without a client cert — the same
// silent-mTLS-downgrade the gate is supposed to prevent. The gate now also
// invokes resolveTLSConfig so unloadable material fails closed at startup.
func TestNewClientWithConfig_TCPPlusTLSRejectsUnreadableCertPath(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	// Point at an empty directory so resolveTLSConfig fails on the missing
	// cert.pem / key.pem read while hasTLSMaterial still says "material is
	// configured" (the bypass the security review flagged).
	emptyDir := t.TempDir()
	t.Setenv("DOCKER_CERT_PATH", emptyDir)

	_, err := NewClientWithConfig(&ClientConfig{Host: "tcp+tls://127.0.0.1:0"})
	if err == nil {
		t.Fatal("expected error for tcp+tls:// with unreadable cert path, got nil")
	}
	if !errors.Is(err, ErrTCPTLSRequiresCertMaterial) {
		t.Fatalf("expected error to wrap ErrTCPTLSRequiresCertMaterial, got %v", err)
	}
	if !strings.Contains(err.Error(), "unreadable or invalid") {
		t.Errorf("expected error to mention unreadable/invalid material, got %q", err.Error())
	}
}
