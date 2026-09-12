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

// Walks the standalone set-active mode: pick a profile, simulate the write,
// land on the confirmation screen.
func TestWizardSetActiveFlow(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter")) // set-active mode
	if view := m.View(); !strings.Contains(view, "reading profiles.xml") {
		t.Fatalf("expected the loading state:\n%s", view)
	}

	m, _ = m.Update(profilesLoadedMsg{names: []string{"lab-a", "lab-b"}, active: "lab-a"})
	view := m.View()
	if !strings.Contains(view, "lab-b") || !strings.Contains(view, "currently active") {
		t.Fatalf("picker view unexpected:\n%s", view)
	}

	// Pick lab-b; the write runs async - only check a command was issued.
	m, _ = m.Update(key("j"))
	var cmd tea.Cmd
	m, cmd = m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("expected a set-active command")
	}
	m, _ = m.Update(activeSetMsg{name: "lab-b"})
	if view := m.View(); !strings.Contains(view, "Active profile set") {
		t.Fatalf("done view unexpected:\n%s", view)
	}
}

func TestWizardSetActiveEmptyList(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(profilesLoadedMsg{})
	if view := m.View(); !strings.Contains(view, "no profiles registered yet") {
		t.Fatalf("empty picker view unexpected:\n%s", view)
	}
	// Enter with nothing to pick must stay put.
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "no profiles registered yet") {
		t.Fatalf("expected to stay on the picker:\n%s", view)
	}
}

// fillCreateFlowToDetails walks create mode up to a filled details step.
func fillCreateFlowToDetails(t *testing.T, m tea.Model) tea.Model {
	t.Helper()
	m, _ = m.Update(key("enter")) // create mode
	m, _ = m.Update(devicesScannedMsg{})
	m, _ = m.Update(key("a"))
	m = typeText(t, m, "AAAABBBBCCCCDDDDEEEEFFFF00001111")
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(key("enter")) // -> privilege
	m, _ = m.Update(key("enter")) // -> details
	m = typeText(t, m, "lab-tvs")
	m, _ = m.Update(key("tab"))
	m = typeText(t, m, "lab@example.com")
	m, _ = m.Update(key("tab"))
	m = typeText(t, m, "Str0ngPass")
	return m
}

// When another profile is active, confirming the details must detour through
// the activation question, and "keep" must reach login with MakeActive false.
func TestWizardActivateChoiceWhenAnotherProfileActive(t *testing.T) {
	var m tea.Model = New()
	m = fillCreateFlowToDetails(t, m)
	m, _ = m.Update(profilesLoadedMsg{names: []string{"old"}, active: "old"})
	m, _ = m.Update(key("enter"))
	if view := m.View(); !strings.Contains(view, "the active signing profile?") {
		t.Fatalf("expected the activate step:\n%s", view)
	}

	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter")) // keep "old" active
	inner := m.(model)
	if inner.step != stepLogin {
		t.Fatalf("step = %v, want stepLogin", inner.step)
	}
	if inner.buildFlowConfig().MakeActive {
		t.Error("MakeActive should be false when keeping the current profile")
	}
}

// With no active profile (or the same name being reissued) the question is
// pointless: skip straight to login and activate the new profile.
func TestWizardActivateSkippedWithoutCurrentActive(t *testing.T) {
	var m tea.Model = New()
	m = fillCreateFlowToDetails(t, m)
	m, _ = m.Update(key("enter"))
	inner := m.(model)
	if inner.step != stepLogin {
		t.Fatalf("step = %v, want stepLogin", inner.step)
	}
	if !inner.buildFlowConfig().MakeActive {
		t.Error("MakeActive should default to true when no other profile is active")
	}

	m = New()
	m = fillCreateFlowToDetails(t, m)
	m, _ = m.Update(profilesLoadedMsg{names: []string{"lab-tvs"}, active: "lab-tvs"})
	m, _ = m.Update(key("enter"))
	inner = m.(model)
	if inner.step != stepLogin {
		t.Fatalf("renaming the active profile: step = %v, want stepLogin", inner.step)
	}
	if !inner.buildFlowConfig().MakeActive {
		t.Error("MakeActive should be true when reissuing the active profile")
	}
}
