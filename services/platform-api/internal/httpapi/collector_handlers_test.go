package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"self-deepsearch/services/platform-api/internal/collectoringest"
	"strings"
	"testing"
	"time"
)

type collectorStub struct {
	calls  int
	err    error
	replay bool
}

func TestCollectorRejectsAmbiguousHeaders(t *testing.T) {
	source := "12345678-1234-1234-1234-123456789abc"
	token := strings.Repeat("t", 40)
	body, err := json.Marshal(collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: source, Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "headers", Records: []collectoringest.Record{{ExternalID: "example", CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: "Synthetic", Aliases: []string{}, ProfileURL: "https://example.test/person", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, header, extra, code string
		status                    int
	}{
		{"duplicate authorization", "Authorization", "Bearer " + token, "COLLECTOR_UNAUTHORIZED", http.StatusUnauthorized},
		{"conflicting authorization", "Authorization", "Bearer wrong", "COLLECTOR_UNAUTHORIZED", http.StatusUnauthorized},
		{"duplicate hash", "X-Content-SHA256", collectoringest.Hash(body), "COLLECTOR_HASH_MISMATCH", http.StatusBadRequest},
		{"conflicting hash", "X-Content-SHA256", "wrong", "COLLECTOR_HASH_MISMATCH", http.StatusBadRequest},
		{"duplicate content type", "Content-Type", "application/json", "COLLECTOR_MEDIA_TYPE", http.StatusUnsupportedMediaType},
		{"conflicting content type", "Content-Type", "text/plain", "COLLECTOR_MEDIA_TYPE", http.StatusUnsupportedMediaType},
		{"hidden content encoding", "Content-Encoding", "gzip", "COLLECTOR_MEDIA_TYPE", http.StatusUnsupportedMediaType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &collectorStub{}
			handler := New(Options{Collector: stub, CollectorCredentials: collectoringest.Credentials{SourceID: source, Token: token}, Now: func() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) }})
			r := httptest.NewRequest(http.MethodPost, collectoringest.Path, strings.NewReader(string(body)))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Content-SHA256", collectoringest.Hash(body))
			if tc.header == "Content-Encoding" {
				r.Header.Set("Content-Encoding", "")
			}
			r.Header.Add(tc.header, tc.extra)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var response errorEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || response.Error.Code != tc.code || response.Error.RequestID == "" || stub.calls != 0 {
				t.Fatalf("status %d, response %+v, repository calls %d", w.Code, response, stub.calls)
			}
		})
	}
}

func (c *collectorStub) IngestPerformers(_ context.Context, b collectoringest.Batch, h string) (collectoringest.Receipt, bool, error) {
	c.calls++
	return collectoringest.Receipt{RequestHash: h, Status: "staged", InputCount: len(b.Records)}, c.replay, c.err
}
func TestCollectorHTTP(t *testing.T) {
	source := "12345678-1234-1234-1234-123456789abc"
	token := strings.Repeat("t", 40)
	body, _ := json.Marshal(collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: source, Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "batch-1", Records: []collectoringest.Record{{ExternalID: "example", CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: "Synthetic", Aliases: []string{}, ProfileURL: "https://example.test/models/example/", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}})
	cases := []struct {
		name                         string
		status                       int
		auth, hash, origin, encoding string
		disabled, replay             bool
		failure                      error
	}{
		{name: "created", status: 201, auth: token}, {name: "replay", status: 200, auth: token, replay: true},
		{name: "disabled", status: 404, disabled: true}, {name: "anonymous", status: 401}, {name: "bad credential", status: 401, auth: "wrong"},
		{name: "browser", status: 403, auth: token, origin: "https://example.test"}, {name: "hash", status: 400, auth: token, hash: "wrong"},
		{name: "gzip", status: 415, auth: token, encoding: "gzip"}, {name: "conflict", status: 409, auth: token, failure: collectoringest.ErrConflict},
		{name: "source blocked", status: 403, auth: token, failure: collectoringest.ErrSource}, {name: "unavailable", status: 503, auth: token, failure: errors.New("secret database detail")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &collectorStub{err: tc.failure, replay: tc.replay}
			cred := collectoringest.Credentials{SourceID: source, Token: token}
			if tc.disabled {
				cred = collectoringest.Credentials{}
			}
			handler := New(Options{Collector: stub, CollectorCredentials: cred, SecureCookies: true, Now: func() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) }})
			r := httptest.NewRequest("POST", collectoringest.Path, strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+tc.auth)
			hash := collectoringest.Hash(body)
			if tc.hash != "" {
				hash = tc.hash
			}
			r.Header.Set("X-Content-SHA256", hash)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Encoding", tc.encoding)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "secret database detail") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unsafe response")
			}
			if tc.failure == nil && tc.status >= 400 && stub.calls != 0 {
				t.Fatal("rejected request reached database")
			}
		})
	}
}

func TestCollectorRejectsSourceMalformedAndOversizedBodies(t *testing.T) {
	source := "12345678-1234-1234-1234-123456789abc"
	token := strings.Repeat("t", 40)
	valid := collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abd", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "example", Records: []collectoringest.Record{{ExternalID: "example", CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: "Synthetic", Aliases: []string{}, ProfileURL: "https://example.test/person", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}}
	foreign, _ := json.Marshal(valid)
	for _, tc := range []struct {
		body   string
		status int
	}{{string(foreign), 403}, {"{}", 400}, {strings.Repeat(" ", collectoringest.MaxBytes+1), 413}} {
		stub := &collectorStub{}
		handler := New(Options{Collector: stub, CollectorCredentials: collectoringest.Credentials{SourceID: source, Token: token}})
		r := httptest.NewRequest("POST", collectoringest.Path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Content-SHA256", collectoringest.Hash([]byte(tc.body)))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || stub.calls != 0 {
			t.Fatalf("status %d, calls %d", w.Code, stub.calls)
		}
	}
}
