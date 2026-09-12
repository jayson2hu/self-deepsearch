package mediainspect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func assetBody(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "apps", "display-web", "public", strings.TrimPrefix(path, "/")))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDefaultContractsMatchShippedAssets(t *testing.T) {
	for _, asset := range defaultAssets {
		digest := sha256.Sum256(bytes.ReplaceAll(assetBody(t, asset.Path), []byte("\r\n"), []byte("\n")))
		if hex.EncodeToString(digest[:]) != asset.SHA256 {
			t.Fatalf("update worker and display default contracts together: %s", asset.Path)
		}
	}
}

func TestDefaultHTTPProbesAndRecovery(t *testing.T) {
	bodies := make(map[string][]byte)
	for _, asset := range defaultAssets {
		bodies[asset.Path] = assetBody(t, asset.Path)
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	for _, test := range []struct{ name, want string }{
		{"healthy", ""}, {"crlf", ""}, {"missing", "http_status"}, {"redirect", "http_status"},
		{"wrong-type", "content_type"}, {"login-as-svg", "content_mismatch"}, {"empty", "content_mismatch"},
		{"oversized", "body_invalid"}, {"truncated", "body_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var recovered atomic.Bool
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, exists := bodies[r.URL.Path]
				if !exists || r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error("unexpected probe target or credentials")
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "image/svg+xml")
				if !recovered.Load() && r.URL.Path == defaultAssets[0].Path {
					switch test.name {
					case "crlf":
						body = bytes.ReplaceAll(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
					case "missing":
						w.WriteHeader(404)
						return
					case "redirect":
						http.Redirect(w, r, target.URL+"/private", 302)
						return
					case "wrong-type":
						w.Header().Set("Content-Type", "text/html")
					case "login-as-svg":
						body = []byte("<html>sign in</html>")
					case "empty":
						body = nil
					case "oversized":
						body = bytes.Repeat([]byte("x"), 65537)
					case "truncated":
						w.Header().Set("Content-Length", "60000")
					}
				}
				_, _ = w.Write(body)
			}))
			defer server.Close()
			client, err := NewHTTPDefaults(server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			results := client.Check(context.Background())
			if !validDefaultResults(results) || results[0].ErrorCode != test.want || results[1].ErrorCode != "" {
				t.Fatalf("unexpected report: %+v", results)
			}
			recovered.Store(true)
			results = client.Check(context.Background())
			if results[0].ErrorCode != "" || results[1].ErrorCode != "" || requests.Load() != 4 {
				t.Fatalf("recovery did not re-read actual assets: %+v", results)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("followed redirect to unrelated endpoint")
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingBody) Close() error             { return nil }

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDefaultProbeBoundsAndConfiguration(t *testing.T) {
	for _, origin := range []string{"", "ftp://display.test", "https://display.test/x", "https://u:p@display.test", "https://display.test?", "https://display.test#", "https://display.test/?next=http://private"} {
		if _, err := NewHTTPDefaults(origin, nil); err == nil {
			t.Fatalf("invalid origin accepted: %s", origin)
		}
	}
	client, err := NewHTTPDefaults("http://display.test/", &http.Client{Timeout: time.Hour, Transport: probeTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/svg+xml"}}, Body: failingBody{}}, nil
	})})
	if err != nil || client.client.Timeout != 5*time.Second || client.Origin() != "http://display.test" {
		t.Fatal("probe deadline/origin contract broken")
	}
	if report := client.Check(context.Background()); report[0].ErrorCode != "body_invalid" || report[1].ErrorCode != "body_invalid" {
		t.Fatal(report)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, _ = NewHTTPDefaults(server.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if report := client.Check(ctx); report[0].ErrorCode != "request_failed" || report[1].ErrorCode != "request_failed" {
		t.Fatal(report)
	}
	if time.Since(start) > time.Second {
		t.Fatal("probe ignored cancellation")
	}
}
