package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gifflet/tizen-cert-cli/internal/pki"
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

// Picking a profile to renew hands its folder to the flow so the author is reused.
func TestWizardRenewReusesAuthorFolder(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter")) // renew mode
	m, _ = m.Update(renewScannedMsg{choices: []renewChoice{{Dir: "/certs/lab", Info: &pki.CertInfo{}}}})
	m, _ = m.Update(key("enter"))
	if got := m.(model).buildFlowConfig().ReuseAuthorFrom; got != "/certs/lab" {
		t.Fatalf("ReuseAuthorFrom = %q, want /certs/lab", got)
	}
}

// A renewal abandoned with esc must not make a new profile reuse the old author.
func TestWizardCreateAfterAbandonedRenewDoesNotReuseAuthor(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("j"))
	m, _ = m.Update(key("enter")) // renew mode
	m, _ = m.Update(renewScannedMsg{choices: []renewChoice{{Dir: "/certs/lab", Info: &pki.CertInfo{}}}})
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(key("esc"))
	for m.(model).step != stepMode {
		m, _ = m.Update(key("esc"))
	}
	m, _ = m.Update(key("k"))
	m, _ = m.Update(key("enter")) // create mode
	if got := m.(model).buildFlowConfig().ReuseAuthorFrom; got != "" {
		t.Fatalf("ReuseAuthorFrom = %q, want empty in create mode", got)
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

// Picking the same renew profile twice must not duplicate rows.
func TestRenewSameProfileTwiceNoDuplicateRows(t *testing.T) {
	info := &pki.CertInfo{DUIDs: []string{"DUID-1", "DUID-2"}}
	choices := []renewChoice{{Dir: "/tmp/prof1", Info: info}}

	m := New().(model)
	m.step = stepRenewPick
	m.renewChoices = choices

	// First pick.
	result, _ := m.Update(key("enter"))
	m = result.(model)
	if len(m.rows) != 2 {
		t.Fatalf("after first pick: expected 2 rows, got %d", len(m.rows))
	}

	// Simulate going back to renew pick without clearing rows.
	m.step = stepRenewPick

	// Second pick of the same profile.
	result, _ = m.Update(key("enter"))
	m = result.(model)
	if len(m.rows) != 2 {
		t.Errorf("after second pick: expected 2 rows, got %d (duplicates present)", len(m.rows))
	}
}

// A cert that already has duplicate DUIDs must produce only one row per DUID.
func TestRenewCertWithDuplicateDUIDsGeneratesOneRow(t *testing.T) {
	info := &pki.CertInfo{DUIDs: []string{"DUID-1", "DUID-1", "DUID-2"}}
	choices := []renewChoice{{Dir: "/tmp/prof1", Info: info}}

	m := New().(model)
	m.step = stepRenewPick
	m.renewChoices = choices

	result, _ := m.Update(key("enter"))
	m = result.(model)

	if len(m.rows) != 2 {
		t.Errorf("expected 2 rows (deduped), got %d", len(m.rows))
	}
	seen := make(map[string]bool)
	for _, r := range m.rows {
		if seen[r.DUID] {
			t.Errorf("duplicate DUID %q in rows", r.DUID)
		}
		seen[r.DUID] = true
	}
}

// A repeated scan must not add new copies of already-listed TVs.
func TestRepeatedScanDoesNotDuplicateTVs(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(key("enter")) // create mode → stepDevices

	scanned := []deviceRow{{Label: "192.168.1.100  MyTV", DUID: "DUID-A", Selected: true}}

	m, _ = m.Update(devicesScannedMsg{rows: scanned})
	if got := len(m.(model).rows); got != 1 {
		t.Fatalf("after first scan: expected 1 row, got %d", got)
	}

	// Second scan with the same result.
	m, _ = m.Update(devicesScannedMsg{rows: scanned})
	if got := len(m.(model).rows); got != 1 {
		t.Errorf("after second scan: expected 1 row, got %d (duplicates present)", got)
	}
}

// TestDUIDFetchedAppliesCorrectlyAfterMerge verifies that async DUID results
// are applied to the right row even when a prior result caused a merge+removal
// that shifted row positions.
func TestDUIDFetchedAppliesCorrectlyAfterMerge(t *testing.T) {
	m := New().(model)
	m.step = stepDevices

	// Row A: scan result, pending DUID fetch.
	m.nextRowID++
	rowAID := m.nextRowID
	// Row X: renewed entry with a known DUID.
	m.nextRowID++
	rowXID := m.nextRowID
	// Row IP: IP-based entry, pending DUID fetch.
	m.nextRowID++
	rowIPID := m.nextRowID

	m.rows = []deviceRow{
		{ID: rowAID, Label: "serial-A", Selected: true},
		{ID: rowXID, Label: "renewed: DUID-X", DUID: "DUID-X", Manual: true, Selected: true},
		{ID: rowIPID, Label: "192.168.1.1 (connecting...)", Selected: true},
	}

	// A's DUID arrives as "DUID-X" — same as row X — so X is removed (merged).
	result, _ := m.Update(duidFetchedMsg{rowID: rowAID, duid: "DUID-X"})
	m = result.(model)

	if len(m.rows) != 2 {
		t.Fatalf("after merge: expected 2 rows, got %d", len(m.rows))
	}
	foundIP := false
	for _, r := range m.rows {
		if r.ID == rowIPID {
			foundIP = true
		}
	}
	if !foundIP {
		t.Fatal("IP row disappeared after merge")
	}

	// IP's DUID arrives — must be applied to the IP row, not dropped or misapplied.
	result, _ = m.Update(duidFetchedMsg{rowID: rowIPID, duid: "DUID-IP"})
	m = result.(model)

	if len(m.rows) != 2 {
		t.Fatalf("after IP DUID: expected 2 rows, got %d", len(m.rows))
	}
	for _, r := range m.rows {
		if r.ID == rowIPID {
			if r.DUID != "DUID-IP" {
				t.Errorf("IP row DUID = %q, want DUID-IP", r.DUID)
			}
			return
		}
	}
	t.Error("IP row not found after DUID fetch")
}

// A renewed DUID merged into a scanned TV must survive a rescan without that TV.
func TestMergedRenewedDUIDSurvivesRescan(t *testing.T) {
	m := New().(model)
	m.step = stepDevices
	m.rows = []deviceRow{
		{ID: 1, Label: "serial-A", Selected: true},
		{ID: 2, Label: "renewed: DUID-X", DUID: "DUID-X", Manual: true, Selected: true},
	}
	m.nextRowID = 2

	result, _ := m.Update(duidFetchedMsg{rowID: 1, duid: "DUID-X"})
	result, _ = result.Update(devicesScannedMsg{}) // TV A is gone
	m = result.(model)

	if len(m.rows) != 1 || m.rows[0].DUID != "DUID-X" {
		t.Fatalf("expected DUID-X to be kept after rescan, got %+v", m.rows)
	}
}

// Rows shrinking under the cursor (merge or rescan) must not leave it out of range.
func TestDevCursorClampedWhenRowsShrink(t *testing.T) {
	m := New().(model)
	m.step = stepDevices
	m.rows = []deviceRow{
		{ID: 1, Label: "serial-A", Scanned: true, Selected: true},
		{ID: 2, Label: "renewed: DUID-X", DUID: "DUID-X", Manual: true, Selected: true},
	}
	m.nextRowID = 2
	m.devCursor = 1

	result, _ := m.Update(duidFetchedMsg{rowID: 1, duid: "DUID-X"})
	result, _ = result.Update(key(" ")) // panicked with index out of range before
	if got := result.(model).devCursor; got != 0 {
		t.Fatalf("devCursor = %d, want 0", got)
	}
}

// Re-entering the devices step keeps address-added rows and the user's choices.
func TestRescanKeepsAddressRowsAndSelection(t *testing.T) {
	m := New().(model)
	m.step = stepDevices
	m.rows = []deviceRow{
		{ID: 1, Label: "10.0.0.5:26101  TV-A", DUID: "DUID-A", Scanned: true, Selected: false},
		{ID: 2, Label: "10.0.0.9 (connecting...)", DUID: "DUID-B", Selected: true},
	}
	m.nextRowID = 2

	result, _ := m.Update(devicesScannedMsg{rows: []deviceRow{
		{Label: "10.0.0.5:26101  TV-A", Scanned: true, Selected: true},
	}})
	m = result.(model)

	if len(m.rows) != 2 {
		t.Fatalf("expected 2 rows, got %+v", m.rows)
	}
	if m.rows[0].Selected {
		t.Error("TV-A was deselected by the user and must stay deselected")
	}
	if m.rows[1].DUID != "DUID-B" {
		t.Errorf("address-added row lost, rows = %+v", m.rows)
	}
}

// buildFlowConfig must not emit the same DUID twice even if m.rows has duplicates.
func TestBuildFlowConfigDeduplicatesDUIDs(t *testing.T) {
	m := New().(model)
	m.rows = []deviceRow{
		{DUID: "DUID-X", Selected: true},
		{DUID: "DUID-X", Selected: true},
		{DUID: "DUID-Y", Selected: true},
	}

	cfg := m.buildFlowConfig()

	if len(cfg.DUIDs) != 2 {
		t.Errorf("expected 2 DUIDs, got %d: %v", len(cfg.DUIDs), cfg.DUIDs)
	}
	seen := make(map[string]bool)
	for _, d := range cfg.DUIDs {
		if seen[d] {
			t.Errorf("duplicate DUID %q in flow config", d)
		}
		seen[d] = true
	}
}
