package pki

import (
	"bytes"
	cryptorand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestCreateDistributorCSRCarriesDUIDs(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	duids := []string{"AAAABBBBCCCCDDDDEEEEFFFF00001111", "2.0#ZZZZYYYYXXXXWWWWVVVVUUUU2222"}

	csrPEM, err := CreateDistributorCSR(key, "lab@example.com", duids)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		t.Fatal("no PEM block in CSR output")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("CSR signature invalid: %v", err)
	}
	if csr.SignatureAlgorithm != x509.SHA512WithRSA {
		t.Errorf("signature algorithm = %v, want SHA512WithRSA", csr.SignatureAlgorithm)
	}
	if csr.Subject.CommonName != "TizenSDK" {
		t.Errorf("CN = %q, want TizenSDK", csr.Subject.CommonName)
	}

	// The SAN strings must survive byte-for-byte (case included), so they
	// are asserted on the raw extension value, not on parsed url.URLs.
	var sanRaw []byte
	for _, ext := range csr.Extensions {
		if ext.Id.Equal(oidSubjectAltName) {
			sanRaw = ext.Value
		}
	}
	if sanRaw == nil {
		t.Fatal("CSR has no subjectAltName extension")
	}
	for _, want := range []string{"URN:tizen:packageid="} {
		if !bytes.Contains(sanRaw, []byte(want)) {
			t.Errorf("SAN extension missing literal %q", want)
		}
	}
	for _, duid := range duids {
		if !bytes.Contains(sanRaw, []byte("URN:tizen:deviceid="+duid)) {
			t.Errorf("SAN extension missing DUID entry for %q", duid)
		}
	}
	if !bytes.Contains(block.Bytes, []byte("lab@example.com")) {
		t.Error("CSR subject missing the EmailAddress RDN")
	}
}

func TestCreateDistributorCSRLimits(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDistributorCSR(key, "", nil); err == nil {
		t.Fatal("expected error for empty DUID list")
	}
	many := make([]string, MaxDUIDs+1)
	for i := range many {
		many[i] = "AAAABBBBCCCCDDDDEEEEFFFF00001111"
	}
	if _, err := CreateDistributorCSR(key, "", many); err == nil {
		t.Fatalf("expected error above %d DUIDs", MaxDUIDs)
	}
}

func TestCreateAuthorCSR(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := CreateAuthorCSR(key, "lab-tvs")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(csrPEM)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if csr.Subject.CommonName != "lab-tvs" {
		t.Errorf("CN = %q", csr.Subject.CommonName)
	}
	if csr.SignatureAlgorithm != x509.SHA512WithRSA {
		t.Errorf("signature algorithm = %v, want SHA512WithRSA", csr.SignatureAlgorithm)
	}
}

func TestEncodeDecodeP12RoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(cryptorand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}

	const password = "TestPass1"
	p12Data, err := EncodeP12(key, cert, nil, password)
	if err != nil {
		t.Fatal(err)
	}

	gotKey, gotCert, _, err := DecodeP12(p12Data, password)
	if err != nil {
		t.Fatalf("DecodeP12: %v", err)
	}
	if gotKey.D.Cmp(key.D) != 0 {
		t.Error("decoded private key does not match original")
	}
	if !gotCert.Equal(cert) {
		t.Error("decoded certificate does not match original")
	}
}

func TestDecodeP12WrongPassword(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(cryptorand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(certDER)
	p12Data, err := EncodeP12(key, cert, nil, "CorrectPass1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeP12(p12Data, "WrongPass1"); err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}
