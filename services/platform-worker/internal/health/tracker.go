package health

import (
	"sync"
	"time"
)

type WorkerSnapshot struct {
	StartedAt               time.Time
	LastHeartbeatAt         time.Time
	LastPollSuccessAt       time.Time
	LastSuccessAt           time.Time
	ActiveLeases            int
	PollsTotal              uint64
	PollFailuresTotal       uint64
	ConsecutivePollFailures uint64
	CompletedTotal          uint64
	FailedTotal             uint64
}

type TelemetryProvider interface {
	Snapshot() WorkerSnapshot
}

type Tracker struct {
	mu                      sync.RWMutex
	startedAt               time.Time
	lastHeartbeatAt         time.Time
	lastPollSuccessAt       time.Time
	lastSuccessAt           time.Time
	activeLeases            int
	pollsTotal              uint64
	pollFailuresTotal       uint64
	consecutivePollFailures uint64
	completedTotal          uint64
	failedTotal             uint64
}

func NewTracker(startedAt time.Time) *Tracker {
	return &Tracker{startedAt: startedAt.UTC()}
}

func (tracker *Tracker) Heartbeat(at time.Time) {
	tracker.mu.Lock()
	tracker.lastHeartbeatAt = at.UTC()
	tracker.pollsTotal++
	tracker.mu.Unlock()
}

func (tracker *Tracker) SetActiveLeases(count int) {
	if count < 0 {
		count = 0
	}
	tracker.mu.Lock()
	tracker.activeLeases = count
	tracker.mu.Unlock()
}

func (tracker *Tracker) PollSucceeded(at time.Time) {
	tracker.mu.Lock()
	tracker.lastPollSuccessAt = at.UTC()
	tracker.consecutivePollFailures = 0
	tracker.mu.Unlock()
}

func (tracker *Tracker) PollFailed(time.Time) {
	tracker.mu.Lock()
	tracker.pollFailuresTotal++
	tracker.consecutivePollFailures++
	tracker.mu.Unlock()
}

func (tracker *Tracker) EventCompleted(at time.Time) {
	tracker.mu.Lock()
	tracker.lastSuccessAt = at.UTC()
	tracker.completedTotal++
	tracker.mu.Unlock()
}

func (tracker *Tracker) EventFailed(time.Time) {
	tracker.mu.Lock()
	tracker.failedTotal++
	tracker.mu.Unlock()
}

func (tracker *Tracker) Snapshot() WorkerSnapshot {
	tracker.mu.RLock()
	snapshot := WorkerSnapshot{
		StartedAt:               tracker.startedAt,
		LastHeartbeatAt:         tracker.lastHeartbeatAt,
		LastPollSuccessAt:       tracker.lastPollSuccessAt,
		LastSuccessAt:           tracker.lastSuccessAt,
		ActiveLeases:            tracker.activeLeases,
		PollsTotal:              tracker.pollsTotal,
		PollFailuresTotal:       tracker.pollFailuresTotal,
		ConsecutivePollFailures: tracker.consecutivePollFailures,
		CompletedTotal:          tracker.completedTotal,
		FailedTotal:             tracker.failedTotal,
	}
	tracker.mu.RUnlock()
	return snapshot
}
