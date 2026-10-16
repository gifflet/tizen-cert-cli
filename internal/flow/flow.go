// Package flow orchestrates the end-to-end certificate generation:
// local keys and CSRs, issuance against Samsung's servers, PKCS#12
// bundling, the SamsungCertificate output folder and profiles.xml.
package flow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gifflet/tizen-cert-cli/internal/pki"
	"github.com/gifflet/tizen-cert-cli/internal/profile"
	"github.com/gifflet/tizen-cert-cli/internal/samsung"
)

// Config is everything the wizard collected.
type Config struct {
	ProfileName     string
	Email           string
	Password        string
	Privilege       string // "Public" or "Partner"
	DUIDs           []string
	Session         samsung.Session
	MakeActive      bool   // make the new profile the active one in profiles.xml
	ReuseAuthorFrom string // if set, try to reuse author.p12 from this directory
}

// Result summarizes what was produced, for the final wizard screen.
type Result struct {
	ProfileName     string
	OutputDir       string
	ProfilesXMLPath string
	ActiveProfile   string // the profile left active in profiles.xml
	DUIDCount       int
	NotAfter        time.Time
	AuthorReused    bool
}

// reusableAuthor holds the raw bytes of an existing author credential to be
// carried forward into a renewal without requesting a new certificate.
type reusableAuthor struct {
	P12Bytes []byte
	CRTBytes []byte // may be nil if author.crt was not present
}

// loadReusableAuthor tries to load and decode author.p12 from dir.
// Returns (author, false, nil) when the author can be reused.
// Returns (nil, true, nil) when the author should be regenerated (not found or expired).
// Returns (nil, false, err) when the file is unreadable, corrupt or the password is wrong.
func loadReusableAuthor(dir, password string, now time.Time) (*reusableAuthor, bool, error) {
	p12Path := filepath.Join(dir, "author.p12")
	data, err := os.ReadFile(p12Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", p12Path, err)
	}
	_, cert, _, decErr := pki.DecodeP12(data, password)
	if decErr != nil {
		return nil, false, fmt.Errorf(
			"cannot open %s: the password must match the one used when the profile was created "+
				"(to issue a new author instead, create a new profile with the same name and "+
				"reinstall the apps on the TVs): %w",
			p12Path, decErr,
		)
	}
	if now.After(cert.NotAfter) {
		// Expired → regenerate
		return nil, true, nil
	}
	crtBytes, _ := os.ReadFile(filepath.Join(dir, "author.crt"))
	return &reusableAuthor{
		P12Bytes: data,
		CRTBytes: crtBytes,
	}, false, nil
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
	var (
		authorReused   bool
		authorP12Bytes []byte
		authorCRTBytes []byte
		authorCSRBytes []byte
	)

	if cfg.ReuseAuthorFrom != "" {
		progress("Checking existing author certificate")
		ra, regen, loadErr := loadReusableAuthor(cfg.ReuseAuthorFrom, cfg.Password, time.Now())
		if loadErr != nil {
			return nil, loadErr
		}
		if !regen {
			authorReused = true
			authorP12Bytes = ra.P12Bytes
			authorCRTBytes = ra.CRTBytes
		} else {
			progress("Author certificate expired or not found — generating new author certificate")
		}
	}

	if !authorReused {
		progress("Generating author key and CSR")
		authorKey, err := pki.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generating author key: %w", err)
		}
		authorCSR, err := pki.CreateAuthorCSR(authorKey, cfg.ProfileName)
		if err != nil {
			return nil, err
		}
		authorCSRBytes = authorCSR

		progress("Requesting author certificate from Samsung")
		authorCertPEM, err := samsung.IssueAuthor(cfg.Session, authorCSR)
		if err != nil {
			return nil, fmt.Errorf("issuing author certificate: %w", err)
		}
		authorCerts, err := pki.ParseCertificatesPEM(authorCertPEM)
		if err != nil {
			return nil, fmt.Errorf("parsing author certificate: %w", err)
		}
		authorChain := append(authorCerts[1:], samsung.AuthorCAChain()...)
		authorP12, err := pki.EncodeP12(authorKey, authorCerts[0], authorChain, cfg.Password)
		if err != nil {
			return nil, fmt.Errorf("building author.p12: %w", err)
		}
		authorP12Bytes = authorP12
		authorCRTBytes = authorCertPEM
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

	distChain := append(distCerts[1:], samsung.DistributorCAChain(cfg.Privilege)...)
	distP12, err := pki.EncodeP12(distKey, distCerts[0], distChain, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("building distributor.p12: %w", err)
	}

	// Distributor files are always (re)written.
	distFiles := map[string][]byte{
		"distributor.crt":    distCertPEM,
		"distributor.p12":    distP12,
		"distributor.csr":    distCSR,
		"device-profile.xml": deviceProfile,
	}
	for name, data := range distFiles {
		if err := os.WriteFile(filepath.Join(outputDir, name), data, 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
	}

	// Author files: skip when reusing from the same directory to avoid
	// overwriting files that are identical by definition.
	sameDir := cfg.ReuseAuthorFrom != "" &&
		filepath.Clean(outputDir) == filepath.Clean(cfg.ReuseAuthorFrom)
	if !authorReused || !sameDir {
		if err := os.WriteFile(filepath.Join(outputDir, "author.p12"), authorP12Bytes, 0o600); err != nil {
			return nil, fmt.Errorf("writing author.p12: %w", err)
		}
		if len(authorCRTBytes) > 0 {
			if err := os.WriteFile(filepath.Join(outputDir, "author.crt"), authorCRTBytes, 0o600); err != nil {
				return nil, fmt.Errorf("writing author.crt: %w", err)
			}
		}
	}
	if len(authorCSRBytes) > 0 {
		if err := os.WriteFile(filepath.Join(outputDir, "author.csr"), authorCSRBytes, 0o600); err != nil {
			return nil, fmt.Errorf("writing author.csr: %w", err)
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
	active, err := profile.Write(profilesPath, profile.Entry{
		Name:              cfg.ProfileName,
		AuthorP12Path:     filepath.Join(outputDir, "author.p12"),
		AuthorPassword:    encrypted,
		DistributorP12:    filepath.Join(outputDir, "distributor.p12"),
		DistributorPasswd: encrypted,
	}, cfg.MakeActive)
	if err != nil {
		return nil, fmt.Errorf("writing profiles.xml: %w", err)
	}

	return &Result{
		ProfileName:     cfg.ProfileName,
		OutputDir:       outputDir,
		ProfilesXMLPath: profilesPath,
		ActiveProfile:   active,
		DUIDCount:       len(cfg.DUIDs),
		NotAfter:        distCerts[0].NotAfter,
		AuthorReused:    authorReused,
	}, nil
}
