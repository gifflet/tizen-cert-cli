// Package profile writes the Tizen security profile registration
// (profiles.xml) directly, so no Tizen Studio CLI is needed on the machine.
package profile

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
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

// DefaultProfilesPath returns the profiles.xml location used by Tizen
// Studio's data directory in the user home. The file may not exist yet.
func DefaultProfilesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "tizen-studio-data", "profile", "profiles.xml"), nil
}

// Write creates or replaces the given profile in profiles.xml, keeping any
// other profiles already registered, and marks it as the active one.
func Write(path string, entry Entry) error {
	doc := profilesXML{Active: entry.Name, Version: "3.1"}

	if data, err := os.ReadFile(path); err == nil {
		var existing profilesXML
		if err := xml.Unmarshal(data, &existing); err == nil {
			for _, p := range existing.Items {
				if p.Name != entry.Name {
					doc.Items = append(doc.Items, p)
				}
			}
			if existing.Version != "" {
				doc.Version = existing.Version
			}
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
