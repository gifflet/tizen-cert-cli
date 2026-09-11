// Package flow orchestrates the end-to-end certificate generation:
// local keys and CSRs, issuance against Samsung's servers, PKCS#12
// bundling, the SamsungCertificate output folder and profiles.xml.
package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gifflet/tizen-cert-cli/internal/pki"
	"github.com/gifflet/tizen-cert-cli/internal/profile"
	"github.com/gifflet/tizen-cert-cli/internal/samsung"
)

// Config is everything the wizard collected.
type Config struct {
	ProfileName string
	Email       string
	Password    string
	Privilege   string // "Public" or "Partner"
	DUIDs       []string
	Session     samsung.Session
}

// Result summarizes what was produced, for the final wizard screen.
type Result struct {
	ProfileName     string
	OutputDir       string
	ProfilesXMLPath string
	DUIDCount       int
	NotAfter        time.Time
}

// Run executes the whole flow, reporting each phase through progress.
func Run(cfg Config, progress func(string)) (*Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	outputDir := filepath.Join(home, "SamsungCertificate", cfg.ProfileName)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}

	// --- author certificate ---
	progress("Generating author key and CSR")
	authorKey, err := pki.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generating author key: %w", err)
	}
	authorCSR, err := pki.CreateAuthorCSR(authorKey, cfg.ProfileName)
	if err != nil {
		return nil, err
	}

	progress("Requesting author certificate from Samsung")
	authorCertPEM, err := samsung.IssueAuthor(cfg.Session, authorCSR)
	if err != nil {
		return nil, fmt.Errorf("issuing author certificate: %w", err)
	}
	authorCerts, err := pki.ParseCertificatesPEM(authorCertPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing author certificate: %w", err)
	}

	// --- distributor certificate ---
	progress(fmt.Sprintf("Generating distributor key and CSR (%d DUIDs, %s)", len(cfg.DUIDs), cfg.Privilege))
	distKey, err := pki.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generating distributor key: %w", err)
	}
	// The distributor CSR carries the Samsung account e-mail; the one typed
	// in the wizard is only a fallback when the callback did not report it.
	email := cfg.Session.Email
	if email == "" {
		email = cfg.Email
	}
	distCSR, err := pki.CreateDistributorCSR(distKey, email, cfg.DUIDs)
	if err != nil {
		return nil, err
	}

	progress("Requesting distributor certificate from Samsung")
	distCertPEM, deviceProfile, err := samsung.IssueDistributor(cfg.Session, distCSR, cfg.Privilege)
	if err != nil {
		return nil, fmt.Errorf("issuing distributor certificate: %w", err)
	}
	distCerts, err := pki.ParseCertificatesPEM(distCertPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing distributor certificate: %w", err)
	}

	// --- write artifacts ---
	progress("Writing certificates and PKCS#12 bundles")
	authorChain := append(authorCerts[1:], samsung.AuthorCAChain()...)
	authorP12, err := pki.EncodeP12(authorKey, authorCerts[0], authorChain, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("building author.p12: %w", err)
	}
	distChain := append(distCerts[1:], samsung.DistributorCAChain(cfg.Privilege)...)
	distP12, err := pki.EncodeP12(distKey, distCerts[0], distChain, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("building distributor.p12: %w", err)
	}

	files := map[string][]byte{
		"author.crt":         authorCertPEM,
		"author.p12":         authorP12,
		"author.csr":         authorCSR,
		"distributor.crt":    distCertPEM,
		"distributor.p12":    distP12,
		"distributor.csr":    distCSR,
		"device-profile.xml": deviceProfile,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(outputDir, name), data, 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
	}

	// --- register the profile ---
	progress("Registering the security profile (profiles.xml)")
	profilesPath, err := profile.DefaultProfilesPath()
	if err != nil {
		return nil, err
	}
	encrypted, err := profile.EncryptPassword(cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("encrypting profile password: %w", err)
	}
	err = profile.Write(profilesPath, profile.Entry{
		Name:              cfg.ProfileName,
		AuthorP12Path:     filepath.Join(outputDir, "author.p12"),
		AuthorPassword:    encrypted,
		DistributorP12:    filepath.Join(outputDir, "distributor.p12"),
		DistributorPasswd: encrypted,
	})
	if err != nil {
		return nil, fmt.Errorf("writing profiles.xml: %w", err)
	}

	return &Result{
		ProfileName:     cfg.ProfileName,
		OutputDir:       outputDir,
		ProfilesXMLPath: profilesPath,
		DUIDCount:       len(cfg.DUIDs),
		NotAfter:        distCerts[0].NotAfter,
	}, nil
}
