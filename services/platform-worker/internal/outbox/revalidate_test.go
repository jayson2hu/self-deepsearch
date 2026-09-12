package outbox

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRevalidationProcessorSignsExactBody(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		timestamp, eventID := request.Header.Get("X-SD-Timestamp"), request.Header.Get("X-SD-Event-ID")
		digest := hmac.New(sha256.New, []byte(secret))
		_, _ = digest.Write([]byte(timestamp + "\n" + eventID + "\n"))
		_, _ = digest.Write(body)
		expected := "v1=" + hex.EncodeToString(digest.Sum(nil))
		if request.Method != http.MethodPost || eventID != "event-1" || timestamp != "1787054400" || request.Header.Get("X-SD-Signature") != expected {
			t.Fatalf("invalid signed request: method=%s event=%s timestamp=%s signature=%s", request.Method, eventID, timestamp, request.Header.Get("X-SD-Signature"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"revalidated": true, "event_id": eventID})
	}))
	defer server.Close()
	processor, err := NewRevalidationProcessor(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	processor.now = func() time.Time { return now }
	event := Event{ID: "event-1", Type: "publication_changed", AggregateType: "work", AggregateID: "work-1", Payload: []byte(`{"slug":"test-1"}`)}
	if err := processor.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func TestRevalidationProcessorRejectsWeakConfiguration(t *testing.T) {
	if _, err := NewRevalidationProcessor("not-a-url", strings.Repeat("s", 32), nil); err == nil {
		t.Fatal("relative endpoint must be rejected")
	}
	if _, err := NewRevalidationProcessor("https://display.example.invalid/revalidate", "short", nil); err == nil {
		t.Fatal("weak secret must be rejected")
	}
	if _, err := NewRevalidationProcessor("https://user:password@display.example.invalid/revalidate?redirect=true", strings.Repeat("s", 32), nil); err == nil {
		t.Fatal("endpoint credentials and query strings must be rejected")
	}
}

func TestRevalidationProcessorDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	processor, err := NewRevalidationProcessor(source.URL, strings.Repeat("s", 32), source.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = processor.Process(context.Background(), Event{ID: "event-4", Type: "cache_purge", AggregateType: "site", AggregateID: "site-1", Payload: []byte(`{}`)})
	if err == nil || redirected {
		t.Fatalf("redirect must fail without forwarding signed headers: error=%v redirected=%v", err, redirected)
	}
}
