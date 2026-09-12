package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"github.com/gifflet/tizen-cert-cli/internal/samsung"
	"github.com/gifflet/tizen-cert-cli/internal/sdb"
)

// progressCh carries issuance progress lines from the flow goroutine into
// the Bubble Tea event loop.
var progressCh chan string

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case devicesScannedMsg:
		m.scanning = false
		if msg.err != nil {
			m.scanErr = msg.err.Error()
		}
		m.rows = append(msg.rows, m.rows...)
		var cmds []tea.Cmd
		for i, r := range m.rows {
			if !r.Manual && r.DUID == "" && r.Err == "" {
				serial := strings.Fields(r.Label)[0]
				cmds = append(cmds, fetchDUIDCmd(i, serial))
			}
		}
		return m, tea.Batch(cmds...)

	case duidFetchedMsg:
		if msg.index < len(m.rows) {
			if msg.err != nil {
				m.rows[msg.index].Err = msg.err.Error()
				m.rows[msg.index].Selected = false
			} else {
				m.rows[msg.index].DUID = msg.duid
			}
		}
		return m, nil

	case renewScannedMsg:
		m.renewChoices = msg.choices
		return m, nil

	case profilesLoadedMsg:
		m.activeLoading = false
		m.activeNames = msg.names
		m.currentActive = msg.active
		if msg.err != nil {
			m.activeErr = msg.err.Error()
		}
		return m, nil

	case activeSetMsg:
		if msg.err != nil {
			m.activeErr = msg.err.Error()
			return m, nil
		}
		m.currentActive = msg.name
		m.step = stepActiveDone
		return m, nil

	case loginReadyMsg:
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			m.manualMode = true
			m.tokenInput.Focus()
			return m, nil
		}
		m.login = msg.server
		m.sshMode = samsung.InSSHSession()
		if !m.sshMode {
			if err := samsung.OpenBrowser(msg.server.URL); err != nil {
				m.sshMode = true
			}
		}
		return m, waitLoginCmd(msg.server)

	case loginDoneMsg:
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			m.manualMode = true
			m.tokenInput.Focus()
			return m, nil
		}
		m.session = msg.session
		m.step = stepIssue
		progressCh = make(chan string, 16)
		return m, tea.Batch(m.spin.Tick, m.startIssueCmd(progressCh), listenProgressCmd(progressCh))

	case issueProgressMsg:
		m.progress = append(m.progress, string(msg))
		return m, listenProgressCmd(progressCh)

	case issueDoneMsg:
		if msg.err != nil {
			m.issueErr = msg.err.Error()
		} else {
			m.result = msg.result
		}
		m.step = stepDone
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.step {
	case stepMode:
		return m.updateMode(msg)
	case stepRenewPick:
		return m.updateRenewPick(msg)
	case stepActivePick:
		return m.updateActivePick(msg)
	case stepDevices:
		return m.updateDevices(msg)
	case stepPrivilege:
		return m.updatePrivilege(msg)
	case stepDetails:
		return m.updateDetails(msg)
	case stepActivate:
		return m.updateActivate(msg)
	case stepLogin:
		return m.updateLogin(msg)
	case stepDone, stepActiveDone:
		if msg.String() == "q" || msg.Type == tea.KeyEnter || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updateMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.modeCursor > 0 {
			m.modeCursor--
		}
	case "down", "j":
		if m.modeCursor < 2 {
			m.modeCursor++
		}
	case "enter":
		// Every mode needs profiles.xml: the picker lists it, and the
		// create/renew flows use the current active profile to decide
		// whether the activation question is worth asking.
		switch m.modeCursor {
		case 1:
			m.renewing = true
			m.step = stepRenewPick
			return m, tea.Batch(scanRenewCmd, loadProfilesCmd)
		case 2:
			m.step = stepActivePick
			m.activeLoading = true
			m.activeErr = ""
			return m, loadProfilesCmd
		default:
			m.step = stepDevices
			m.scanning = true
			return m, tea.Batch(scanDevicesCmd, loadProfilesCmd)
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) updateActivePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.activeCursor > 0 {
			m.activeCursor--
		}
	case "down", "j":
		if m.activeCursor < len(m.activeNames)-1 {
			m.activeCursor++
		}
	case "esc":
		m.step = stepMode
	case "enter":
		if len(m.activeNames) == 0 {
			return m, nil
		}
		return m, setActiveCmd(m.activeNames[m.activeCursor])
	}
	return m, nil
}

