package operations

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var uploadUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

type MediaUploadInput struct {
	IdempotencyKey     string    `json:"idempotency_key"`
	TargetRef          string    `json:"target_ref"`
	ExpectedEpoch      string    `json:"expected_epoch"`
	ExpectedGeneration *int64    `json:"expected_generation"`
	ExpectedObservedAt time.Time `json:"expected_observed_at"`
	Mode               string    `json:"mode"`
	Reason             string    `json:"reason"`
	Confirmed          bool      `json:"confirmed"`
}

func (in MediaUploadInput) Valid() bool {
	if !in.Confirmed || !uploadUUID.MatchString(in.IdempotencyKey) || !usageDigest.MatchString(in.TargetRef) ||
		in.ExpectedGeneration == nil || *in.ExpectedGeneration < 0 || *in.ExpectedGeneration >= 1<<53-1 || in.ExpectedObservedAt.IsZero() ||
		(in.Mode != "paused" && in.Mode != "enabled") || !utf8.ValidString(in.Reason) || strings.TrimSpace(in.Reason) != in.Reason ||
		utf8.RuneCountInString(in.Reason) < 2 || utf8.RuneCountInString(in.Reason) > 1000 || strings.IndexFunc(in.Reason, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return false
	}
	return (*in.ExpectedGeneration == 0 && in.ExpectedEpoch == "" && in.Mode == "paused") || (*in.ExpectedGeneration > 0 && uploadUUID.MatchString(in.ExpectedEpoch))
}

type MediaUploadState struct {
	TargetRef     string          `json:"target_ref"`
	Receipt       json.RawMessage `json:"receipt"`
	LastSuccessAt *time.Time      `json:"last_success_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ErrorCode     *string         `json:"error_code"`
	Fresh         bool            `json:"fresh"`
}

type MediaUploadCommand struct {
	ID                string          `json:"command_id"`
	ActorID           string          `json:"actor_id"`
	Mode              string          `json:"mode"`
	Reason            string          `json:"reason"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	ExpiresAt         time.Time       `json:"expires_at"`
	FirstDispatchedAt *time.Time      `json:"first_dispatched_at"`
	CompletedAt       *time.Time      `json:"completed_at"`
	DispatchCount     int             `json:"dispatch_count"`
	ErrorCode         *string         `json:"error_code"`
	Receipt           json.RawMessage `json:"receipt"`
	IdempotentReplay  bool            `json:"idempotent_replay"`
}

type MediaUploadOverview struct {
	State    *MediaUploadState    `json:"state"`
	Commands []MediaUploadCommand `json:"commands"`
}

type MediaUploadRepository interface {
	GetMediaUploadControl(context.Context, time.Time) (MediaUploadOverview, error)
	SubmitMediaUploadCommand(context.Context, MediaUploadInput, string, string, time.Time) (MediaUploadCommand, error)
}
