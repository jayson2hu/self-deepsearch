package mediareconcile

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPClient struct {
	endpoint string
	secret   []byte
	client   *http.Client
	now      func() time.Time
}

type reconcileRequest struct {
	RunID   string   `json:"run_id"`
	Objects []Object `json:"objects"`
}

func NewHTTPClient(endpoint, secret string, client *http.Client) (*HTTPClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("MEDIA_RECONCILE_URL must be an absolute HTTP(S) URL")
	}
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("MEDIA_HMAC_SECRET must contain at least 32 characters")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPClient{endpoint: parsed.String(), secret: []byte(strings.TrimSpace(secret)), client: &copy, now: time.Now}, nil
}

func (client *HTTPClient) Reconcile(ctx context.Context, run Run) (Report, error) {
	body, err := json.Marshal(reconcileRequest{RunID: run.ID, Objects: run.Objects})
	if err != nil {
		return Report{}, fmt.Errorf("encode media reconciliation: %w", err)
	}
	timestamp := fmt.Sprintf("%d", client.now().UTC().Unix())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return Report{}, fmt.Errorf("create media reconciliation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-SD-Event-ID", run.ID)
	request.Header.Set("X-SD-Timestamp", timestamp)
	request.Header.Set("X-SD-Signature", sign(client.secret, timestamp, run.ID, body))
	response, err := client.client.Do(request)
	if err != nil {
		return Report{}, fmt.Errorf("send media reconciliation: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return Report{}, fmt.Errorf("media reconciliation returned HTTP %d", response.StatusCode)
	}
	return decodeReport(response, run)
}

func sign(secret []byte, timestamp, eventID string, body []byte) string {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(timestamp + "\n" + eventID + "\n"))
	_, _ = digest.Write(body)
	return "v1=" + hex.EncodeToString(digest.Sum(nil))
}
