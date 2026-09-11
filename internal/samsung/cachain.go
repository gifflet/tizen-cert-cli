package samsung

import (
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
)

// The Samsung VD CA certificates, extracted from the certificate extension
// 2.0.75 jar (res/ca/). They are static and bundled into the binary.
//
//go:embed ca/vd_tizen_dev_author_ca.cer ca/vd_tizen_dev_public2.crt ca/vd_tizen_dev_partner2.crt
var caFiles embed.FS

// AuthorCAChain returns the CA appended to author.p12 for TV profiles.
func AuthorCAChain() []*x509.Certificate {
	return []*x509.Certificate{mustLoadCA("ca/vd_tizen_dev_author_ca.cer")}
}

// DistributorCAChain returns the CA appended to distributor.p12 for the
// given privilege level.
func DistributorCAChain(privilege string) []*x509.Certificate {
	if privilege == "Partner" {
		return []*x509.Certificate{mustLoadCA("ca/vd_tizen_dev_partner2.crt")}
	}
	return []*x509.Certificate{mustLoadCA("ca/vd_tizen_dev_public2.crt")}
}

func mustLoadCA(name string) *x509.Certificate {
	data, err := caFiles.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("embedded CA %s: %v", name, err))
	}
	block, _ := pem.Decode(data)
	if block == nil {
		panic(fmt.Sprintf("embedded CA %s is not PEM", name))
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("embedded CA %s: %v", name, err))
	}
	return cert
}
