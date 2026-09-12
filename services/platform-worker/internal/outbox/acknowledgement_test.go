package outbox

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProcessorsRequireBoundCompletionAcknowledgement(t *testing.T) {
	eventID := "11111111-1111-4111-8111-111111111111"
	for _, kind := range []string{"revalidate", "media_delete"} {
		t.Run(kind, func(t *testing.T) {
			event := Event{ID: eventID, Type: "publication_hidden", AggregateType: "work", AggregateID: "22222222-2222-4222-8222-222222222222", Payload: []byte(`{}`)}
			ack := map[string]any{"revalidated": true, "event_id": eventID}
			if kind == "media_delete" {
				event.Type = "media_delete"
				event.Payload = []byte(`{"storage_scope":"private","storage_key":"media-master/test.webp","backup_path":"media-master/test.webp","public_url":null}`)
				ack = map[string]any{"status": "deleted", "event_id": eventID, "storage_scope": "private", "storage_key": "media-master/test.webp"}
			}
			encode := func(value map[string]any) string {
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return string(body)
			}
			changed := func(key string, value any) string {
				copy := make(map[string]any, len(ack))
				for name, item := range ack {
					copy[name] = item
				}
				copy[key] = value
				return encode(copy)
			}
			body := encode(ack)
			type ackCase struct {
				name        string
				status      int
				contentType string
				body        string
				valid       bool
			}
			cases := []ackCase{
				{"confirmed", 200, "application/json; charset=utf-8", body, true},
				{"empty", 200, "application/json", "", false},
				{"html", 200, "text/html", "<html>Sign in</html>", false},
				{"wrong_content_type", 200, "text/plain", body, false},
				{"invalid_json", 200, "application/json", "{", false},
				{"missing_fields", 200, "application/json", `{}`, false},
				{"different_event", 200, "application/json", changed("event_id", "33333333-3333-4333-8333-333333333333"), false},
				{"null_event", 200, "application/json", changed("event_id", nil), false},
				{"oversized", 200, "application/json", body + strings.Repeat(" ", 4096), false},
				{"multiple_documents", 200, "application/json", body + `{}`, false},
				{"invalid_utf8", 200, "application/json", body[:len(body)-1] + ",\"extra\":\"\xff\"}", false},
				{"accepted_not_completed", 202, "application/json", body, false},
			}
			if kind == "revalidate" {
				cases = append(cases,
					ackCase{"not_revalidated", 200, "application/json", changed("revalidated", false), false},
					ackCase{"wrong_flag_type", 200, "application/json", changed("revalidated", "true"), false},
				)
			} else {
				cases = append(cases,
					ackCase{"not_deleted", 200, "application/json", changed("status", "pending"), false},
					ackCase{"different_object", 200, "application/json", changed("storage_key", "media-master/other.webp"), false},
					ackCase{"different_scope", 200, "application/json", changed("storage_scope", "public"), false},
					ackCase{"missing_object", 200, "application/json", changed("storage_key", nil), false},
				)
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", test.contentType)
						w.WriteHeader(test.status)
						_, _ = io.WriteString(w, test.body)
					}))
					defer server.Close()
					var processor Processor
					var err error
					if kind == "revalidate" {
						processor, err = NewRevalidationProcessor(server.URL, strings.Repeat("s", 32), server.Client())
					} else {
						processor, err = NewMediaDeletionProcessor(server.URL, strings.Repeat("s", 32), server.Client())
					}
					if err != nil {
						t.Fatal(err)
					}
					repository := &fakeRepository{events: []Event{event}}
					dispatcher := Dispatcher{
						Repository: repository, WorkerID: "ack-test", Processor: processor,
						Now: time.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
					}
					if err := dispatcher.runOnce(context.Background()); err != nil {
						t.Fatal(err)
					}
					if test.valid {
						if len(repository.completed) != 1 || len(repository.failed) != 0 {
							t.Fatalf("matching acknowledgement must complete the event: completed=%v failed=%v", repository.completed, repository.failed)
						}
					} else if len(repository.completed) != 0 || len(repository.failed) != 1 || repository.failureCodes[0] != kind+"_invalid_response" {
						t.Fatalf("unconfirmed response must retry, never complete: completed=%v failed=%v codes=%v", repository.completed, repository.failed, repository.failureCodes)
					}
				})
			}
		})
	}
}
