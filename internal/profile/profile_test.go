package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreatesAndPreservesProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.xml")

	first := Entry{
		Name:              "lab-a",
		AuthorP12Path:     "/certs/a/author.p12",
		AuthorPassword:    "encA",
		DistributorP12:    "/certs/a/distributor.p12",
		DistributorPasswd: "encA",
	}
	if err := Write(path, first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Name = "lab-b"
	if err := Write(path, second); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, want := range []string{`name="lab-a"`, `name="lab-b"`, `active="lab-b"`, `distributor="0"`, `distributor="1"`, `distributor="2"`} {
		if !strings.Contains(content, want) {
			t.Errorf("profiles.xml missing %s\n%s", want, content)
		}
	}
}

func TestWriteReplacesSameName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.xml")
	entry := Entry{Name: "lab", AuthorP12Path: "old.p12", AuthorPassword: "x", DistributorP12: "old-d.p12", DistributorPasswd: "x"}
	if err := Write(path, entry); err != nil {
		t.Fatal(err)
	}
	entry.AuthorP12Path = "new.p12"
	if err := Write(path, entry); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "old.p12") {
		t.Error("old entry was not replaced")
	}
	if strings.Count(content, `name="lab"`) != 1 {
		t.Error("duplicate profile entries for the same name")
	}
}
