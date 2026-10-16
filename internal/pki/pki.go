// Package pki handles the local cryptography of the certificate flow:
// RSA keys, CSRs (author and distributor with DUID subject alt names),
// PKCS#12 bundles and inspection of existing certificates for renewal.
//
// The exact formats mirror Samsung's certificate extension (2.0.75):
// RSA 2048, SHA512withRSA, and distributor SANs of URI type carrying
// "URN:tizen:deviceid=<DUID>" strings.
package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// duidSANPrefix is the string form Samsung embeds per device. The first SAN
// entry is always the empty "URN:tizen:packageid=".
const (
	duidSANPrefix = "URN:tizen:deviceid="
	packageIDSAN  = "URN:tizen:packageid="
	// MaxDUIDs is Samsung's documented limit per distributor certificate.
	MaxDUIDs = 50
)

var (
	oidSubjectAltName = asn1.ObjectIdentifier{2, 5, 29, 17}
	oidEmailAddress   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}
)

// GenerateKey creates the RSA 2048 key expected by Samsung's issuance APIs.
func GenerateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// CreateAuthorCSR builds a PEM-encoded CSR for the author certificate.
// Only the CN is mandatory; Samsung's own client sends CN/OU/O/L/S/C from
// the wizard and tolerates empty components.
func CreateAuthorCSR(key *rsa.PrivateKey, name string) ([]byte, error) {
	template := x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: name},
		SignatureAlgorithm: x509.SHA512WithRSA,
	}
	return createCSR(&template, key)
}

// CreateDistributorCSR builds a PEM-encoded CSR with subject CN=TizenSDK,
// the account e-mail as an EmailAddress RDN, and one URI SAN per DUID.
// The SAN extension is built by hand: Go's url.URL would lowercase the
// "URN:" scheme, and Samsung's servers receive the string verbatim.
func CreateDistributorCSR(key *rsa.PrivateKey, email string, duids []string) ([]byte, error) {
	if len(duids) == 0 {
		return nil, fmt.Errorf("at least one DUID is required for a distributor CSR")
	}
	if len(duids) > MaxDUIDs {
		return nil, fmt.Errorf("Samsung limits a distributor certificate to %d DUIDs, got %d", MaxDUIDs, len(duids))
	}

	sanValue, err := buildDUIDSAN(duids)
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{CommonName: "TizenSDK"}
	if email != "" {
		subject.ExtraNames = []pkix.AttributeTypeAndValue{{
			Type:  oidEmailAddress,
			Value: asn1.RawValue{Tag: asn1.TagIA5String, Bytes: []byte(email)},
		}}
	}

	template := x509.CertificateRequest{
		Subject:            subject,
		SignatureAlgorithm: x509.SHA512WithRSA,
		ExtraExtensions: []pkix.Extension{{
			Id:    oidSubjectAltName,
			Value: sanValue,
		}},
	}
	return createCSR(&template, key)
}

// buildDUIDSAN encodes GeneralNames = SEQUENCE of [6] IA5String, starting
// with the empty packageid entry, exactly as Samsung's client does.
func buildDUIDSAN(duids []string) ([]byte, error) {
	names := make([]asn1.RawValue, 0, len(duids)+1)
	names = append(names, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 6, Bytes: []byte(packageIDSAN)})
	for _, duid := range duids {
		duid = strings.TrimSpace(duid)
		if duid == "" {
			return nil, fmt.Errorf("empty DUID in list")
		}
		names = append(names, asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 6, Bytes: []byte(duidSANPrefix + duid)})
	}
	return asn1.Marshal(names)
}

func createCSR(template *x509.CertificateRequest, key *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, fmt.Errorf("creating CSR: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// EncodeP12 bundles a key, its certificate and the CA chain into a PKCS#12
// file. LegacyDES (PBE-SHA1-3DES bags + SHA-1 MAC) is used on purpose:
// Tizen Studio's Java signer rejects modern AES/PBKDF2 PKCS#12 files.
func EncodeP12(key *rsa.PrivateKey, cert *x509.Certificate, chain []*x509.Certificate, password string) ([]byte, error) {
	return pkcs12.LegacyDES.WithRand(rand.Reader).Encode(key, cert, chain, password)
}

// DecodeP12 extracts the private key, leaf certificate and CA chain from a
// PKCS#12 bundle. Returns an error if the key is not RSA.
func DecodeP12(data []byte, password string) (*rsa.PrivateKey, *x509.Certificate, []*x509.Certificate, error) {
	key, cert, chain, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return nil, nil, nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, nil, fmt.Errorf("author.p12 key is not RSA")
	}
	return rsaKey, cert, chain, nil
}

// ParseCertificatesPEM parses every CERTIFICATE block in the given PEM data.
func ParseCertificatesPEM(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parsing certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no CERTIFICATE block found")
	}
	return certs, nil
}

// CertInfo is a summary of an existing certificate, used by the renewal flow.
type CertInfo struct {
	Subject  string
	NotAfter time.Time
	Expired  bool
	DUIDs    []string
}

// InspectCertFile reads a PEM certificate file and summarizes it, extracting
// DUIDs from the URI SANs of distributor certificates. Go re-serializes the
// URN scheme lowercased, so the prefix match is case-insensitive.
func InspectCertFile(path string) (*CertInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	certs, err := ParseCertificatesPEM(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cert := certs[0]
	info := &CertInfo{
		Subject:  cert.Subject.String(),
		NotAfter: cert.NotAfter,
		Expired:  time.Now().After(cert.NotAfter),
	}
	for _, uri := range cert.URIs {
		full := uri.String()
		if len(full) >= len(duidSANPrefix) && strings.EqualFold(full[:len(duidSANPrefix)], duidSANPrefix) {
			duid := full[len(duidSANPrefix):]
			if duid != "" {
				info.DUIDs = append(info.DUIDs, duid)
			}
		}
	}
	return info, nil
}
