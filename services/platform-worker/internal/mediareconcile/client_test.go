package mediareconcile

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

func TestHTTPClientSignsInventoryAndReadsReport(t *testing.T) {
	secret := strings.Repeat("r", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		timestamp, runID := request.Header.Get("X-SD-Timestamp"), request.Header.Get("X-SD-Event-ID")
		digest := hmac.New(sha256.New, []byte(secret))
		_, _ = digest.Write([]byte(timestamp + "\n" + runID + "\n"))
		_, _ = digest.Write(body)
		expected := "v1=" + hex.EncodeToString(digest.Sum(nil))
		if request.Header.Get("X-SD-Signature") != expected || !strings.Contains(string(body), `"storage_scope":"private"`) {
			t.Fatalf("invalid reconciliation request: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(completeReport(reportRun()))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Unix(123, 0) }
	run := Run{ID: "10000000-0000-4000-8000-000000000001", Objects: []Object{{StorageScope: "private", StorageKey: "media-master/a", BackupPath: "media-master/a", SHA256: strings.Repeat("a", 64), ByteSize: 1}}}
	report, err := client.Reconcile(context.Background(), run)
	if err != nil || report.RunID != run.ID || report.ExpectedCount != 1 {
		t.Fatalf("unexpected report: %#v error=%v", report, err)
	}
}
