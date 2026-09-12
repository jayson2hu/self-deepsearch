package mediausage

import "time"

type Policy struct {
	ClassALimit   uint64        `json:"class_a_limit"`
	ClassBLimit   uint64        `json:"class_b_limit"`
	ClassAReserve uint64        `json:"class_a_reserve"`
	ClassBReserve uint64        `json:"class_b_reserve"`
	MaxAge        time.Duration `json:"-"`
}

func DefaultPolicy() Policy {
	return Policy{ClassALimit: 1000000, ClassBLimit: 10000000, ClassAReserve: 50000, ClassBReserve: 500000, MaxAge: 30 * time.Minute}
}

func (p Policy) valid() bool {
	return p.ClassALimit > 0 && p.ClassBLimit > 0 && p.ClassAReserve > 0 && p.ClassBReserve > 0 &&
		p.ClassAReserve < p.ClassALimit && p.ClassBReserve < p.ClassBLimit && p.MaxAge > 0 && p.MaxAge <= time.Hour
}

type Assessment struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	// This is a recommendation for operations pressure, not a delivery command.
	Recommendation string `json:"recommendation"`
}

// Assess never recommends automatic recovery. ReceivedAt is retrieval time,
// and Window.EndInclusive is the requested horizon, NOT a provider completeness
// watermark. Even a low estimate cannot prove free-tier or storage headroom.
func Assess(o *Observation, fetchError error, expectedStart, now time.Time, p Policy) Assessment {
	hold := func(reason string) Assessment { return Assessment{"unknown", reason, "hold_for_review"} }
	if !p.valid() {
		return hold("policy_invalid")
	}
	if fetchError != nil {
		return hold(ErrorCode(fetchError))
	}
	if o == nil {
		return hold("usage_no_data")
	}
	if !o.Window.valid(now) || !o.Window.Start.Equal(expectedStart) || o.ReceivedAt.IsZero() ||
		o.ReceivedAt.After(now) || o.ReceivedAt.Before(o.Window.EndInclusive) || o.Segments < 1 || o.Segments > 31 {
		return hold("observation_invalid")
	}
	if now.Sub(o.ReceivedAt) > p.MaxAge || now.Sub(o.Window.EndInclusive) > p.MaxAge {
		return hold("observation_stale")
	}
	status := "low_estimate"
	recommendation := "observe_only"
	reason := "operations_below_warning"
	for _, budget := range [][3]uint64{{o.Counts.ClassA, p.ClassALimit, p.ClassAReserve}, {o.Counts.ClassB, p.ClassBLimit, p.ClassBReserve}} {
		used, limit, reserve := budget[0], budget[1], budget[2]
		if used >= limit || reserve >= limit-used {
			return Assessment{"stop_recommended", "operations_reserve_reached", "stop_media_review_required"}
		}
		if used >= percentageCeiling(limit, 85) {
			status, reason, recommendation = "high", "operations_at_85_percent", "pause_nonessential"
		} else if used >= percentageCeiling(limit, 70) && status == "low_estimate" {
			status, reason, recommendation = "warning", "operations_at_70_percent", "review_usage"
		}
	}
	return Assessment{status, reason, recommendation}
}

// Avoid float rounding and uint64 multiplication overflow at threshold edges.
func percentageCeiling(limit, percent uint64) uint64 {
	return limit/100*percent + (limit%100*percent+99)/100
}
