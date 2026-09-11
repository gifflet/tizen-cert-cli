package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func typeText(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	for _, r := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// Walks the wizard from the mode screen to the details validation,
// exercising every step transition without touching sdb or Samsung.
func TestWizardHappyPathUpToLogin(t *testing.T) {
	var m tea.Model = New()

	if view := m.View(); !strings.Contains(view, "What do you want to do?") {
		t.Fatalf("mode view unexpected:\n%s", view)
	}

	// Enter on "new profile" moves to devices (scan runs async; simulate result).
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(devicesScannedMsg{rows: nil})
	if view := m.View(); !strings.Contains(view, "press 'a' to add a DUID") {
		t.Fatalf("devices view unexpected:\n%s", view)
	}

	// Enter without any DUID must refuse to advance.
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "select at least one device") {
		t.Fatalf("expected DUID validation message:\n%s", view)
	}

	// Add a manual DUID.
	m, _ = m.Update(key("a"))
	m = typeText(t, m, "AAAABBBBCCCCDDDDEEEEFFFF00001111")
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "1 device(s) selected") {
		t.Fatalf("manual DUID not registered:\n%s", view)
	}

	// Advance to privilege, pick Public, advance to details.
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "privilege level") {
		t.Fatalf("privilege view unexpected:\n%s", view)
	}
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "Certificate details") {
		t.Fatalf("details view unexpected:\n%s", view)
	}

	// Fill the three fields; a weak password must be rejected.
	m = typeText(t, m, "lab-tvs")
	m, _ = m.Update(key("tab"))
	m = typeText(t, m, "lab@example.com")
	m, _ = m.Update(key("tab"))
	m = typeText(t, m, "weak")
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "password policy") {
		t.Fatalf("expected password policy error:\n%s", view)
	}

	// A profile name with a slash must also be rejected.
	inner := m.(model)
	inner.inputs[fieldPassword].SetValue("Str0ngPass")
	inner.inputs[fieldName].SetValue("bad/name")
	if msg := inner.validateDetails(); !strings.Contains(msg, "profile name may only contain") {
		t.Fatalf("expected name validation, got %q", msg)
	}
	inner.inputs[fieldName].SetValue("lab-tvs")
	if msg := inner.validateDetails(); msg != "" {
		t.Fatalf("expected valid details, got %q", msg)
	}

	// buildFlowConfig must carry the selections through.
	cfg := inner.buildFlowConfig()
	if len(cfg.DUIDs) != 1 || cfg.DUIDs[0] != "AAAABBBBCCCCDDDDEEEEFFFF00001111" {
		t.Errorf("flow config DUIDs = %v", cfg.DUIDs)
	}
	if cfg.Privilege != "Public" {
		t.Errorf("flow config privilege = %q", cfg.Privilege)
	}
}

func TestWizardRenewPrepopulatesDUIDs(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter")) // renew mode
	m, _ = m.Update(renewScannedMsg{})
	if view := m.View(); !strings.Contains(view, "no folders found") {
		t.Fatalf("renew view unexpected:\n%s", view)
	}
}
