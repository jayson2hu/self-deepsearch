package operations

import (
	"context"
	"time"
)

// MediaDeliveryDefaultOnly evaluates the durable usage state at request time.
// It is intentionally conservative: a review latch is only cleared by the
// separate authorized review flow, so a later low sample cannot implicitly
// restore images after an observed failure or stop recommendation.
func MediaDeliveryDefaultOnly(state *MediaUsageState, now time.Time) bool {
	if state == nil || now.IsZero() || state.ReviewRequired || state.StopRecommended ||
		state.LastSuccessAt == nil || state.LastUntil == nil || now.Before(state.PeriodStart) || !now.Before(state.PeriodEnd) ||
		now.Before(*state.LastSuccessAt) || now.Before(*state.LastUntil) || now.Sub(*state.LastSuccessAt) > 30*time.Minute ||
		now.Sub(*state.LastUntil) > 30*time.Minute {
		return true
	}
	return state.LastStatus != "low_estimate" && state.LastStatus != "warning"
}

// Separate capability keeps the dynamic public projection read-only and does
// not expand the large editorial repository contract or grant state writes.
type MediaDeliveryPolicyRepository interface {
	MediaDeliveryDefaultOnly(context.Context, time.Time) (bool, error)
}
