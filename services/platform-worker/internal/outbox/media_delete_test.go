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

func TestMediaDeletionProcessorSignsExactBody(t *testing.T) {
	secret := strings.Repeat("m", 32)
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
		var payload mediaDeleteRequest
		if json.Unmarshal(body, &payload) != nil || payload.StorageScope != "public" || payload.StorageKey != "media-public/test.webp" ||
			strings.Contains(string(body), `"media_object_id"`) || request.Header.Get("X-SD-Signature") != expected {
			t.Fatalf("invalid signed request: body=%s signature=%s", body, request.Header.Get("X-SD-Signature"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "deleted", "event_id": eventID,
			"storage_scope": payload.StorageScope, "storage_key": payload.StorageKey,
		})
	}))
	defer server.Close()
	processor, err := NewMediaDeletionProcessor(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	processor.now = func() time.Time { return now }
	// New canonical tasks include immutable object identity; the existing
	// signed transport still forwards only the location required by Python.
	event := Event{ID: "11111111-1111-4111-8111-111111111111", Type: "media_delete", Payload: []byte(`{"media_object_id":"33333333-3333-4333-8333-333333333333","asset_id":"22222222-2222-4222-8222-222222222222","storage_scope":"public","storage_key":"media-public/test.webp","backup_path":"media-public/test.webp","public_url":"https://media.example/media-public/test.webp"}`)}
	if err := processor.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func TestMediaDeletionProcessorRetainsEventOnRemoteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	processor, err := NewMediaDeletionProcessor(server.URL, strings.Repeat("m", 32), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = processor.Process(context.Background(), Event{ID: "event-2", Type: "media_delete", Payload: []byte(`{"storage_scope":"private","storage_key":"media-master/test.webp","backup_path":"media-master/test.webp"}`)})
	if err == nil {
		t.Fatal("remote failure must keep the outbox event retryable")
	}
}

func TestMediaDeletionProcessorRejectsWeakConfiguration(t *testing.T) {
	if _, err := NewMediaDeletionProcessor("not-a-url", strings.Repeat("m", 32), nil); err == nil {
		t.Fatal("relative endpoint must be rejected")
	}
	if _, err := NewMediaDeletionProcessor("https://media.example/delete", "short", nil); err == nil {
		t.Fatal("weak secret must be rejected")
	}
}
