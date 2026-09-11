package samsung

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient uses the same 40s timeouts as Samsung's client.
var httpClient = &http.Client{Timeout: 40 * time.Second}

// IssueAuthor requests the author certificate. The response body is the
// certificate in PEM form.
func IssueAuthor(session Session, csrPEM []byte) ([]byte, error) {
	fields := []field{
		{"access_token", session.AccessToken},
		{"user_id", session.UserID},
		{"platform", "VD"},
	}
	return post(authorURL, fields, "author.csr", csrPEM)
}

// IssueDistributor requests the distributor certificate for a TV profile.
// Samsung's client performs two calls: v1 returns the device-profile.xml
// intermediate and v3 returns the actual certificate; both are mirrored.
func IssueDistributor(session Session, csrPEM []byte, privilege string) (certPEM, deviceProfile []byte, err error) {
	if privilege != "Public" && privilege != "Partner" {
		return nil, nil, fmt.Errorf("privilege_level must be Public or Partner, got %q", privilege)
	}
	fields := []field{
		{"access_token", session.AccessToken},
		{"user_id", session.UserID},
		{"privilege_level", privilege},
		{"developer_type", "Individual"},
		{"platform", "VD"},
	}

	deviceProfile, err = post(distributorURLOld, fields, "distributor.csr", csrPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("v1 distributors call (device-profile.xml): %w", err)
	}
	certPEM, err = post(distributorURL, fields, "distributor.csr", csrPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("v3 distributors call (distributor.crt): %w", err)
	}
	return certPEM, deviceProfile, nil
}

type field struct{ name, value string }

// post replicates the exact multipart body Samsung's client builds by hand:
// literal "*****" boundary, text fields with a text/plain part header, and a
// csr file part with filename + RFC 5987 filename* but no Content-Type.
func post(url string, fields []field, csrName string, csr []byte) ([]byte, error) {
	var body bytes.Buffer
	for _, f := range fields {
		body.WriteString("--*****\r\n")
		body.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		body.WriteString("Content-Disposition: form-data; name=" + f.name + "\r\n\r\n")
		body.WriteString(f.value + "\r\n")
	}
	body.WriteString("--*****\r\n")
	body.WriteString("Content-Disposition: form-data; name=csr; filename=" + csrName +
		"; filename*=utf-8''" + csrName + "\r\n\r\n")
	body.Write(csr)
	body.WriteString("\r\n--*****--\r\n")

	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `multipart/form-data; boundary="*****"`)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return payload, nil
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("rate limited by Samsung (HTTP 403) - wait a while and try again")
	default:
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, apiError(payload))
	}
}

// apiError extracts Samsung's structured error when present:
// {"error":{"name":..,"status":..,"code":..,"description":..}}
func apiError(body []byte) string {
	var parsed struct {
		Error struct {
			Name        string `json:"name"`
			Status      int    `json:"status"`
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Description != "" {
		if parsed.Error.Status == 401 && parsed.Error.Code == 303 {
			return "the Samsung account has no e-mail on file - fill it in at https://account.samsung.com/membership/getProfileInfo.do"
		}
		return fmt.Sprintf("%s (code %d)", parsed.Error.Description, parsed.Error.Code)
	}
	return strings.TrimSpace(string(body))
}
