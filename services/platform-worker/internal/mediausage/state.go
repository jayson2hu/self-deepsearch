package mediausage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotDue          = errors.New("usage_not_due")
	ErrLeaseLost       = errors.New("usage_lease_lost")
	ErrScheduleChanged = errors.New("usage_schedule_changed")
	ErrPeriodInactive  = errors.New("usage_period_inactive")
)

// Schedule identifies one explicitly confirmed account/period. It never rolls
// over by guessing calendar months. Tokens are deliberately not part of it.
type Schedule struct {
	AccountID   string
	PeriodStart time.Time
	PeriodEnd   time.Time // exclusive
	Every       time.Duration
	Policy      Policy
}

func (s Schedule) Validate() error {
	if !accountPattern.MatchString(s.AccountID) || !s.Policy.valid() ||
		s.PeriodStart.IsZero() || s.PeriodEnd.IsZero() || s.PeriodStart.Nanosecond() != 0 || s.PeriodEnd.Nanosecond() != 0 ||
		!s.PeriodEnd.After(s.PeriodStart) || s.PeriodEnd.Sub(s.PeriodStart) > retention ||
		s.Every < 5*time.Minute || s.Every > 15*time.Minute || s.Every > s.Policy.MaxAge/2 {
		return ErrConfig
	}
	return nil
}

func (s Schedule) AccountRef() string {
	hash := sha256.Sum256([]byte(strings.ToLower(s.AccountID)))
	return hex.EncodeToString(hash[:])
}

func (s Schedule) Fingerprint() string {
	// Fixed fields and UTC normalization keep restart identity deterministic.
	encoded, _ := json.Marshal(struct {
		Account       string
		Start, End    time.Time
		Every, MaxAge int64
		Policy        Policy
	}{s.AccountRef(), s.PeriodStart.UTC(), s.PeriodEnd.UTC(), int64(s.Every), int64(s.Policy.MaxAge), s.Policy})
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (s Schedule) PolicyDocument() string {
	encoded, _ := json.Marshal(struct {
		Policy
		MaxAgeSeconds    int64 `json:"max_age_seconds"`
		PollEverySeconds int64 `json:"poll_every_seconds"`
	}{s.Policy, int64(s.Policy.MaxAge / time.Second), int64(s.Every / time.Second)})
	return string(encoded)
}

// State is durable observation state, not a confirmation that any delivery
// controller has actually stopped. No method automatically clears its latches.
type State struct {
	HighWater       Counts
	LastSuccessAt   time.Time
	LastUntil       time.Time
	ReviewRequired  bool
	ReviewSince     time.Time
	ReviewReason    string
	StopRecommended bool
	LastAssessment  Assessment
	UpdatedAt       time.Time
}

func requireReview(state State, now time.Time, reason string) State {
	if !state.ReviewRequired {
		state.ReviewRequired, state.ReviewSince, state.ReviewReason = true, now, reason
	}
	state.LastAssessment = Assessment{"unknown", reason, "hold_for_review"}
	state.UpdatedAt = now
	return state
}

// Advance retains the last valid estimate on failure, stale data and regression.
// The returned observation is nil when it must not be treated as complete.
func Advance(previous State, o *Observation, fetchError error, s Schedule, now time.Time) (State, *Observation) {
	if s.Validate() != nil {
		return requireReview(previous, now, "policy_invalid"), nil
	}
	if now.Before(s.PeriodStart) || !now.Before(s.PeriodEnd) {
		return requireReview(previous, now, "usage_period_inactive"), nil
	}
	a := Assess(o, fetchError, s.PeriodStart, now, s.Policy)
	if a.Status == "unknown" {
		return requireReview(previous, now, a.Reason), nil
	}
	if !o.Window.EndInclusive.Before(s.PeriodEnd) || o.BoundaryPolicy != "inclusive_shared_endpoints" {
		return requireReview(previous, now, "observation_invalid"), nil
	}
	expectedSegments := int((o.Window.EndInclusive.Sub(o.Window.Start) + 24*time.Hour - 1) / (24 * time.Hour))
	if expectedSegments == 0 {
		expectedSegments = 1
	}
	if o.Segments != expectedSegments {
		return requireReview(previous, now, "observation_invalid"), nil
	}
	if o.Window.EndInclusive.Before(previous.LastUntil) || o.ReceivedAt.Before(previous.LastSuccessAt) ||
		o.Counts.ClassA < previous.HighWater.ClassA || o.Counts.ClassB < previous.HighWater.ClassB || o.Counts.Free < previous.HighWater.Free {
		return requireReview(previous, now, "usage_counter_regressed"), nil
	}
	next := previous
	next.HighWater, next.LastSuccessAt, next.LastUntil = o.Counts, o.ReceivedAt, o.Window.EndInclusive
	next.LastAssessment, next.UpdatedAt = a, now
	if a.Status == "stop_recommended" {
		next.StopRecommended = true
		if !next.ReviewRequired {
			next.ReviewRequired, next.ReviewSince, next.ReviewReason = true, now, a.Reason
		}
	}
	// A renewed low estimate may be recorded, but never clears prior review or
	// stop flags. Explicit authorized review/recovery is a separate future path.
	copy := *o
	return next, &copy
}

// EffectiveRecommendation also fails closed if the scheduler disappears or
// clocks move backwards. Consumers must evaluate freshness, not just a stored
// low_estimate. This is still a recommendation, not source authorization.
func (s State) EffectiveRecommendation(now time.Time, schedule Schedule) string {
	if s.StopRecommended {
		return "stop_media_review_required"
	}
	if schedule.Validate() != nil || s.ReviewRequired || s.LastSuccessAt.IsZero() || s.LastUntil.IsZero() ||
		now.Before(schedule.PeriodStart) || !now.Before(schedule.PeriodEnd) ||
		now.Before(s.LastSuccessAt) || now.Before(s.LastUntil) || now.Sub(s.LastSuccessAt) > schedule.Policy.MaxAge || now.Sub(s.LastUntil) > schedule.Policy.MaxAge {
		return "hold_for_review"
	}
	return s.LastAssessment.Recommendation
}
