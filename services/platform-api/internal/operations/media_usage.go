package operations

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type MediaUsageReviewInput struct {
	IdempotencyKey    string     `json:"idempotency_key"`
	Action            string     `json:"action"`
	ExpectedDigest    string     `json:"expected_digest"`
	ExpectedUpdatedAt time.Time  `json:"expected_updated_at"`
	NextPeriodStart   *time.Time `json:"next_period_start"`
	NextPeriodEnd     *time.Time `json:"next_period_end"`
	Reason            string     `json:"reason"`
	Confirmed         bool       `json:"confirmed"`
}

var usageDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var usageUUID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func (r MediaUsageReviewInput) Valid() bool {
	if !r.Confirmed || !usageUUID.MatchString(r.IdempotencyKey) || !usageDigest.MatchString(r.ExpectedDigest) || r.ExpectedUpdatedAt.IsZero() ||
		len([]rune(strings.TrimSpace(r.Reason))) < 2 || len([]rune(r.Reason)) > 1000 {
		return false
	}
	if r.Action == "acknowledge" {
		return r.NextPeriodStart == nil && r.NextPeriodEnd == nil
	}
	return r.Action == "rotate_period" && r.NextPeriodStart != nil && r.NextPeriodEnd != nil && !r.NextPeriodStart.IsZero() &&
		r.NextPeriodStart.Nanosecond() == 0 && r.NextPeriodEnd.Nanosecond() == 0 && r.NextPeriodEnd.After(*r.NextPeriodStart) &&
		r.NextPeriodEnd.Sub(*r.NextPeriodStart) <= 31*24*time.Hour
}

type MediaUsageState struct {
	ConfigDigest    string     `json:"config_digest"`
	PeriodStart     time.Time  `json:"period_start"`
	PeriodEnd       time.Time  `json:"period_end"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastSuccessAt   *time.Time `json:"last_success_at"`
	LastUntil       *time.Time `json:"last_until"`
	ClassAHighwater string     `json:"class_a_highwater"`
	ClassBHighwater string     `json:"class_b_highwater"`
	ReviewRequired  bool       `json:"review_required"`
	ReviewReason    *string    `json:"review_reason"`
	StopRecommended bool       `json:"stop_recommended"`
	LastStatus      string     `json:"last_status"`
	ActiveRun       bool       `json:"active_run"`
	LastReviewID    *string    `json:"last_review_id"`
	// Staleness is evaluated at read time; cleared latches are not permission to
	// resume delivery. Decimal counts remain strings to preserve uint64 values.
	ObservationFresh bool `json:"observation_fresh"`
}

type MediaUsageReview struct {
	ID               string     `json:"review_id"`
	Action           string     `json:"action"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	ErrorCode        *string    `json:"error_code"`
	Reason           string     `json:"reason"`
	NextPeriodStart  *time.Time `json:"next_period_start"`
	NextPeriodEnd    *time.Time `json:"next_period_end"`
	IdempotentReplay bool       `json:"idempotent_replay"`
}

type MediaUsageOverview struct {
	State       *MediaUsageState   `json:"state"`
	Reviews     []MediaUsageReview `json:"reviews"`
	Enforcement string             `json:"enforcement"`
}

// Separate capability avoids expanding every editorial repository test double.
type MediaUsageRepository interface {
	GetMediaUsage(context.Context, time.Time) (MediaUsageOverview, error)
	SubmitMediaUsageReview(context.Context, MediaUsageReviewInput, string, string, time.Time) (MediaUsageReview, error)
}
