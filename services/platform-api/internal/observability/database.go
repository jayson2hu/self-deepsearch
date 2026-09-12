package observability

// DatabasePoolStats is a transport-neutral snapshot of the API PostgreSQL pool.
// It intentionally contains only bounded numeric values suitable for metrics.
type DatabasePoolStats struct {
	AcquiredConnections  int32
	IdleConnections      int32
	TotalConnections     int32
	MaxConnections       int32
	AcquireCount         int64
	EmptyAcquireCount    int64
	CanceledAcquireCount int64
}

type DatabasePoolStatsProvider interface {
	DatabasePoolStats() DatabasePoolStats
}
