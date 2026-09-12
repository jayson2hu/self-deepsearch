package mediauploadadmission

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"self-deepsearch/services/platform-worker/internal/health"
	"self-deepsearch/services/platform-worker/internal/mediausage"
)

type guardFunc func(context.Context) error

func (f guardFunc) Allow(ctx context.Context) error { return f(ctx) }

type stateFunc func(context.Context) (mediausage.State, error)

func (f stateFunc) ReadMetricsState(ctx context.Context) (mediausage.State, error) { return f(ctx) }

const secret = "synthetic-admission-secret-at-least-32-bytes"

func request(now time.Time) *http.Request {
	r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{}`))
	timestamp, nonce := strconv.FormatInt(now.Unix(), 10), strings.Repeat("a", 64)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SD-Admission-Timestamp", timestamp)
	r.Header.Set("X-SD-Admission-Nonce", nonce)
	r.Header.Set("X-SD-Admission-Signature", sign([]byte(secret), requestPrefix(timestamp, nonce), []byte(`{}`)))
	return r
}

func TestRequestAuthenticationBeforeAnyUsageRead(t *testing.T) {
	now := time.Now().UTC()
	var reads atomic.Int64
	h, _ := NewHandler(secret, guardFunc(func(context.Context) error { reads.Add(1); return nil }), func() time.Time { return now })
	cases := []struct {
		name   string
		edit   func(*http.Request)
		status int
	}{
		{"get", func(r *http.Request) { r.Method = "GET" }, 404},
		{"path", func(r *http.Request) { r.URL.Path = "/metrics" }, 404},
		{"query", func(r *http.Request) { r.URL.RawQuery = "debug=1" }, 404},
		{"empty-query", func(r *http.Request) { r.URL.ForceQuery = true }, 404},
		{"encoded-path", func(r *http.Request) { r.URL.RawPath = "/v1/%75pload-admission" }, 404},
		{"missing-signature", func(r *http.Request) { r.Header.Del("X-SD-Admission-Signature") }, 400},
		{"duplicate-nonce", func(r *http.Request) { r.Header.Add("X-SD-Admission-Nonce", strings.Repeat("a", 64)) }, 400},
		{"type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
		{"length", func(r *http.Request) { r.ContentLength = 3 }, 400},
		{"chunked", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, 400},
		{"compressed", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 400},
		{"bad-mac", func(r *http.Request) { r.Header.Set("X-SD-Admission-Signature", "u1="+strings.Repeat("a", 64)) }, 401},
		{"nonce", func(r *http.Request) { r.Header.Set("X-SD-Admission-Nonce", "private-sample") }, 401},
		{"old", func(r *http.Request) {
			r.Header.Set("X-SD-Admission-Timestamp", strconv.FormatInt(now.Add(-31*time.Second).Unix(), 10))
		}, 401},
		{"future", func(r *http.Request) {
			r.Header.Set("X-SD-Admission-Timestamp", strconv.FormatInt(now.Add(32*time.Second).Unix(), 10))
		}, 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := request(now)
			tc.edit(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-SD-Admission-Response") != "" {
				t.Fatalf("invalid request accepted: %d", w.Code)
			}
		})
	}
	if reads.Load() != 0 {
		t.Fatal("untrusted request reached the database guard")
	}
}

func TestResponseBoundToExactRequestAndNoPrivateErrors(t *testing.T) {
	now := time.Now().UTC()
	for _, allow := range []bool{true, false} {
		h, _ := NewHandler(secret, guardFunc(func(context.Context) error {
			if !allow {
				return errors.New("private database password sample")
			}
			return nil
		}), func() time.Time { return now })
		r := request(now)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private") {
			t.Fatal("incorrect disclosure or status")
		}
		var body map[string]any
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 3 || (body["decision"] == "allow") != allow {
			t.Fatal("bad decision")
		}
		expected := sign([]byte(secret), responsePrefix(r.Header.Get("X-SD-Admission-Timestamp"), r.Header.Get("X-SD-Admission-Nonce"), []byte(`{}`), 200), w.Body.Bytes())
		if w.Header().Get("X-SD-Admission-Response") != expected {
			t.Fatal("unsigned response")
		}
		wrong := sign([]byte(secret), responsePrefix(r.Header.Get("X-SD-Admission-Timestamp"), strings.Repeat("b", 64), []byte(`{}`), 200), w.Body.Bytes())
		if wrong == expected {
			t.Fatal("response not bound to nonce")
		}
	}
}

func TestConcurrencyDeadlineAndBackwardClockFailClosed(t *testing.T) {
	now := time.Now().UTC()
	h, _ := NewHandler(secret, guardFunc(func(context.Context) error { return nil }), func() time.Time { return now })
	h.active <- struct{}{}
	h.active <- struct{}{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request(now))
	if w.Code != 429 || !strings.Contains(w.Body.String(), `"decision":"deny"`) {
		t.Fatal("concurrency guard failed")
	}
	h, _ = NewHandler(secret, guardFunc(func(ctx context.Context) error { <-ctx.Done(); return nil }), time.Now)
	w = httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, request(start))
	if time.Since(start) > 3*time.Second || !strings.Contains(w.Body.String(), `"decision":"deny"`) {
		t.Fatal("cancelled read granted permit")
	}
	start = now
	h, _ = NewHandler(secret, guardFunc(func(context.Context) error { now = now.Add(-time.Second); return nil }), func() time.Time { return now })
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request(start))
	if !strings.Contains(w.Body.String(), `"decision":"deny"`) {
		t.Fatal("backward clock allowed")
	}
}

func TestActualUsageGuardAndPrivateHealthMount(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	schedule := mediausage.Schedule{AccountID: strings.Repeat("a", 32), PeriodStart: now.Add(-time.Hour).Truncate(time.Second), PeriodEnd: now.Add(time.Hour).Truncate(time.Second), Every: 15 * time.Minute, Policy: mediausage.DefaultPolicy()}
	s := mediausage.State{LastSuccessAt: now, LastUntil: now.Add(-time.Second), UpdatedAt: now,
		LastAssessment: mediausage.Assessment{Status: "low_estimate", Recommendation: "observe_only"}}
	var readError error
	guard, err := mediausage.NewTaskGuard(stateFunc(func(context.Context) (mediausage.State, error) { return s, readError }), schedule, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	h, _ := NewHandler(secret, guard, func() time.Time { return now })
	mux := health.Handler{Region: "japan", Capabilities: []string{"media_usage"}, UploadAdmissionHTTP: h, UploadAdmissionMetrics: h, MetricsToken: "synthetic-metrics-token"}.Routes()
	for _, tc := range []struct {
		name  string
		edit  func()
		allow bool
	}{
		{"low", func() {}, true},
		{"85-percent", func() { s.HighWater.ClassA = 850000 }, false},
		{"95-percent", func() { s.HighWater.ClassA = 950000 }, false},
		{"review-latch", func() { s.HighWater.ClassA = 0; s.ReviewRequired = true }, false},
		{"stop-latch", func() { s.ReviewRequired = false; s.StopRecommended = true }, false},
		{"stale", func() { s.StopRecommended = false; s.LastSuccessAt = now.Add(-31 * time.Minute) }, false},
		{"missing", func() { s.LastSuccessAt = time.Time{} }, false},
		{"database-error", func() { readError = errors.New("private database") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request(now))
			if (strings.Contains(w.Body.String(), `"decision":"allow"`)) != tc.allow {
				t.Fatalf("wrong admission: %s", w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	health.Handler{}.Routes().ServeHTTP(w, request(now))
	if w.Code != 404 {
		t.Fatal("disabled route present")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 401 {
		t.Fatal("metrics exposed")
	}
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer synthetic-metrics-token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "self_deepsearch_media_upload_admission_enabled 1") || strings.Contains(w.Body.String(), secret) {
		t.Fatal("metrics invalid")
	}
}
