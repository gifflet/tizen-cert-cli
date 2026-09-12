// Package profile writes the Tizen security profile registration
// (profiles.xml) directly, so no Tizen Studio CLI is needed on the machine.
package profile

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type profilesXML struct {
	XMLName xml.Name     `xml:"profiles"`
	Active  string       `xml:"active,attr"`
	Version string       `xml:"version,attr"`
	Items   []profileXML `xml:"profile"`
}

type profileXML struct {
	Name  string           `xml:"name,attr"`
	Items []profileItemXML `xml:"profileitem"`
}

type profileItemXML struct {
	CA          string `xml:"ca,attr"`
	Distributor string `xml:"distributor,attr"`
	Key         string `xml:"key,attr"`
	Password    string `xml:"password,attr"`
	RootCA      string `xml:"rootca,attr"`
}

// Entry describes one signing profile to register.
type Entry struct {
	Name              string
	AuthorP12Path     string
	AuthorPassword    string // encrypted form, see password.go
	DistributorP12    string
	DistributorPasswd string // encrypted form
}

// DefaultProfilesPath returns the profiles.xml the signing tools will read.
// The tizen CLI can be configured (default.profiles.path) to a different
// location than the current user's data directory - registering the profile
// anywhere else makes `tizen package` silently emit UNSIGNED packages - so
// when the CLI is available its effective path wins over the home default.
func DefaultProfilesPath() (string, error) {
	if path := tizenCLIProfilesPath(); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "tizen-studio-data", "profile", "profiles.xml"), nil
}

func tizenCLIProfilesPath() string {
	out, err := exec.Command("tizen", "cli-config", "-l").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "default.profiles.path="); found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// read parses an existing profiles.xml.
func read(path string) (profilesXML, error) {
	var doc profilesXML
	data, err := os.ReadFile(path)
	if err != nil {
		return doc, err
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parsing %s: %w", path, err)
	}
	return doc, nil
}

// List returns the registered profile names and which one is active.
// A missing profiles.xml is not an error - there is just nothing yet.
func List(path string) (names []string, active string, err error) {
	doc, err := read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	for _, p := range doc.Items {
		names = append(names, p.Name)
	}
	return names, doc.Active, nil
}

// SetActive marks an already-registered profile as the active one.
func SetActive(path, name string) error {
	doc, err := read(path)
	if err != nil {
		return err
	}
	if !hasProfile(doc.Items, name) {
		return fmt.Errorf("profile %q is not registered in %s", name, path)
	}
	doc.Active = name
	return write(path, doc)
}

// Write creates or replaces the given profile in profiles.xml, keeping any
// other profiles already registered. With activate the new profile becomes
// the active one; otherwise the currently active profile keeps that status.
// Either way the file never ends up without an active profile - the signing
// tools silently ignore profiles.xml when active points nowhere. Returns the
// name left active.
func Write(path string, entry Entry, activate bool) (string, error) {
	existing, _ := read(path) // a missing or unreadable file starts fresh

	doc := profilesXML{Active: entry.Name, Version: "3.1"}
	if existing.Version != "" {
		doc.Version = existing.Version
	}
	for _, p := range existing.Items {
		if p.Name != entry.Name {
			doc.Items = append(doc.Items, p)
		}
	}
	doc.Items = append(doc.Items, profileXML{
		Name: entry.Name,
		Items: []profileItemXML{
			{Distributor: "0", Key: entry.AuthorP12Path, Password: entry.AuthorPassword},
			{Distributor: "1", Key: entry.DistributorP12, Password: entry.DistributorPasswd},
			{Distributor: "2"},
		},
	})
	if !activate && hasProfile(doc.Items, existing.Active) {
		doc.Active = existing.Active
	}

	return doc.Active, write(path, doc)
}

func hasProfile(items []profileXML, name string) bool {
	for _, p := range items {
		if p.Name == name {
			return true
		}
	}
	return false
}

// write serializes the document and atomically replaces profiles.xml.
func write(path string, doc profilesXML) error {
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing profiles.xml: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// profiles.xml may hold the user's other signing profiles - write to a
	// temp file and rename so a failure never truncates the original.
	content := []byte(xml.Header + string(out) + "\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
