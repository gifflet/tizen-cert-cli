package tui

import (
	"fmt"
	"strings"

	"github.com/gifflet/tizen-cert-cli/internal/samsung"
)

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Tizen TV Certificate Profile Generator") + "\n")
	b.WriteString(subtleStyle.Render("Samsung author + distributor certificates, end to end") + "\n\n")

	switch m.step {
	case stepMode:
		b.WriteString(m.viewMode())
	case stepRenewPick:
		b.WriteString(m.viewRenewPick())
	case stepDevices:
		b.WriteString(m.viewDevices())
	case stepPrivilege:
		b.WriteString(m.viewPrivilege())
	case stepDetails:
		b.WriteString(m.viewDetails())
	case stepLogin:
		b.WriteString(m.viewLogin())
	case stepIssue:
		b.WriteString(m.viewIssue())
	case stepDone:
		b.WriteString(m.viewDone())
	}

	if m.err != "" {
		b.WriteString("\n" + errStyle.Render("✗ "+m.err) + "\n")
	}
	return b.String()
}

func option(cursor bool, label, description string) string {
	prefix := "  "
	if cursor {
		prefix = cursorStyle.Render("> ")
		label = selectedStyle.Render(label)
	}
	return prefix + label + "\n    " + subtleStyle.Render(description) + "\n"
}

func (m model) viewMode() string {
	var b strings.Builder
	b.WriteString("What do you want to do?\n\n")
	b.WriteString(option(m.modeCursor == 0, "Create a new certificate profile", "generate author + distributor certificates for selected TVs"))
	b.WriteString(option(m.modeCursor == 1, "Renew an existing profile", "reuse the DUIDs of an expired profile from ~/SamsungCertificate"))
	b.WriteString("\n" + subtleStyle.Render("↑/↓ move · enter confirm · q quit"))
	return b.String()
}

func (m model) viewRenewPick() string {
	var b strings.Builder
	b.WriteString("Select the profile to renew:\n\n")
	if len(m.renewChoices) == 0 {
		b.WriteString(subtleStyle.Render("  no folders found in ~/SamsungCertificate") + "\n")
	}
	for i, c := range m.renewChoices {
		label := filepathBase(c.Dir)
		desc := c.Error
		if c.Info != nil {
			status := okStyle.Render("valid until " + c.Info.NotAfter.Format("2006-01-02"))
			if c.Info.Expired {
				status = errStyle.Render("EXPIRED " + c.Info.NotAfter.Format("2006-01-02"))
			}
			desc = fmt.Sprintf("%s · %d DUID(s)", status, len(c.Info.DUIDs))
		}
		b.WriteString(option(i == m.renewCursor, label, desc))
	}
	b.WriteString("\n" + subtleStyle.Render("↑/↓ move · enter confirm · esc back"))
	return b.String()
}

