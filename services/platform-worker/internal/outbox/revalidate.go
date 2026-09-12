package outbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RevalidationProcessor struct {
	endpoint string
	secret   []byte
	client   *http.Client
	now      func() time.Time
}

type revalidationRequest struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Payload       json.RawMessage `json:"payload"`
}

func NewRevalidationProcessor(endpoint, secret string, client *http.Client) (*RevalidationProcessor, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("DISPLAY_REVALIDATE_URL must be an absolute HTTP(S) URL")
	}
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("CACHE_HMAC_SECRET must contain at least 32 characters")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &RevalidationProcessor{endpoint: parsed.String(), secret: []byte(strings.TrimSpace(secret)), client: &clientCopy, now: time.Now}, nil
}

func (processor *RevalidationProcessor) Process(ctx context.Context, event Event) error {
	if err := validateEvent(ctx, event); err != nil {
		return err
	}
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(revalidationRequest{
		EventID: event.ID, EventType: event.Type, AggregateType: event.AggregateType,
		AggregateID: event.AggregateID, Payload: payload,
	})
	if err != nil {
		return processingError{code: "revalidate_encode", err: fmt.Errorf("encode cache revalidation: %w", err)}
	}
	timestamp := fmt.Sprintf("%d", processor.now().UTC().Unix())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, processor.endpoint, bytes.NewReader(body))
	if err != nil {
		return processingError{code: "revalidate_request", err: fmt.Errorf("create cache revalidation request: %w", err)}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-SD-Event-ID", event.ID)
	request.Header.Set("X-SD-Timestamp", timestamp)
	request.Header.Set("X-SD-Signature", signRevalidation(processor.secret, timestamp, event.ID, body))

	response, err := processor.client.Do(request)
	if err != nil {
		return processingError{code: "revalidate_unavailable", err: fmt.Errorf("send cache revalidation: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := "revalidate_unavailable"
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			code = "revalidate_rejected"
		}
		return processingError{code: code, err: fmt.Errorf("cache revalidation returned HTTP %d", response.StatusCode)}
	}
	var acknowledgement struct {
		Revalidated bool   `json:"revalidated"`
		EventID     string `json:"event_id"`
	}
	if err := decodeAcknowledgement(response, &acknowledgement); err != nil {
		return processingError{code: "revalidate_invalid_response", err: err}
	}
	if !acknowledgement.Revalidated || acknowledgement.EventID != event.ID {
		return processingError{code: "revalidate_invalid_response", err: errors.New("cache completion acknowledgement does not match the event")}
	}
	return nil
}

func signRevalidation(secret []byte, timestamp, eventID string, body []byte) string {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(timestamp))
	_, _ = digest.Write([]byte("\n"))
	_, _ = digest.Write([]byte(eventID))
	_, _ = digest.Write([]byte("\n"))
	_, _ = digest.Write(body)
	return "v1=" + hex.EncodeToString(digest.Sum(nil))
}
