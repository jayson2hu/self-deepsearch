package security

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrHumanVerification = errors.New("human verification failed")

const (
	TurnstileActionSignupCode   = "signup_code"
	TurnstileActionPasswordCode = "password_code"

	turnstileSiteVerifyURL    = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	maxTurnstileTokenBytes    = 2 * 1024
	maxTurnstileResponseBytes = 16 * 1024
)

type HumanVerifier interface {
	Verify(context.Context, string, string, string) error
}

type Turnstile struct {
	secret           string
	expectedHostname string
	endpoint         string
	client           *http.Client
}

func NewTurnstile(secret, expectedHostname string) *Turnstile {
	return &Turnstile{
		secret:           strings.TrimSpace(secret),
		expectedHostname: strings.TrimSpace(expectedHostname),
		endpoint:         turnstileSiteVerifyURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (verifier *Turnstile) Verify(ctx context.Context, token, remoteIP, action string) error {
	token = strings.TrimSpace(token)
	if verifier == nil || verifier.client == nil || verifier.secret == "" || verifier.expectedHostname == "" ||
		len(token) == 0 || len(token) > maxTurnstileTokenBytes || !validTurnstileAction(action) {
		return ErrHumanVerification
	}
	form := url.Values{"secret": {verifier.secret}, "response": {token}}
	if ip := net.ParseIP(strings.TrimSpace(remoteIP)); ip != nil {
		form.Set("remoteip", ip.String())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, verifier.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrHumanVerification
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := verifier.client.Do(request)
	if err != nil {
		return ErrHumanVerification
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrHumanVerification
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return ErrHumanVerification
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTurnstileResponseBytes+1))
	if err != nil || len(body) > maxTurnstileResponseBytes {
		return ErrHumanVerification
	}
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if json.Unmarshal(body, &result) != nil || !result.Success ||
		!strings.EqualFold(strings.TrimSpace(result.Hostname), verifier.expectedHostname) || result.Action != action {
		return ErrHumanVerification
	}
	return nil
}

type DevelopmentVerifier struct{}

func (DevelopmentVerifier) Verify(_ context.Context, token, _ string, action string) error {
	if token != "test-pass" || !validTurnstileAction(action) {
		return ErrHumanVerification
	}
	return nil
}

func validTurnstileAction(action string) bool {
	return action == TurnstileActionSignupCode || action == TurnstileActionPasswordCode
}
