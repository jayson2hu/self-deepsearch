// Package collectoringest defines the v1 performer metadata staging contract.
package collectoringest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 2 * 1024 * 1024
const MaxRecords = 500
const Path = "/internal/v1/collector/performer-batches"

var ErrInvalid = errors.New("invalid collector batch")
var ErrConflict = errors.New("collector idempotency conflict")
var ErrSource = errors.New("collector source unavailable")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var fieldPattern = regexp.MustCompile(`^[a-z_]+$`)
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Credentials bind a single service secret to a single pre-registered source.
type Credentials struct{ SourceID, Token string }

func (c Credentials) Valid() bool {
	if !uuidPattern.MatchString(c.SourceID) || len(c.Token) < 32 || len(c.Token) > 4096 {
		return false
	}
	for _, r := range c.Token {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

type Payload struct {
	Name              string   `json:"name"`
	Aliases           []string `json:"aliases"`
	ProfileURL        string   `json:"profile_url"`
	SourceContentHash string   `json:"source_content_hash"`
}
type Record struct {
	ExternalID string  `json:"external_id"`
	CheckedAt  string  `json:"checked_at"`
	Payload    Payload `json:"payload"`
}
type Batch struct {
	SchemaVersion    string   `json:"schema_version"`
	SourceID         string   `json:"source_id"`
	Region           string   `json:"region"`
	ConnectorVersion string   `json:"connector_version"`
	IdempotencyKey   string   `json:"idempotency_key"`
	Records          []Record `json:"records"`
}
type Receipt struct {
	BatchID        string    `json:"batch_id"`
	RequestHash    string    `json:"request_hash"`
	Status         string    `json:"status"`
	InputCount     int       `json:"input_count"`
	InsertedCount  int       `json:"inserted_count"`
	DuplicateCount int       `json:"duplicate_count"`
	ReceivedAt     time.Time `json:"received_at"`
}
type Repository interface {
	IngestPerformers(context.Context, Batch, string) (Receipt, bool, error)
}

func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func textValid(s string, limit int) bool {
	if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > limit {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Decode rejects duplicate JSON keys, unknown fields, trailing data and invalid UTF-8.
func Decode(raw []byte, now time.Time) (Batch, error) {
	var b Batch
	if len(raw) > MaxBytes || !utf8.Valid(raw) {
		return b, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueValue(d); err != nil {
		return b, ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return b, ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || b.SchemaVersion != "collector.performers.v1" || !uuidPattern.MatchString(b.SourceID) || (b.Region != "japan" && b.Region != "beijing") || !textValid(b.ConnectorVersion, 200) || !keyPattern.MatchString(b.IdempotencyKey) || len(b.Records) < 1 || len(b.Records) > MaxRecords {
		return b, ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range b.Records {
		checked, err := time.Parse(time.RFC3339Nano, r.CheckedAt)
		u, urlErr := url.Parse(r.Payload.ProfileURL)
		if !textValid(r.ExternalID, 255) || seen[r.ExternalID] || err != nil || checked.Year() < 2000 || checked.After(now.Add(5*time.Minute)) || !textValid(r.Payload.Name, 200) || !hashPattern.MatchString(r.Payload.SourceContentHash) || len(r.Payload.ProfileURL) > 2048 || urlErr != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || r.Payload.Aliases == nil || len(r.Payload.Aliases) > 30 {
			return b, ErrInvalid
		}
		seen[r.ExternalID] = true
		aliases := map[string]bool{}
		for _, a := range r.Payload.Aliases {
			if !textValid(a, 200) || aliases[a] {
				return b, ErrInvalid
			}
			aliases[a] = true
		}
	}
	return b, nil
}
func uniqueValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || !fieldPattern.MatchString(name) || keys[name] {
				return ErrInvalid
			}
			keys[name] = true
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
