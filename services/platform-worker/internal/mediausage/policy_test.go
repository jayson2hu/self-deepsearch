package mediausage

import (
	"errors"
	"math"
	"testing"
	"time"
)

func observation() *Observation {
	return &Observation{Window: shortWindow(), ReceivedAt: testNow, Segments: 1, BoundaryPolicy: "inclusive_shared_endpoints"}
}

func TestAssessmentThresholdsAndReserves(t *testing.T) {
	cases := []struct {
		a, b   uint64
		status string
	}{
		{0, 0, "low_estimate"}, {699999, 6999999, "low_estimate"},
		{700000, 0, "warning"}, {0, 7000000, "warning"}, {849999, 8499999, "warning"},
		{850000, 0, "high"}, {0, 8500000, "high"}, {949999, 9499999, "high"},
		{950000, 0, "stop_recommended"}, {0, 9500000, "stop_recommended"},
		{1000000, 0, "stop_recommended"}, {0, math.MaxUint64, "stop_recommended"},
		{900000, 7500000, "high"}, {750000, 9000000, "high"},
	}
	for _, tc := range cases {
		o := observation()
		o.Counts = Counts{tc.a, tc.b, math.MaxUint64}
		a := Assess(o, nil, o.Window.Start, testNow, DefaultPolicy())
		if a.Status != tc.status || a.Recommendation == "resume" {
			t.Errorf("a=%d b=%d: %+v", tc.a, tc.b, a)
		}
	}
}

func TestAssessmentInvalidAndStale(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Observation)
	}{
		{"future_receive", func(o *Observation) { o.ReceivedAt = testNow.Add(time.Second) }},
		{"missing_receive", func(o *Observation) { o.ReceivedAt = time.Time{} }},
		{"receive_before_horizon", func(o *Observation) { o.ReceivedAt = testNow.Add(-time.Second) }},
		{"old_window_fresh_fetch", func(o *Observation) { o.Window.EndInclusive = testNow.Add(-31 * time.Minute) }},
		{"old_receive", func(o *Observation) {
			o.Window.EndInclusive = testNow.Add(-31 * time.Minute)
			o.ReceivedAt = o.Window.EndInclusive
		}},
		{"wrong_period", func(o *Observation) { o.Window.Start = o.Window.Start.Add(-time.Hour) }},
		{"future_window", func(o *Observation) { o.Window.EndInclusive = testNow.Add(time.Second) }},
		{"no_segments", func(o *Observation) { o.Segments = 0 }},
		{"too_many_segments", func(o *Observation) { o.Segments = 32 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := observation()
			tc.change(o)
			a := Assess(o, nil, shortWindow().Start, testNow, DefaultPolicy())
			if a.Status != "unknown" || a.Recommendation != "hold_for_review" {
				t.Fatalf("invalid observation used: %+v", a)
			}
		})
	}
	if a := Assess(nil, nil, shortWindow().Start, testNow, DefaultPolicy()); a.Status != "unknown" {
		t.Fatal(a)
	}
	if a := Assess(observation(), errors.New(testToken), shortWindow().Start, testNow, DefaultPolicy()); a.Reason != "usage_unavailable" {
		t.Fatal(a)
	}
	// Exactly the maximum age is allowed; one second older is not.
	o := observation()
	o.ReceivedAt = testNow.Add(-30 * time.Minute)
	o.Window.EndInclusive = o.ReceivedAt
	if a := Assess(o, nil, o.Window.Start, testNow, DefaultPolicy()); a.Status != "low_estimate" {
		t.Fatal(a)
	}
	if a := Assess(o, nil, o.Window.Start, testNow.Add(time.Second), DefaultPolicy()); a.Status != "unknown" {
		t.Fatal(a)
	}
}

func TestPolicyValidationAndIntegerEdges(t *testing.T) {
	for _, change := range []func(*Policy){
		func(p *Policy) { p.ClassALimit = 0 }, func(p *Policy) { p.ClassBReserve = p.ClassBLimit },
		func(p *Policy) { p.ClassAReserve = 0 }, func(p *Policy) { p.MaxAge = 0 }, func(p *Policy) { p.MaxAge = time.Hour + time.Second },
	} {
		p := DefaultPolicy()
		change(&p)
		if a := Assess(observation(), nil, shortWindow().Start, testNow, p); a.Reason != "policy_invalid" {
			t.Fatal(a)
		}
	}
	if percentageCeiling(101, 70) != 71 || percentageCeiling(101, 85) != 86 || percentageCeiling(math.MaxUint64, 85) != 15679732462653118873 {
		t.Fatal("integer threshold rounding/overflow")
	}
	p := DefaultPolicy()
	p.ClassALimit = math.MaxUint64
	p.ClassAReserve = 5
	o := observation()
	o.Counts.ClassA = math.MaxUint64 - 6
	if a := Assess(o, nil, o.Window.Start, testNow, p); a.Status != "high" {
		t.Fatal(a)
	}
	o.Counts.ClassA++
	if a := Assess(o, nil, o.Window.Start, testNow, p); a.Status != "stop_recommended" {
		t.Fatal(a)
	}
}
