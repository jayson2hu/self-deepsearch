package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTurnstileVerifyBindsTokenIPHostnameAndAction(t *testing.T) {
	requestErrors := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestErrors <- fmt.Sprintf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/x-www-form-urlencoded" {
			requestErrors <- fmt.Sprintf("unexpected content type %q", contentType)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			requestErrors <- fmt.Sprintf("parse form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("secret") != "secret-value" || r.Form.Get("response") != "token-value" ||
			r.Form.Get("remoteip") != "192.0.2.8" {
			requestErrors <- fmt.Sprintf("unexpected form %#v", r.Form)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"success":true,"hostname":"DISPLAY.EXAMPLE.TEST","action":"signup_code"}`))
	}))
	defer server.Close()

	verifier := NewTurnstile(" secret-value ", "display.example.test")
	verifier.endpoint = server.URL
	if err := verifier.Verify(context.Background(), " token-value ", "192.0.2.8", TurnstileActionSignupCode); err != nil {
		t.Fatalf("valid verification failed: %v", err)
	}
	select {
	case message := <-requestErrors:
		t.Fatal(message)
	default:
	}
}

func TestTurnstileVerifyOmitsInvalidRemoteIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if _, present := r.Form["remoteip"]; present {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"hostname":"display.example.test","action":"password_code"}`))
	}))
	defer server.Close()

	verifier := NewTurnstile("secret-value", "display.example.test")
	verifier.endpoint = server.URL
	if err := verifier.Verify(context.Background(), "token-value", "not-an-ip", TurnstileActionPasswordCode); err != nil {
		t.Fatalf("invalid optional remote IP should be omitted: %v", err)
	}
}

func TestTurnstileVerifyRejectsMismatchedBinding(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "hostname", body: `{"success":true,"hostname":"other.example.test","action":"signup_code"}`},
		{name: "action", body: `{"success":true,"hostname":"display.example.test","action":"password_code"}`},
		{name: "unsuccessful", body: `{"success":false,"hostname":"display.example.test","action":"signup_code"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			verifier := NewTurnstile("secret-value", "display.example.test")
			verifier.endpoint = server.URL
			if err := verifier.Verify(context.Background(), "token-value", "", TurnstileActionSignupCode); !errors.Is(err, ErrHumanVerification) {
				t.Fatalf("expected verification failure, got %v", err)
			}
		})
	}
}

func TestTurnstileVerifyRejectsInvalidInputBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	verifier := NewTurnstile("secret-value", "display.example.test")
	verifier.endpoint = server.URL

	tests := []struct {
		name   string
		token  string
		action string
	}{
		{name: "empty token", token: " ", action: TurnstileActionSignupCode},
		{name: "oversized token", token: strings.Repeat("x", maxTurnstileTokenBytes+1), action: TurnstileActionSignupCode},
		{name: "unknown action", token: "token-value", action: "profile_update"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := verifier.Verify(context.Background(), test.token, "", test.action); !errors.Is(err, ErrHumanVerification) {
				t.Fatalf("expected verification failure, got %v", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input made %d network requests", calls.Load())
	}
}

func TestTurnstileVerifyRejectsUnsafeOrInvalidResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "non 200", status: http.StatusBadGateway, contentType: "application/json", body: `{}`},
		{name: "non json", status: http.StatusOK, contentType: "text/html", body: `{}`},
		{name: "missing content type", status: http.StatusOK, body: `{}`},
		{name: "malformed json", status: http.StatusOK, contentType: "application/json", body: `{"success":`},
		{name: "oversized response", status: http.StatusOK, contentType: "application/json", body: strings.Repeat(" ", maxTurnstileResponseBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.contentType != "" {
					w.Header().Set("Content-Type", test.contentType)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			verifier := NewTurnstile("secret-value", "display.example.test")
			verifier.endpoint = server.URL
			if err := verifier.Verify(context.Background(), "token-value", "", TurnstileActionSignupCode); !errors.Is(err, ErrHumanVerification) {
				t.Fatalf("expected verification failure, got %v", err)
			}
		})
	}
}

func TestTurnstileVerifyDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"hostname":"display.example.test","action":"signup_code"}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	verifier := NewTurnstile("secret-value", "display.example.test")
	verifier.endpoint = redirect.URL
	if err := verifier.Verify(context.Background(), "token-value", "", TurnstileActionSignupCode); !errors.Is(err, ErrHumanVerification) {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target was called %d times", targetCalls.Load())
	}
}

func TestDevelopmentVerifierOnlyAcceptsKnownTestTokenAndAction(t *testing.T) {
	verifier := DevelopmentVerifier{}
	if err := verifier.Verify(context.Background(), "test-pass", "", TurnstileActionSignupCode); err != nil {
		t.Fatalf("development token rejected: %v", err)
	}
	for _, test := range []struct {
		token  string
		action string
	}{
		{token: "other", action: TurnstileActionSignupCode},
		{token: "test-pass", action: "other_action"},
	} {
		if err := verifier.Verify(context.Background(), test.token, "", test.action); !errors.Is(err, ErrHumanVerification) {
			t.Fatalf("unexpected development verification result for %#v: %v", test, err)
		}
	}
}
