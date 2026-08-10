package gitforge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCode is the user-facing device authorization challenge.
type DeviceCode struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               int
	Interval                int
}

// ErrDevicePending means the user has not approved yet.
var ErrDevicePending = errors.New("authorization_pending")

// ErrDeviceSlowDown means the client should increase poll interval.
var ErrDeviceSlowDown = errors.New("slow_down")

// ErrDeviceExpired means the device code expired.
var ErrDeviceExpired = errors.New("expired_token")

// ErrDeviceDenied means the user denied the request.
var ErrDeviceDenied = errors.New("access_denied")

// SupportsDeviceFlow reports whether provider supports OAuth device code.
func SupportsDeviceFlow(provider string) bool {
	switch NormalizeProvider(provider) {
	case ProviderGitHub, ProviderGitLab:
		return true
	default:
		return false
	}
}

// RequestDeviceCode starts a device-code grant (GitHub / GitLab).
func RequestDeviceCode(ctx context.Context, provider string, app AppCredentials) (*DeviceCode, error) {
	provider = NormalizeProvider(provider)
	if !SupportsDeviceFlow(provider) {
		return nil, fmt.Errorf("device flow not supported for %s", provider)
	}
	if strings.TrimSpace(app.ClientID) == "" {
		return nil, fmt.Errorf("oauth client id required")
	}
	base := baseEndpoint(provider, app.Endpoint)
	form := url.Values{}
	form.Set("client_id", app.ClientID)
	form.Set("scope", DefaultScopes(provider))

	var endpoint string
	switch provider {
	case ProviderGitHub:
		endpoint = base + "/login/device/code"
	case ProviderGitLab:
		endpoint = base + "/oauth/authorize_device"
	default:
		return nil, fmt.Errorf("unsupported provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("device code HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}

	dc, err := parseDeviceCode(body)
	if err != nil {
		return nil, err
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	if dc.VerificationURI == "" {
		switch provider {
		case ProviderGitHub:
			dc.VerificationURI = "https://github.com/login/device"
		case ProviderGitLab:
			dc.VerificationURI = base + "/oauth/device"
		}
	}
	return dc, nil
}

// PollDeviceToken exchanges a device code for tokens (or returns ErrDevicePending).
func PollDeviceToken(ctx context.Context, provider string, app AppCredentials, deviceCode string) (*TokenSet, error) {
	provider = NormalizeProvider(provider)
	if !SupportsDeviceFlow(provider) {
		return nil, fmt.Errorf("device flow not supported for %s", provider)
	}
	base := baseEndpoint(provider, app.Endpoint)
	form := url.Values{}
	form.Set("client_id", app.ClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	if app.ClientSecret != "" {
		form.Set("client_secret", app.ClientSecret)
	}

	var endpoint string
	switch provider {
	case ProviderGitHub:
		endpoint = base + "/login/oauth/access_token"
	case ProviderGitLab:
		endpoint = base + "/oauth/token"
	default:
		return nil, fmt.Errorf("unsupported provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	// GitHub returns 200 with error field for pending; GitLab may use 400.
	raw, err := parseTokenMap(body)
	if err != nil {
		if res.StatusCode >= 400 {
			return nil, fmt.Errorf("device poll HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
		}
		return nil, err
	}
	if errCode := strVal(raw["error"]); errCode != "" {
		switch errCode {
		case "authorization_pending":
			return nil, ErrDevicePending
		case "slow_down":
			return nil, ErrDeviceSlowDown
		case "expired_token":
			return nil, ErrDeviceExpired
		case "access_denied":
			return nil, ErrDeviceDenied
		default:
			desc := strVal(raw["error_description"])
			if desc == "" {
				desc = errCode
			}
			return nil, fmt.Errorf("%s", desc)
		}
	}
	ts := &TokenSet{
		AccessToken:  strVal(raw["access_token"]),
		RefreshToken: strVal(raw["refresh_token"]),
		TokenType:    strVal(raw["token_type"]),
		Scope:        strVal(raw["scope"]),
	}
	switch v := raw["expires_in"].(type) {
	case float64:
		ts.ExpiresIn = int(v)
	}
	if ts.AccessToken == "" {
		if res.StatusCode >= 400 {
			return nil, fmt.Errorf("device poll HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
		}
		return nil, ErrDevicePending
	}
	return ts, nil
}

func parseDeviceCode(body []byte) (*DeviceCode, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		vals, err2 := url.ParseQuery(string(body))
		if err2 != nil {
			return nil, fmt.Errorf("parsing device code: %w", err)
		}
		dc := &DeviceCode{
			DeviceCode:              vals.Get("device_code"),
			UserCode:                vals.Get("user_code"),
			VerificationURI:         vals.Get("verification_uri"),
			VerificationURIComplete: vals.Get("verification_uri_complete"),
		}
		fmt.Sscanf(vals.Get("expires_in"), "%d", &dc.ExpiresIn)
		fmt.Sscanf(vals.Get("interval"), "%d", &dc.Interval)
		if dc.DeviceCode == "" || dc.UserCode == "" {
			return nil, fmt.Errorf("incomplete device code response")
		}
		return dc, nil
	}
	dc := &DeviceCode{
		DeviceCode:              strVal(raw["device_code"]),
		UserCode:                strVal(raw["user_code"]),
		VerificationURI:         strVal(raw["verification_uri"]),
		VerificationURIComplete: strVal(raw["verification_uri_complete"]),
	}
	if dc.VerificationURI == "" {
		dc.VerificationURI = strVal(raw["verification_url"]) // GitLab
	}
	switch v := raw["expires_in"].(type) {
	case float64:
		dc.ExpiresIn = int(v)
	}
	switch v := raw["interval"].(type) {
	case float64:
		dc.Interval = int(v)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, fmt.Errorf("incomplete device code response")
	}
	return dc, nil
}

func parseTokenMap(body []byte) (map[string]interface{}, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err == nil {
		return raw, nil
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	raw = map[string]interface{}{}
	for k, v := range vals {
		if len(v) > 0 {
			raw[k] = v[0]
		}
	}
	return raw, nil
}

// SleepInterval returns how long to wait before the next poll.
func SleepInterval(dc *DeviceCode, slowDown bool) time.Duration {
	sec := 5
	if dc != nil && dc.Interval > 0 {
		sec = dc.Interval
	}
	if slowDown {
		sec += 5
	}
	return time.Duration(sec) * time.Second
}
