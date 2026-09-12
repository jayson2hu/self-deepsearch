package outbox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Invoked by the Node harness against its own production Next.js process.
// No PostgreSQL, outbox scheduling, Cloudflare or real catalog is involved.
func TestRevalidationNextContract(t *testing.T) {
	endpoint := os.Getenv("NEXT_CONTRACT_URL")
	if endpoint == "" {
		t.Skip("run npm run test:cache:release-a to start the isolated Next.js contract")
	}
	if !validNextContractEndpoint(endpoint) {
		t.Fatal("contract endpoint must be an explicit IPv4 loopback HTTP revalidation URL")
	}
	mode := os.Getenv("NEXT_CONTRACT_MODE")
	if mode != "valid" && mode != "wrong-secret" && mode != "stale" {
		t.Fatal("unsupported contract mode")
	}
	secret := os.Getenv("NEXT_CONTRACT_SECRET")
	if len(secret) < 32 {
		t.Fatal("missing ephemeral contract secret")
	}
	if mode == "wrong-secret" {
		secret += "-wrong"
	}
	transport := &nextContractTransport{base: &http.Transport{}}
	defer transport.base.CloseIdleConnections()
	processor, err := NewRevalidationProcessor(endpoint, secret, &http.Client{
		Timeout: 10 * time.Second, Transport: transport,
	})
	if err != nil {
		t.Fatal("invalid contract processor configuration")
	}
	if mode == "stale" {
		processor.now = func() time.Time { return time.Now().Add(-10 * time.Minute) }
	}
	event := Event{
		ID: os.Getenv("NEXT_CONTRACT_EVENT_ID"), Type: os.Getenv("NEXT_CONTRACT_EVENT_TYPE"),
		AggregateType: "work", AggregateID: os.Getenv("NEXT_CONTRACT_WORK_ID"), Payload: []byte(`{}`),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	err = processor.Process(ctx, event)
	var coded processingError
	errors.As(err, &coded)
	if mode == "valid" {
		if err != nil || transport.status != http.StatusOK {
			t.Fatalf("real Next acknowledgement failed: HTTP %d, code=%s", transport.status, coded.code)
		}
	} else if err == nil || transport.status != http.StatusUnauthorized || coded.code != "revalidate_rejected" {
		t.Fatalf("invalid request must be rejected with HTTP 401: HTTP %d, code=%s", transport.status, coded.code)
	}
}

type nextContractTransport struct {
	base   *http.Transport
	status int
}

func (transport *nextContractTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if response != nil {
		transport.status = response.StatusCode
	}
	return response, err
}

func validNextContractEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && parsed.Scheme == "http" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" &&
		parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" &&
		!strings.Contains(endpoint, "#") && parsed.EscapedPath() == "/api/internal/revalidate"
}

func TestNextContractEndpointGuard(t *testing.T) {
	for _, endpoint := range []string{
		"https://example.test/api/internal/revalidate", "http://127.0.0.1/api/internal/revalidate",
		"http://localhost:3000/api/internal/revalidate", "http://127.0.0.1:3000/other",
		"http://user:pass@127.0.0.1:3000/api/internal/revalidate",
		"http://127.0.0.1:3000/api/internal/revalidate?", "http://127.0.0.1:3000/api/internal/revalidate#",
	} {
		if validNextContractEndpoint(endpoint) {
			t.Fatalf("unsafe contract endpoint accepted: %s", endpoint)
		}
	}
	if !validNextContractEndpoint("http://127.0.0.1:3000/api/internal/revalidate") {
		t.Fatal("loopback fixture URL rejected")
	}
}
