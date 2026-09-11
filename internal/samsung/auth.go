package samsung

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Session holds the Samsung account credentials required by the
// certificate issuance APIs. Email is the account e-mail reported by the
// sign-in callback and goes into the distributor CSR subject.
type Session struct {
	AccessToken string
	UserID      string
	Email       string
}

// LoginServer is the local HTTP listener that captures the Samsung account
// sign-in result, mirroring what Tizen Studio's certificate extension does.
type LoginServer struct {
	URL      string // sign-in page to open in a browser
	server   *http.Server
	resultCh chan loginResult
}

type loginResult struct {
	session Session
	err     error
}

// StartLogin binds the local callback listener and returns the sign-in URL.
func StartLogin() (*LoginServer, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return nil, fmt.Errorf("binding local callback port %d: %w", callbackPort, err)
	}

	ls := &LoginServer{
		URL:      buildLoginURL(),
		resultCh: make(chan loginResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, ls.handleCallback)
	ls.server = &http.Server{Handler: mux}
	go func() { _ = ls.server.Serve(listener) }()
	return ls, nil
}

func (ls *LoginServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	session, err := parseCallback(r)
	if err != nil {
		http.Error(w, "sign-in failed: "+err.Error(), http.StatusBadRequest)
		ls.resultCh <- loginResult{err: err}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><body><h2>Sign-in complete.</h2>You can close this tab and return to the terminal.</body></html>")
	ls.resultCh <- loginResult{session: session}
}

// Wait blocks until the browser flow completes or times out.
func (ls *LoginServer) Wait() (Session, error) {
	select {
	case result := <-ls.resultCh:
		ls.Close()
		return result.session, result.err
	case <-time.After(10 * time.Minute):
		ls.Close()
		return Session{}, fmt.Errorf("timed out waiting for the Samsung sign-in")
	}
}

// Close shuts the local listener down.
func (ls *LoginServer) Close() {
	if ls.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = ls.server.Shutdown(ctx)
	}
}

// ParseManualInput accepts the token pasted by the user in headless
// environments, in the form "access_token" or "access_token:user_id".
func ParseManualInput(value string) (Session, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Session{}, fmt.Errorf("empty token")
	}
	parts := strings.SplitN(value, ":", 2)
	session := Session{AccessToken: parts[0]}
	if len(parts) == 2 {
		session.UserID = parts[1]
	}
	if session.UserID == "" {
		return Session{}, fmt.Errorf("the user id is required - paste the value as access_token:user_id")
	}
	return session, nil
}

// OpenBrowser launches the platform's default browser at the given URL.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
