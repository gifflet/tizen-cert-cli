// Package sdb wraps the Tizen sdb command line tool for device discovery
// and DUID retrieval. sdb is only required when detecting connected TVs;
// manually entered DUIDs bypass this package entirely.
package sdb

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Device is a TV visible to sdb.
type Device struct {
	Serial string // e.g. "10.30.50.116:26101"
	State  string // "device", "offline", "unauthorized"
	Name   string // e.g. "QN43QN90DAGXZD"
	DUID   string // filled by FetchDUID
}

// duidPattern validates TV DUIDs: a bare alphanumeric token, optionally
// carrying the wearable-style "N.N#" prefix (passed through verbatim -
// Samsung's client never rewrites TV DUIDs).
var duidPattern = regexp.MustCompile(`^(?:\d\.\d#)?[A-Za-z0-9]{10,64}$`)

// Path is the sdb executable used by this package. It defaults to "sdb"
// resolved from PATH and can be overridden by the caller.
var Path = "sdb"

// Available reports whether the sdb executable can be found.
func Available() bool {
	_, err := exec.LookPath(Path)
	return err == nil
}

// ListDevices returns the devices currently known to sdb.
func ListDevices() ([]Device, error) {
	out, err := exec.Command(Path, "devices").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running sdb devices: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	var devices []Device
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		dev := Device{Serial: fields[0], State: fields[1]}
		if len(fields) >= 3 {
			dev.Name = fields[2]
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

// FetchDUID retrieves the device unique ID used by Samsung's distributor
// certificates. `0 getduid` is flaky on older TVs (empty or truncated first
// answer), so a few attempts are made and the output is validated instead
// of trusted - failures answer in prose, not a token.
func FetchDUID(serial string) (string, error) {
	var lastOutput string
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		out, err := exec.Command(Path, "-s", serial, "shell", "0", "getduid").CombinedOutput()
		lastOutput = strings.TrimSpace(string(out))
		if err != nil {
			continue
		}
		for _, line := range strings.Fields(lastOutput) {
			if duidPattern.MatchString(line) {
				return line, nil
			}
		}
	}
	return "", fmt.Errorf("no valid DUID from %s after 3 attempts (last output: %q)", serial, lastOutput)
}

// Connect asks sdb to connect to a TV by address (ip[:port]). Useful when
// the TV is reachable but not yet in the device list.
func Connect(address string) error {
	out, err := exec.Command(Path, "connect", address).CombinedOutput()
	raw := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("sdb connect %s: %w (output: %s)", address, err, raw)
	}
	// sdb exits 0 even on failure; the outcome is only in the text.
	if strings.Contains(raw, "failed") || strings.Contains(raw, "error") {
		return fmt.Errorf("sdb connect %s: %s", address, raw)
	}
	return nil
}
