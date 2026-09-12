package mediausage

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testSchedule() Schedule {
	return Schedule{testAccount, testNow.Add(-time.Hour), testNow.Add(24 * time.Hour), 15 * time.Minute, DefaultPolicy()}
}

func TestScheduleIdentityAndValidation(t *testing.T) {
	s := testSchedule()
	if s.Validate() != nil {
		t.Fatal("valid schedule rejected")
	}
	copy := s
	copy.AccountID = strings.ToUpper(copy.AccountID)
	copy.PeriodStart = copy.PeriodStart.In(time.FixedZone("Shanghai", 8*3600))
	copy.PeriodEnd = copy.PeriodEnd.In(time.FixedZone("Shanghai", 8*3600))
	if s.Fingerprint() != copy.Fingerprint() || s.AccountRef() != copy.AccountRef() {
		t.Fatal("restart identity changed with representation")
	}
	if strings.Contains(s.Fingerprint()+s.PolicyDocument(), testAccount) {
		t.Fatal("account ID must not appear in the policy evidence")
	}
	for _, change := range []func(*Schedule){func(s *Schedule) { s.AccountID = "" }, func(s *Schedule) { s.PeriodStart = time.Time{} }, func(s *Schedule) { s.PeriodEnd = s.PeriodStart }, func(s *Schedule) { s.PeriodEnd = s.PeriodStart.Add(retention + time.Second) }, func(s *Schedule) { s.Every = 4 * time.Minute }, func(s *Schedule) { s.Every = 16 * time.Minute }, func(s *Schedule) { s.Policy.MaxAge = 10 * time.Minute }, func(s *Schedule) { s.PeriodStart = s.PeriodStart.Add(time.Nanosecond) }} {
		copy := s
		change(&copy)
		if copy.Validate() == nil {
			t.Fatal("bad schedule accepted")
		}
	}
	for _, change := range []func(*Schedule){func(s *Schedule) { s.AccountID = strings.Repeat("a", 32) }, func(s *Schedule) { s.PeriodStart = s.PeriodStart.Add(-time.Hour) }, func(s *Schedule) { s.PeriodEnd = s.PeriodEnd.Add(time.Hour) }, func(s *Schedule) { s.Policy.ClassAReserve++ }, func(s *Schedule) { s.Every = 5 * time.Minute }, func(s *Schedule) { s.Policy.MaxAge = 40 * time.Minute }} {
		copy := s
		change(&copy)
		if copy.Fingerprint() == s.Fingerprint() {
			t.Fatal("meaningful policy change retained identity")
		}
	}
	// Expiry is a persisted review condition, not a reason to stop unrelated
	// worker jobs during startup configuration validation.
	copy = s
	copy.PeriodEnd = testNow.Add(-time.Minute)
	if copy.Validate() != nil {
		t.Fatal("structurally valid old period should be handled by the scheduler")
	}
}

func TestDurableFailureRecoveryNeverClearsReview(t *testing.T) {
	s := testSchedule()
	o := observation()
	o.Counts = Counts{100, 200, 3}
	first, accepted := Advance(State{}, o, nil, s, testNow)
	if accepted == nil || first.HighWater != o.Counts || first.ReviewRequired || first.EffectiveRecommendation(testNow, s) != "observe_only" {
		t.Fatal(first)
	}
	failed, accepted := Advance(first, nil, errors.New(testToken), s, testNow.Add(time.Minute))
	if accepted != nil || !failed.ReviewRequired || failed.ReviewReason != "usage_unavailable" || failed.HighWater != first.HighWater || failed.LastSuccessAt != first.LastSuccessAt {
		t.Fatal(failed)
	}
	o2 := *o
	o2.ReceivedAt = testNow.Add(2 * time.Minute)
	o2.Window.EndInclusive = o2.ReceivedAt
	o2.Counts.ClassA++
	recovered, accepted := Advance(failed, &o2, nil, s, o2.ReceivedAt)
	if accepted == nil || !recovered.ReviewRequired || recovered.ReviewSince != failed.ReviewSince || recovered.ReviewReason != failed.ReviewReason || recovered.LastAssessment.Status != "low_estimate" || recovered.EffectiveRecommendation(o2.ReceivedAt, s) != "hold_for_review" {
		t.Fatal(recovered)
	}
	accepted.Counts.ClassA = 0
	if recovered.HighWater.ClassA == 0 || o2.Counts.ClassA == 0 {
		t.Fatal("returned evidence aliases input/state")
	}
	encoded, _ := json.Marshal(recovered)
	var restarted State
	if json.Unmarshal(encoded, &restarted) != nil || restarted.EffectiveRecommendation(o2.ReceivedAt, s) != "hold_for_review" {
		t.Fatal("restart forgot review")
	}
}

