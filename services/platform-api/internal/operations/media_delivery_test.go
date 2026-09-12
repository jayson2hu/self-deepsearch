package operations

import (
	"testing"
	"time"
)

func TestMediaDeliveryDefaultOnlyRequiresFreshUnlatchedLowOrWarningState(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	success, until := now.Add(-time.Minute), now.Add(-2*time.Minute)
	base := MediaUsageState{PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), LastSuccessAt: &success, LastUntil: &until, LastStatus: "low_estimate"}
	if MediaDeliveryDefaultOnly(&base, now) {
		t.Fatal("fresh low state suppressed delivery")
	}
	warning := base
	warning.LastStatus = "warning"
	if MediaDeliveryDefaultOnly(&warning, now) {
		t.Fatal("fresh warning state should remain visible while review is not latched")
	}
	tests := map[string]func(*MediaUsageState){
		"review":       func(s *MediaUsageState) { s.ReviewRequired = true },
		"stop":         func(s *MediaUsageState) { s.StopRecommended = true },
		"high":         func(s *MediaUsageState) { s.LastStatus = "high" },
		"unknown":      func(s *MediaUsageState) { s.LastStatus = "unknown" },
		"missing-time": func(s *MediaUsageState) { s.LastUntil = nil },
		"stale": func(s *MediaUsageState) {
			value := now.Add(-31 * time.Minute)
			s.LastSuccessAt, s.LastUntil = &value, &value
		},
		"future": func(s *MediaUsageState) { value := now.Add(time.Second); s.LastSuccessAt, s.LastUntil = &value, &value },
		"before-period": func(s *MediaUsageState) {
			s.PeriodStart, s.PeriodEnd = now.Add(time.Minute), now.Add(time.Hour)
		},
		"after-period": func(s *MediaUsageState) { s.PeriodEnd = now },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			state := base
			mutate(&state)
			if !MediaDeliveryDefaultOnly(&state, now) {
				t.Fatal("unsafe state exposed images")
			}
		})
	}
	if !MediaDeliveryDefaultOnly(nil, now) || !MediaDeliveryDefaultOnly(&base, time.Time{}) {
		t.Fatal("missing state or clock must fail closed")
	}
}
