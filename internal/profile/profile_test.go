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
	if _, err := Write(path, first, true); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Name = "lab-b"
	if _, err := Write(path, second, true); err != nil {
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
	if _, err := Write(path, entry, true); err != nil {
		t.Fatal(err)
	}
	entry.AuthorP12Path = "new.p12"
	if _, err := Write(path, entry, true); err != nil {
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

func TestWriteKeepsCurrentActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.xml")
	if _, err := Write(path, Entry{Name: "lab-a"}, true); err != nil {
		t.Fatal(err)
	}
	active, err := Write(path, Entry{Name: "lab-b"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if active != "lab-a" {
		t.Errorf("active = %q, want lab-a", active)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, `active="lab-a"`) {
		t.Errorf("active attribute changed:\n%s", content)
	}
	if !strings.Contains(content, `name="lab-b"`) {
		t.Errorf("new profile not registered:\n%s", content)
	}
}

func TestWriteActivatesOnFreshFileEvenWhenNotAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.xml")
	active, err := Write(path, Entry{Name: "lab"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if active != "lab" {
		t.Errorf("active = %q, want lab (a profiles.xml without an active profile is useless)", active)
	}
}

func TestListAndSetActive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.xml")
	if _, err := Write(path, Entry{Name: "lab-a"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(path, Entry{Name: "lab-b"}, true); err != nil {
		t.Fatal(err)
	}

	names, active, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "lab-a" || names[1] != "lab-b" {
		t.Errorf("names = %v", names)
	}
	if active != "lab-b" {
		t.Errorf("active = %q, want lab-b", active)
	}

	if err := SetActive(path, "lab-a"); err != nil {
		t.Fatal(err)
	}
	names, active, err = List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("SetActive dropped profiles: %v", names)
	}
	if active != "lab-a" {
		t.Errorf("active after SetActive = %q, want lab-a", active)
	}

	if err := SetActive(path, "nope"); err == nil {
		t.Error("expected an error for an unregistered profile name")
	}
}

func TestListMissingFile(t *testing.T) {
	names, active, err := List(filepath.Join(t.TempDir(), "none.xml"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if names != nil || active != "" {
		t.Errorf("expected empty result, got %v / %q", names, active)
	}
}