func (m model) updateRenewPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.renewCursor > 0 {
			m.renewCursor--
		}
	case "down", "j":
		if m.renewCursor < len(m.renewChoices)-1 {
			m.renewCursor++
		}
	case "esc":
		m.step = stepMode
	case "enter":
		if len(m.renewChoices) == 0 {
			return m, nil
		}
		choice := m.renewChoices[m.renewCursor]
		if choice.Info == nil {
			return m, nil
		}
		// Pre-populate the wizard with everything the old profile knew:
		// its DUIDs become manual rows and its folder name the default.
		for _, duid := range choice.Info.DUIDs {
			m.rows = append(m.rows, deviceRow{
				Label:    "renewed: " + truncate(duid, 40),
				DUID:     duid,
				Manual:   true,
				Selected: true,
			})
		}
		m.inputs[fieldName].SetValue(strings.TrimSuffix(filepathBase(choice.Dir), "/"))
		m.step = stepDevices
		m.scanning = true
		return m, scanDevicesCmd
	}
	return m, nil
}

func (m model) updateDevices(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addingDUID {
		switch msg.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(m.duidInput.Value())
			m.duidInput.SetValue("")
			m.addingDUID = false
			if value == "" {
				return m, nil
			}
			// An IP address means "connect via sdb and fetch the DUID";
			// anything else is treated as a literal DUID.
			if looksLikeAddress(value) {
				index := len(m.rows)
				m.rows = append(m.rows, deviceRow{Label: value + " (connecting...)", Selected: true})
				return m, func() tea.Msg {
					if err := sdb.Connect(value); err != nil {
						return duidFetchedMsg{index: index, err: err}
					}
					serial := value
					if !strings.Contains(serial, ":") {
						serial += ":26101"
					}
					duid, err := sdb.FetchDUID(serial)
					return duidFetchedMsg{index: index, duid: duid, err: err}
				}
			}
			m.rows = append(m.rows, deviceRow{Label: "manual: " + truncate(value, 40), DUID: value, Manual: true, Selected: true})
			return m, nil
		case tea.KeyEsc:
			m.addingDUID = false
			return m, nil
		}
		var cmd tea.Cmd
		m.duidInput, cmd = m.duidInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.devCursor > 0 {
			m.devCursor--
		}
	case "down", "j":
		if m.devCursor < len(m.rows)-1 {
			m.devCursor++
		}
	case " ":
		if len(m.rows) > 0 {
			row := &m.rows[m.devCursor]
			if row.DUID != "" || row.Manual {
				row.Selected = !row.Selected
			}
		}
	case "a":
		m.addingDUID = true
		m.duidInput.Focus()
		return m, nil
	case "r":
		m.scanning = true
		m.scanErr = ""
		// Keep manual rows, rescan the rest.
		var manual []deviceRow
		for _, r := range m.rows {
			if r.Manual {
				manual = append(manual, r)
			}
		}
		m.rows = manual
		return m, scanDevicesCmd
	case "esc":
		m.step = stepMode
	case "enter":
		if m.selectedDUIDCount() == 0 {
			m.scanErr = "select at least one device with a resolved DUID (or add one manually with 'a')"
			return m, nil
		}
		m.scanErr = ""
		m.step = stepPrivilege
	}
	return m, nil
}

