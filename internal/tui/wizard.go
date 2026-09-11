// Package tui implements the interactive wizard that drives the whole
// certificate generation flow, built on Bubble Tea.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gifflet/tizen-cert-cli/internal/flow"
	"github.com/gifflet/tizen-cert-cli/internal/pki"
	"github.com/gifflet/tizen-cert-cli/internal/samsung"
	"github.com/gifflet/tizen-cert-cli/internal/sdb"
)

type step int

const (
	stepMode step = iota
	stepRenewPick
	stepDevices
	stepPrivilege
	stepDetails
	stepLogin
	stepIssue
	stepDone
)

// detailField indexes the inputs of the details step.
const (
	fieldName = iota
	fieldEmail
	fieldPassword
	fieldCount
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// deviceRow is one selectable line in the devices step: either a TV found
// via sdb or a DUID typed in manually.
type deviceRow struct {
	Label    string
	DUID     string
	Manual   bool
	Selected bool
	Err      string
}

// renewChoice is one existing certificate folder offered for renewal.
type renewChoice struct {
	Dir   string
	Info  *pki.CertInfo
	Error string
}

type model struct {
	step step

	// mode
	modeCursor int
	renewing   bool

	// renew
	renewChoices []renewChoice
	renewCursor  int

	// devices
	rows       []deviceRow
	devCursor  int
	duidInput  textinput.Model
	addingDUID bool
	scanErr    string
	scanning   bool

	// privilege
	privCursor int

	// details
	inputs     [fieldCount]textinput.Model
	fieldFocus int

	// login
	login      *samsung.LoginServer
	loginErr   string
	manualMode bool
	sshMode    bool
	urlCopied  bool
	tokenInput textinput.Model
	session    samsung.Session

	// issue
	spin      spinner.Model
	progress  []string
	issueErr  string
	result    *flow.Result

	width int
	err   string
}

// New builds the wizard's initial model.
func New() tea.Model {
	duid := textinput.New()
	duid.Placeholder = "2.0#... (DUID) or TV IP address to connect"
	duid.CharLimit = 120
	duid.Width = 60

	token := textinput.New()
	token.Placeholder = "paste the access_token[:user_id] value here"
	token.CharLimit = 400
	token.Width = 70

	var inputs [fieldCount]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].CharLimit = 100
		inputs[i].Width = 46
	}
	inputs[fieldName].Placeholder = "certificate/profile name (e.g. lab-tvs)"
	inputs[fieldEmail].Placeholder = "e-mail (goes into the certificates)"
	inputs[fieldPassword].Placeholder = "password for the .p12 files and profile"
	inputs[fieldPassword].EchoMode = textinput.EchoPassword
	inputs[fieldName].Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		step:      stepMode,
		duidInput: duid,
		tokenInput: token,
		inputs:    inputs,
		spin:      sp,
	}
}

func (m model) Init() tea.Cmd { return nil }

// ---- messages produced by async commands ----

type devicesScannedMsg struct {
	rows []deviceRow
	err  error
}

type duidFetchedMsg struct {
	index int
	duid  string
	err   error
}

type renewScannedMsg struct{ choices []renewChoice }

type loginReadyMsg struct {
	server *samsung.LoginServer
	err    error
}

type loginDoneMsg struct {
	session samsung.Session
	err     error
}

type issueProgressMsg string

type issueDoneMsg struct {
	result *flow.Result
	err    error
}

// ---- commands ----

func scanDevicesCmd() tea.Msg {
	if !sdb.Available() {
		return devicesScannedMsg{err: fmt.Errorf("sdb not found in PATH - connected TVs cannot be detected (DUIDs can still be entered manually)")}
	}
	devices, err := sdb.ListDevices()
	if err != nil {
		return devicesScannedMsg{err: err}
	}
	rows := make([]deviceRow, 0, len(devices))
	for _, d := range devices {
		label := d.Serial
		if d.Name != "" {
			label += "  " + d.Name
		}
		if d.State != "device" {
			label += "  (" + d.State + ")"
		}
		rows = append(rows, deviceRow{Label: label, Selected: d.State == "device"})
	}
	return devicesScannedMsg{rows: rows}
}

func fetchDUIDCmd(index int, serial string) tea.Cmd {
	return func() tea.Msg {
		duid, err := sdb.FetchDUID(serial)
		return duidFetchedMsg{index: index, duid: duid, err: err}
	}
}

func scanRenewCmd() tea.Msg {
	home, err := os.UserHomeDir()
	if err != nil {
		return renewScannedMsg{}
	}
	base := filepath.Join(home, "SamsungCertificate")
	entries, err := os.ReadDir(base)
	if err != nil {
		return renewScannedMsg{}
	}
	var choices []renewChoice
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		choice := renewChoice{Dir: dir}
		info, err := pki.InspectCertFile(filepath.Join(dir, "distributor.crt"))
		if err != nil {
			choice.Error = "no readable distributor.crt"
		} else {
			choice.Info = info
		}
		choices = append(choices, choice)
	}
	return renewScannedMsg{choices: choices}
}

func startLoginCmd() tea.Msg {
	server, err := samsung.StartLogin()
	return loginReadyMsg{server: server, err: err}
}

func waitLoginCmd(server *samsung.LoginServer) tea.Cmd {
	return func() tea.Msg {
		session, err := server.Wait()
		return loginDoneMsg{session: session, err: err}
	}
}

func (m model) startIssueCmd(progress chan string) tea.Cmd {
	cfg := m.buildFlowConfig()
	return func() tea.Msg {
		result, err := flow.Run(cfg, func(msg string) { progress <- msg })
		close(progress)
		return issueDoneMsg{result: result, err: err}
	}
}

func listenProgressCmd(progress chan string) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-progress
		if !ok {
			return nil
		}
		return issueProgressMsg(msg)
	}
}

func (m model) buildFlowConfig() flow.Config {
	var duids []string
	for _, r := range m.rows {
		if r.Selected && r.DUID != "" {
			duids = append(duids, r.DUID)
		}
	}
	privilege := "Public"
	if m.privCursor == 1 {
		privilege = "Partner"
	}
	return flow.Config{
		ProfileName: strings.TrimSpace(m.inputs[fieldName].Value()),
		Email:       strings.TrimSpace(m.inputs[fieldEmail].Value()),
		Password:    m.inputs[fieldPassword].Value(),
		Privilege:   privilege,
		DUIDs:       duids,
		Session:     m.session,
	}
}
