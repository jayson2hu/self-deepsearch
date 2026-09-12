package mediauploadcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-only-upload-control-secret-1234567890"
const testID = "10000000-0000-4000-8000-000000000001"

func testCommand() Command {
	return Command{ID: testID, ExpectedGeneration: 0, Mode: "paused", Reason: "test-only reviewed"}
}

func ackFor(command Command) Receipt {
	epoch := command.ExpectedEpoch
	if epoch == "" {
		epoch = command.ID
	}
	return Receipt{Status: "applied", Epoch: epoch, Generation: command.ExpectedGeneration + 1, Mode: command.Mode, CommandID: command.ID, AppliedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z")}
}

func signedResponse(w http.ResponseWriter, r *http.Request, status int, ack []byte, mutate func(*http.Header)) {
	body, _ := io.ReadAll(r.Body)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-SD-Control-Response", mac([]byte(testSecret), responsePrefix(r.URL.Path, r.Header.Get("X-SD-Control-Timestamp"), r.Header.Get("X-SD-Control-Nonce"), body, status), ack))
	if mutate != nil {
		mutate(&h)
	}
	w.WriteHeader(status)
	_, _ = w.Write(ack)
}

func TestSignedControlRoundTripAndReceiptValidation(t *testing.T) {
	good := ackFor(testCommand())
	goodBody, _ := json.Marshal(good)
	cases := []struct {
		name   string
		body   []byte
		status int
		header func(*http.Header)
		want   error
	}{
		{"applied", goodBody, 200, nil, nil},
		{"unsigned", goodBody, 200, func(h *http.Header) { h.Del("X-SD-Control-Response") }, ErrUncertain},
		{"forged", goodBody, 200, func(h *http.Header) { h.Set("X-SD-Control-Response", "u1="+strings.Repeat("a", 64)) }, ErrUncertain},
		{"html", goodBody, 200, func(h *http.Header) { h.Set("Content-Type", "text/html") }, ErrUncertain},
		{"duplicate_header", goodBody, 200, func(h *http.Header) { h.Add("X-SD-Control-Response", h.Get("X-SD-Control-Response")) }, ErrUncertain},
		{"encoded", goodBody, 200, func(h *http.Header) { h.Set("Content-Encoding", "gzip") }, ErrUncertain},
		{"oversized", bytes.Repeat([]byte("x"), 4097), 200, nil, ErrUncertain},
		{"utf8", []byte{0xff}, 200, nil, ErrUncertain},
		{"empty", nil, 200, nil, ErrUncertain},
		{"array", []byte(`[]`), 200, nil, ErrUncertain},
		{"missing", []byte(`{}`), 200, nil, ErrUncertain},
		{"duplicate_json", append([]byte(`{"status":"observed",`), goodBody[1:]...), 200, nil, ErrUncertain},
		{"trailing", append(append([]byte{}, goodBody...), []byte(`{}`)...), 200, nil, ErrUncertain},
		{"conflict", []byte(`{"error":"control_conflict"}`), 409, nil, ErrConflict},
		{"busy", []byte(`{"error":"control_busy"}`), 503, nil, ErrBusy},
		{"unauthenticated_conflict", []byte(`{"error":"control_conflict"}`), 409, func(h *http.Header) { h.Del("X-SD-Control-Response") }, ErrUncertain},
		{"wrong_http_status", goodBody, 202, nil, ErrUncertain},
	}
	for name, mutate := range map[string]func(*Receipt){
		"wrong_command":    func(r *Receipt) { r.CommandID = "20000000-0000-4000-8000-000000000002" },
		"wrong_epoch":      func(r *Receipt) { r.Epoch = "20000000-0000-4000-8000-000000000002" },
		"wrong_generation": func(r *Receipt) { r.Generation++ },
		"wrong_mode":       func(r *Receipt) { r.Mode = "enabled" },
		"wrong_status":     func(r *Receipt) { r.Status = "queued" },
		"future_time":      func(r *Receipt) { r.AppliedAt = time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z") },
	} {
		r := good
		mutate(&r)
		body, _ := json.Marshal(r)
		cases = append(cases, struct {
			name   string
			body   []byte
			status int
			header func(*http.Header)
			want   error
		}{name, body, 200, nil, ErrUncertain})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				expected := mac([]byte(testSecret), requestPrefix(r.URL.Path, r.Header.Get("X-SD-Control-Timestamp"), r.Header.Get("X-SD-Control-Nonce")), body)
				if r.Header.Get("X-SD-Control-Signature") != expected || r.Method != http.MethodPost || len(r.Header.Get("X-SD-Control-Nonce")) != 64 {
					t.Error("request was not correctly authenticated")
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				signedResponse(w, r, tc.status, tc.body, tc.header)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, testSecret, true, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			r, err := client.Apply(context.Background(), testCommand())
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err != nil && r != (Receipt{}) {
				t.Fatal("untrusted receipt escaped")
			}
		})
	}
}

func TestOldResponseCannotBeReplayedAgainstFreshNonce(t *testing.T) {
	var savedSignature string
	body, _ := json.Marshal(ackFor(testCommand()))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signedResponse(w, r, 200, body, func(h *http.Header) {
			if savedSignature == "" {
				savedSignature = h.Get("X-SD-Control-Response")
			} else {
				h.Set("X-SD-Control-Response", savedSignature)
			}
		})
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, testSecret, true, server.Client())
	if _, err := c.Apply(context.Background(), testCommand()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(context.Background(), testCommand()); !errors.Is(err, ErrUncertain) {
		t.Fatal("old signed receipt accepted")
	}
}

func TestClientRejectsRedirectsInvalidConfigAndCanceledRequests(t *testing.T) {
	for _, origin := range []string{"", "http://host", "https://host/path", "https://u:p@host", "https://host?x=1", "https://host#x", "ftp://host"} {
		if _, err := NewClient(origin, testSecret, false, nil); err == nil {
			t.Errorf("accepted %q", origin)
		}
	}
	if _, err := NewClient("https://host", "short", false, nil); err == nil {
		t.Fatal("short secret")
	}
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	c, _ := NewClient(redirect.URL, testSecret, true, nil)
	if _, err := c.Status(context.Background()); err == nil || called {
		t.Fatal("redirect followed or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Status(ctx); !errors.Is(err, ErrUncertain) {
		t.Fatal("cancellation not honored")
	}
}

func TestStrictReceiptAndCommandFields(t *testing.T) {
	for _, body := range []string{`null`, `{"status":null}`, `{"status":"observed","epoch":"","generation":0,"mode":"paused","command_id":"","applied_at":"","extra":1}`} {
		if uniqueReceiptFields([]byte(body)) {
			t.Fatal("ambiguous receipt accepted")
		}
	}
	for name, change := range map[string]func(*Command){
		"id": func(c *Command) { c.ID = "bad" }, "reason": func(c *Command) { c.Reason = "\nprivate" },
		"epoch": func(c *Command) { c.ExpectedEpoch = testID }, "generation": func(c *Command) { c.ExpectedGeneration = -1 },
		"initial_resume":     func(c *Command) { c.Mode, c.ResumeConfirmed = "enabled", true },
		"pause_confirmation": func(c *Command) { c.ResumeConfirmed = true },
	} {
		t.Run(name, func(t *testing.T) {
			c := testCommand()
			change(&c)
			if c.valid() {
				t.Fatal("invalid command")
			}
		})
	}
}
