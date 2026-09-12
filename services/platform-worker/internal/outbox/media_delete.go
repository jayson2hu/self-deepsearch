package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MediaDeletionProcessor struct {
	endpoint string
	secret   []byte
	client   *http.Client
	now      func() time.Time
}

type mediaDeletePayload struct {
	AssetID      string  `json:"asset_id"`
	StorageScope string  `json:"storage_scope"`
	StorageKey   string  `json:"storage_key"`
	BackupPath   string  `json:"backup_path"`
	PublicURL    *string `json:"public_url"`
}

type mediaDeleteRequest struct {
	EventID      string  `json:"event_id"`
	StorageScope string  `json:"storage_scope"`
	StorageKey   string  `json:"storage_key"`
	BackupPath   string  `json:"backup_path"`
	PublicURL    *string `json:"public_url"`
}

func NewMediaDeletionProcessor(endpoint, secret string, client *http.Client) (*MediaDeletionProcessor, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("MEDIA_DELETE_URL must be an absolute HTTP(S) URL")
	}
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("MEDIA_HMAC_SECRET must contain at least 32 characters")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &MediaDeletionProcessor{endpoint: parsed.String(), secret: []byte(strings.TrimSpace(secret)), client: &clientCopy, now: time.Now}, nil
}

func (processor *MediaDeletionProcessor) Process(ctx context.Context, event Event) error {
	if event.Type != "media_delete" {
		return fmt.Errorf("unsupported media event type %q", event.Type)
	}
	var payload mediaDeletePayload
	if len(event.Payload) == 0 || json.Unmarshal(event.Payload, &payload) != nil || payload.StorageKey == "" || payload.BackupPath == "" {
		return processingError{code: "media_delete_invalid", err: errors.New("media deletion payload is invalid")}
	}
	validLocation := (payload.StorageScope == "private" && strings.HasPrefix(payload.StorageKey, "media-master/")) ||
		(payload.StorageScope == "public" && strings.HasPrefix(payload.StorageKey, "media-public/"))
	if !validLocation || payload.BackupPath != payload.StorageKey {
		return processingError{code: "media_delete_invalid", err: errors.New("media deletion storage scope is invalid")}
	}
	if (payload.StorageScope == "private" && payload.PublicURL != nil) || (payload.StorageScope == "public" && (payload.PublicURL == nil || !strings.HasPrefix(*payload.PublicURL, "https://"))) {
		return processingError{code: "media_delete_invalid", err: errors.New("media deletion public URL is invalid")}
	}
	body, err := json.Marshal(mediaDeleteRequest{EventID: event.ID, StorageScope: payload.StorageScope, StorageKey: payload.StorageKey, BackupPath: payload.BackupPath, PublicURL: payload.PublicURL})
	if err != nil {
		return processingError{code: "media_delete_encode", err: fmt.Errorf("encode media deletion: %w", err)}
	}
	timestamp := fmt.Sprintf("%d", processor.now().UTC().Unix())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, processor.endpoint, bytes.NewReader(body))
	if err != nil {
		return processingError{code: "media_delete_request", err: fmt.Errorf("create media deletion request: %w", err)}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-SD-Event-ID", event.ID)
	request.Header.Set("X-SD-Timestamp", timestamp)
	request.Header.Set("X-SD-Signature", signRevalidation(processor.secret, timestamp, event.ID, body))
	response, err := processor.client.Do(request)
	if err != nil {
		return processingError{code: "media_delete_unavailable", err: fmt.Errorf("send media deletion: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := "media_delete_unavailable"
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			code = "media_delete_rejected"
		}
		return processingError{code: code, err: fmt.Errorf("media deletion returned HTTP %d", response.StatusCode)}
	}
	var acknowledgement struct {
		Status       string `json:"status"`
		EventID      string `json:"event_id"`
		StorageScope string `json:"storage_scope"`
		StorageKey   string `json:"storage_key"`
	}
	if err := decodeAcknowledgement(response, &acknowledgement); err != nil {
		return processingError{code: "media_delete_invalid_response", err: err}
	}
	if acknowledgement.Status != "deleted" || acknowledgement.EventID != event.ID ||
		acknowledgement.StorageScope != payload.StorageScope || acknowledgement.StorageKey != payload.StorageKey {
		return processingError{code: "media_delete_invalid_response", err: errors.New("media completion acknowledgement does not match the event and object")}
	}
	return nil
}
