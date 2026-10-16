package flow

import (
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gifflet/tizen-cert-cli/internal/pki"
)

func makeSelfSignedP12(t *testing.T, notAfter time.Time, password string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-author"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	certDER, err := x509.CreateCertificate(cryptorand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	p12Data, err := pki.EncodeP12(key, cert, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	return p12Data
}

func TestLoadReusableAuthorValid(t *testing.T) {
	dir := t.TempDir()
	p12Data := makeSelfSignedP12(t, time.Now().Add(365*24*time.Hour), "TestPass1")
	if err := os.WriteFile(filepath.Join(dir, "author.p12"), p12Data, 0o600); err != nil {
		t.Fatal(err)
	}

	ra, regen, err := loadReusableAuthor(dir, "TestPass1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if regen {
		t.Fatal("expected regen=false for a valid certificate")
	}
	if ra == nil {
		t.Fatal("expected non-nil reusableAuthor")
	}
	if len(ra.P12Bytes) == 0 {
		t.Error("expected non-empty P12Bytes")
	}
}

func TestLoadReusableAuthorWrongPassword(t *testing.T) {
	dir := t.TempDir()
	p12Data := makeSelfSignedP12(t, time.Now().Add(365*24*time.Hour), "CorrectPass1")
	if err := os.WriteFile(filepath.Join(dir, "author.p12"), p12Data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadReusableAuthor(dir, "WrongPass1", time.Now())
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestLoadReusableAuthorExpired(t *testing.T) {
	dir := t.TempDir()
	p12Data := makeSelfSignedP12(t, time.Now().Add(-time.Hour), "TestPass1")
	if err := os.WriteFile(filepath.Join(dir, "author.p12"), p12Data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, regen, err := loadReusableAuthor(dir, "TestPass1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regen {
		t.Fatal("expected regen=true for an expired certificate")
	}
}

func TestLoadReusableAuthorMissingP12(t *testing.T) {
	dir := t.TempDir()

	_, regen, err := loadReusableAuthor(dir, "TestPass1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !regen {
		t.Fatal("expected regen=true when author.p12 is missing")
	}
}

// An author.p12 that exists but cannot be read must not be silently replaced.
func TestLoadReusableAuthorUnreadableP12(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "author.p12"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, regen, err := loadReusableAuthor(dir, "TestPass1", time.Now())
	if err == nil || regen {
		t.Fatalf("expected an error and no regeneration, got regen=%v err=%v", regen, err)
	}
}
