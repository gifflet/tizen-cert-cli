package samsung

import (
	"strings"
	"testing"
)

func TestTunnelHintShape(t *testing.T) {
	hint := TunnelHint()
	if !strings.HasPrefix(hint, "ssh -L 4794:localhost:4794 ") {
		t.Errorf("unexpected tunnel hint: %q", hint)
	}
	if !strings.Contains(hint, "@") {
		t.Errorf("tunnel hint missing user@host: %q", hint)
	}
}

func TestTunnelHintUsesSSHConnectionServerIP(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.30.0.99 55555 10.30.50.7 22")
	hint := TunnelHint()
	if !strings.HasSuffix(hint, "@10.30.50.7") {
		t.Errorf("expected the SSH_CONNECTION server IP in the hint, got %q", hint)
	}
}

func TestParseManualInput(t *testing.T) {
	if _, err := ParseManualInput("tokenonly"); err == nil {
		t.Error("expected error without user id")
	}
	session, err := ParseManualInput("tok123:user456")
	if err != nil {
		t.Fatal(err)
	}
	if session.AccessToken != "tok123" || session.UserID != "user456" {
		t.Errorf("unexpected session: %+v", session)
	}
}