func (m model) viewDevices() string {
	var b strings.Builder
	b.WriteString("Target TVs (the distributor certificate is bound to their DUIDs):\n\n")

	if m.scanning {
		b.WriteString(subtleStyle.Render("  scanning sdb devices...") + "\n")
	}
	if m.scanErr != "" {
		b.WriteString(errStyle.Render("  ! "+m.scanErr) + "\n")
	}
	if len(m.rows) == 0 && !m.scanning {
		b.WriteString(subtleStyle.Render("  no devices - press 'a' to add a DUID or TV address manually") + "\n")
	}

	for i, r := range m.rows {
		check := "[ ]"
		if r.Selected {
			check = okStyle.Render("[x]")
		}
		line := fmt.Sprintf("%s %s", check, r.Label)
		switch {
		case r.Err != "":
			line += "  " + errStyle.Render("DUID error: "+truncate(r.Err, 40))
		case r.DUID == "" && !r.Manual:
			line += "  " + subtleStyle.Render("resolving DUID...")
		case r.DUID != "" && !r.Manual:
			line += "  " + subtleStyle.Render(truncate(r.DUID, 28))
		}
		if i == m.devCursor && !m.addingDUID {
			line = cursorStyle.Render("> ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}

	if m.addingDUID {
		b.WriteString("\n  Add DUID or TV IP: " + m.duidInput.View() + "\n")
		b.WriteString(subtleStyle.Render("  an IP connects via sdb and fetches the DUID automatically") + "\n")
		b.WriteString(subtleStyle.Render("  no sdb? TV menu: Support > Contact Samsung (Unique Device ID)") + "\n")
		b.WriteString(subtleStyle.Render("  enter confirm · esc cancel"))
		return b.String()
	}

	b.WriteString(fmt.Sprintf("\n%d device(s) selected\n", m.selectedDUIDCount()))
	b.WriteString(subtleStyle.Render("space select · a add manual · r rescan · enter continue · esc back"))
	return b.String()
}

func (m model) viewPrivilege() string {
	var b strings.Builder
	b.WriteString("Distributor privilege level:\n\n")
	b.WriteString(option(m.privCursor == 0, "Public", "standard level - enough to install and debug test apps"))
	b.WriteString(option(m.privCursor == 1, "Partner", "extra privileges - requires a Samsung partnership-enabled account"))
	b.WriteString("\n" + subtleStyle.Render("↑/↓ move · enter confirm · esc back"))
	return b.String()
}

func (m model) viewDetails() string {
	labels := [fieldCount]string{"Profile name", "E-mail", "Password"}
	var b strings.Builder
	b.WriteString("Certificate details:\n\n")
	for i := range m.inputs {
		marker := "  "
		if i == m.fieldFocus {
			marker = cursorStyle.Render("> ")
		}
		b.WriteString(fmt.Sprintf("%s%-13s %s\n", marker, labels[i]+":", m.inputs[i].View()))
	}
	b.WriteString("\n" + subtleStyle.Render("tab/enter next field · enter on last field continues · esc back"))
	return b.String()
}

func (m model) viewLogin() string {
	var b strings.Builder
	b.WriteString("Samsung account sign-in (required to issue the certificates)\n\n")

	if m.manualMode {
		if m.loginErr != "" {
			b.WriteString(errStyle.Render("  ! "+m.loginErr) + "\n\n")
		}
		b.WriteString("  The sign-in result is POSTed to localhost:4794, so on a headless\n")
		b.WriteString("  machine the token must be captured elsewhere: complete the sign-in\n")
		b.WriteString("  on a machine running this tool (or grab the `code` field of the\n")
		b.WriteString("  callback with browser dev tools) and paste it below as\n")
		b.WriteString("  access_token:user_id\n\n")
		b.WriteString("  " + m.tokenInput.View() + "\n\n")
		b.WriteString(subtleStyle.Render("enter confirm · esc back"))
		return b.String()
	}

	if m.login == nil {
		b.WriteString(subtleStyle.Render("  starting local callback server..."))
		return b.String()
	}

	if m.sshMode {
		b.WriteString("  SSH session detected - the sign-in must happen in a browser on YOUR machine,\n")
		b.WriteString("  and Samsung posts the result to localhost:4794, so forward that port first.\n\n")
		b.WriteString("  1. On your local machine, open the tunnel (keep it running):\n")
		b.WriteString(boxStyle.Render(samsung.TunnelHint()) + "\n\n")
		b.WriteString("  2. Then open this URL in your local browser ('c' copies it to your clipboard):\n")
	} else {
		b.WriteString("  A browser window was opened for the Samsung sign-in.\n")
		b.WriteString("  If it did not open, use this URL:\n\n")
	}
	b.WriteString(boxStyle.Render(m.login.URL) + "\n\n")
	if m.urlCopied {
		b.WriteString(okStyle.Render("  ✓ URL copied to the clipboard (OSC 52)") + "\n\n")
	}
	b.WriteString(subtleStyle.Render("waiting for the sign-in to complete... · c copy URL · m enter token manually · esc back"))
	return b.String()
}

func (m model) viewIssue() string {
	var b strings.Builder
	b.WriteString(m.spin.View() + " Issuing certificates...\n\n")
	for _, p := range m.progress {
		b.WriteString("  " + okStyle.Render("•") + " " + p + "\n")
	}
	return b.String()
}

func (m model) viewDone() string {
	var b strings.Builder
	if m.issueErr != "" {
		b.WriteString(errStyle.Render("✗ Generation failed") + "\n\n")
		b.WriteString("  " + m.issueErr + "\n\n")
		b.WriteString(subtleStyle.Render("q quit"))
		return b.String()
	}
	b.WriteString(okStyle.Render("✓ Certificate profile generated successfully") + "\n\n")
	if m.result != nil {
		b.WriteString("  Certificates folder:  " + m.result.OutputDir + "\n")
		b.WriteString("  Profile registered:   " + m.result.ProfilesXMLPath + "\n")
		b.WriteString("  Profile name:         " + m.result.ProfileName + "\n")
		b.WriteString(fmt.Sprintf("  Devices covered:      %d DUID(s)\n", m.result.DUIDCount))
		b.WriteString("  Valid until:          " + m.result.NotAfter.Format("2006-01-02") + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render("q quit"))
	return b.String()
}