func (m model) updatePrivilege(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.privCursor = 0
	case "down", "j":
		m.privCursor = 1
	case "esc":
		m.step = stepDevices
	case "enter":
		m.step = stepDetails
	}
	return m, nil
}

func (m model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.step = stepPrivilege
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		m.fieldFocus = (m.fieldFocus + 1) % fieldCount
		return m.refocusDetails(), nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.fieldFocus = (m.fieldFocus + fieldCount - 1) % fieldCount
		return m.refocusDetails(), nil
	case tea.KeyEnter:
		if m.fieldFocus < fieldCount-1 {
			m.fieldFocus++
			return m.refocusDetails(), nil
		}
		if err := m.validateDetails(); err != "" {
			m.err = err
			return m, nil
		}
		m.err = ""
		// Only ask about the active profile when there is a real choice:
		// another profile is active today and would lose that status.
		name := strings.TrimSpace(m.inputs[fieldName].Value())
		if m.currentActive == "" || m.currentActive == name {
			m.makeActive = true
			m.step = stepLogin
			return m, startLoginCmd
		}
		m.activateCursor = 0
		m.step = stepActivate
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[m.fieldFocus], cmd = m.inputs[m.fieldFocus].Update(msg)
	return m, cmd
}

func (m model) updateActivate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.activateCursor = 0
	case "down", "j":
		m.activateCursor = 1
	case "esc":
		m.step = stepDetails
	case "enter":
		m.makeActive = m.activateCursor == 0
		m.step = stepLogin
		return m, startLoginCmd
	}
	return m, nil
}

func (m model) refocusDetails() model {
	for i := range m.inputs {
		if i == m.fieldFocus {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return m
}

func (m model) validateDetails() string {
	name := strings.TrimSpace(m.inputs[fieldName].Value())
	if name == "" {
		return "the profile name is required"
	}
	// The name becomes a folder under ~/SamsungCertificate and the profile id.
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return "the profile name may only contain letters, digits, '-', '_' and '.'"
		}
	}
	if !strings.Contains(m.inputs[fieldEmail].Value(), "@") {
		return "a valid e-mail is required"
	}
	// Samsung's certificate manager password policy.
	pass := m.inputs[fieldPassword].Value()
	var hasUpper, hasLower, hasDigit bool
	for _, r := range pass {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if len(pass) < 8 || !hasUpper || !hasLower || !hasDigit {
		return "password policy: minimum 8 characters with at least 1 upper case, 1 lower case and 1 numeral"
	}
	return ""
}

func (m model) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.manualMode {
		switch msg.Type {
		case tea.KeyEnter:
			session, err := samsung.ParseManualInput(strings.TrimSpace(m.tokenInput.Value()))
			if err != nil {
				m.loginErr = err.Error()
				return m, nil
			}
			m.session = session
			m.step = stepIssue
			progressCh = make(chan string, 16)
			return m, tea.Batch(m.spin.Tick, m.startIssueCmd(progressCh), listenProgressCmd(progressCh))
		case tea.KeyEsc:
			m.step = stepDetails
			return m, nil
		}
		var cmd tea.Cmd
		m.tokenInput, cmd = m.tokenInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "c":
		if m.login != nil {
			// OSC 52 reaches the user's local clipboard even over SSH.
			termenv.NewOutput(os.Stdout).Copy(m.login.URL)
			m.urlCopied = true
		}
	case "m":
		m.manualMode = true
		m.tokenInput.Focus()
	case "esc":
		if m.login != nil {
			m.login.Close()
		}
		m.step = stepDetails
	}
	return m, nil
}

func (m model) selectedDUIDCount() int {
	count := 0
	for _, r := range m.rows {
		if r.Selected && r.DUID != "" {
			count++
		}
	}
	return count
}

func looksLikeAddress(s string) bool {
	dots := strings.Count(s, ".")
	return dots == 3 && !strings.Contains(s, "#") && len(s) < 25
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func filepathBase(path string) string {
	idx := strings.LastIndexAny(path, "/\\")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
