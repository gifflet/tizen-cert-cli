package samsung

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Constants extracted from Samsung's certificate extension 2.0.75
// (org.tizen.common.cert, built 2025-10-21). The v3 endpoints replaced
// dev.tizen.samsung.com/apis/v2 in October 2025.
const (
	authorURL         = "https://svdca.samsungqbe.com/apis/v3/authors"
	distributorURL    = "https://svdca.samsungqbe.com/apis/v3/distributors"
	distributorURLOld = "https://svdca.samsungqbe.com/apis/v1/distributors"

	serviceID    = "v285zxnl3h"
	callbackPort = 4794
	callbackPath = "/signin/callback"

	loginURL = "https://account.samsung.com/mobile/account/check.do" +
		"?serviceID=" + serviceID +
		"&actionID=StartOAuth2" +
		"&accessToken=Y" +
		"&redirect_uri=http://localhost:4794/signin/callback"
)

func buildLoginURL() string { return loginURL }

// callbackPayload is the JSON carried inside the "code" form field Samsung
// POSTs to the local callback. "code" is not an OAuth authorization code.
type callbackPayload struct {
	AccessToken string `json:"access_token"`
	UserID      string `json:"userId"`
	Email       string `json:"inputEmailID"`
}

func parseCallback(r *http.Request) (Session, error) {
	// Samsung's own client reads the raw body and URL-decodes it without
	// looking at Content-Type, so the body is parsed directly instead of
	// relying on ParseForm's header checks.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return Session{}, fmt.Errorf("reading callback body: %w", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || values.Get("code") == "" {
		values = r.URL.Query()
	}
	code := values.Get("code")
	if code == "" {
		return Session{}, fmt.Errorf("callback carried no `code` field")
	}
	var payload callbackPayload
	if err := json.Unmarshal([]byte(code), &payload); err != nil {
		return Session{}, fmt.Errorf("decoding callback payload: %w", err)
	}
	if payload.AccessToken == "" || payload.UserID == "" {
		return Session{}, fmt.Errorf("callback payload missing access_token or userId")
	}
	return Session{AccessToken: payload.AccessToken, UserID: payload.UserID, Email: payload.Email}, nil
}