func TestCounterAndWindowRegressionHoldLastValidEvidence(t *testing.T) {
	s := testSchedule()
	o := observation()
	o.Counts = Counts{100, 200, 3}
	previous, _ := Advance(State{}, o, nil, s, testNow)
	for _, tc := range []struct {
		name   string
		change func(*Observation)
	}{
		{"class-a", func(o *Observation) { o.Counts.ClassA-- }},
		{"class-b", func(o *Observation) { o.Counts.ClassB-- }},
		{"free", func(o *Observation) { o.Counts.Free-- }},
		{"window", func(o *Observation) { o.Window.EndInclusive = o.Window.EndInclusive.Add(-time.Second) }},
		{"receive", func(o *Observation) {
			o.ReceivedAt = o.ReceivedAt.Add(-time.Second)
			o.Window.EndInclusive = o.Window.EndInclusive.Add(-time.Second)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *o
			tc.change(&copy)
			next, accepted := Advance(previous, &copy, nil, s, testNow)
			if accepted != nil || !next.ReviewRequired || next.HighWater != previous.HighWater || next.LastSuccessAt != previous.LastSuccessAt || next.LastUntil != previous.LastUntil {
				t.Fatal(next)
			}
		})
	}
}

func TestStopLatchCannotBeLoweredByFreshSampleOrNewPeriod(t *testing.T) {
	s := testSchedule()
	o := observation()
	o.Counts.ClassA = 950000
	stopped, accepted := Advance(State{}, o, nil, s, testNow)
	if accepted == nil || !stopped.StopRecommended || !stopped.ReviewRequired || stopped.ReviewReason != "operations_reserve_reached" {
		t.Fatal(stopped)
	}
	low := *o
	low.Counts.ClassA = 1
	next, _ := Advance(stopped, &low, nil, s, testNow)
	if !next.StopRecommended || next.EffectiveRecommendation(testNow, s) != "stop_media_review_required" {
		t.Fatal("low sample cleared stop")
	}
	next, _ = Advance(stopped, o, nil, s, s.PeriodEnd)
	if !next.StopRecommended || next.LastAssessment.Reason != "usage_period_inactive" || next.HighWater != stopped.HighWater {
		t.Fatal(next)
	}
	if next.ReviewReason != stopped.ReviewReason || next.ReviewSince != stopped.ReviewSince {
		t.Fatal("first review evidence overwritten")
	}
}

func TestAdvanceRejectsUnboundObservations(t *testing.T) {
	s := testSchedule()
	for _, change := range []func(*Observation){func(o *Observation) { o.BoundaryPolicy = "" }, func(o *Observation) { o.Segments = 2 }, func(o *Observation) { o.Window.Start = o.Window.Start.Add(-time.Hour) }, func(o *Observation) { o.Window.EndInclusive = testNow.Add(-31 * time.Minute) }} {
		o := observation()
		change(o)
		next, accepted := Advance(State{}, o, nil, s, testNow)
		if accepted != nil || !next.ReviewRequired || !next.LastSuccessAt.IsZero() {
			t.Fatal(next)
		}
	}
	bad := s
	bad.Policy.ClassALimit = 0
	if next, _ := Advance(State{}, observation(), nil, bad, testNow); next.LastAssessment.Reason != "policy_invalid" {
		t.Fatal(next)
	}
	if next, _ := Advance(State{}, observation(), nil, s, s.PeriodStart.Add(-time.Second)); next.LastAssessment.Reason != "usage_period_inactive" {
		t.Fatal(next)
	}
}

func TestReadTimeStalenessCannotReuseStoredLowEstimate(t *testing.T) {
	s := testSchedule()
	state, _ := Advance(State{}, observation(), nil, s, testNow)
	for _, now := range []time.Time{testNow.Add(30*time.Minute + time.Second), testNow.Add(-time.Second), s.PeriodEnd} {
		if state.EffectiveRecommendation(now, s) != "hold_for_review" {
			t.Fatal("stale/future/expired state allowed observation")
		}
	}
	if state.ReviewRequired {
		t.Fatal("read-time check must not pretend a DB mutation happened")
	}
	if (State{}).EffectiveRecommendation(testNow, s) != "hold_for_review" {
		t.Fatal("empty state permitted")
	}
	if state.EffectiveRecommendation(testNow.Add(30*time.Minute), s) != "observe_only" {
		t.Fatal("freshness boundary rejected")
	}
}
