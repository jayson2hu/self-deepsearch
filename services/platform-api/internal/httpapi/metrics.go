package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/observability"
)

type metricKey struct {
	Method      string
	Route       string
	StatusClass string
}

type metricValue struct {
	Count                uint64
	DurationSeconds      float64
	DurationBucketCounts [requestDurationBucketCount]uint64
}

const requestDurationBucketCount = 10

var requestDurationBuckets = [requestDurationBucketCount]time.Duration{
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	300 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
}

type requestMetrics struct {
	mu      sync.Mutex
	started time.Time
	values  map[metricKey]metricValue
}

func newRequestMetrics(now time.Time) *requestMetrics {
	return &requestMetrics{started: now, values: make(map[metricKey]metricValue)}
}

func (metrics *requestMetrics) observe(method, route string, status int, duration time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	key := metricKey{Method: normalizedMetricMethod(method), Route: route, StatusClass: fmt.Sprintf("%dxx", status/100)}
	metrics.mu.Lock()
	value := metrics.values[key]
	value.Count++
	value.DurationSeconds += duration.Seconds()
	for index, upperBound := range requestDurationBuckets {
		if duration <= upperBound {
			value.DurationBucketCounts[index]++
		}
	}
	metrics.values[key] = value
	metrics.mu.Unlock()
}

func (metrics *requestMetrics) snapshot() []struct {
	Key   metricKey
	Value metricValue
} {
	metrics.mu.Lock()
	items := make([]struct {
		Key   metricKey
		Value metricValue
	}, 0, len(metrics.values))
	for key, value := range metrics.values {
		items = append(items, struct {
			Key   metricKey
			Value metricValue
		}{key, value})
	}
	metrics.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].Key, items[j].Key
		return left.Method+left.Route+left.StatusClass < right.Method+right.Route+right.StatusClass
	})
	return items
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r)
		s.metrics.observe(r.Method, routePatternFromContext(r.Context()), writer.status, time.Since(started))
	})
}

func (s *Server) prometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metricsToken == "" {
		http.NotFound(w, r)
		return
	}
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	expectedHash, providedHash := sha256.Sum256([]byte(s.metricsToken)), sha256.Sum256([]byte(provided))
	if provided == "" || subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, r, http.StatusUnauthorized, "METRICS_UNAUTHORIZED", "指标访问凭据无效")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_process_uptime_seconds Process uptime.\n# TYPE self_deepsearch_process_uptime_seconds gauge\nself_deepsearch_process_uptime_seconds %.3f\n", time.Since(s.metrics.started).Seconds())
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_build_info Build information.\n# TYPE self_deepsearch_build_info gauge\nself_deepsearch_build_info{service=%s,version=%s} 1\n", metricLabel(s.service), metricLabel(s.version))
	defaultOnly, policyAvailable := s.effectiveMediaDelivery(r.Context())
	mediaDefaultOnly := 0
	if defaultOnly {
		mediaDefaultOnly = 1
	}
	dynamicEnabled := 0
	if s.mediaDeliveryDynamic {
		dynamicEnabled = 1
	}
	edgePolicyEnabled := 0
	if s.mediaEdgePolicy {
		edgePolicyEnabled = 1
	}
	policyAvailableValue := 0
	if policyAvailable {
		policyAvailableValue = 1
	}
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_default_only Effective media delivery suppression, including manual and dynamic fail-closed policy.\n# TYPE self_deepsearch_media_default_only gauge\nself_deepsearch_media_default_only %d\n", mediaDefaultOnly)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_delivery_dynamic_enabled Dynamic usage-state media delivery enforcement is enabled.\n# TYPE self_deepsearch_media_delivery_dynamic_enabled gauge\nself_deepsearch_media_delivery_dynamic_enabled %d\n", dynamicEnabled)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_edge_policy_enabled Authenticated media edge policy endpoint is enabled.\n# TYPE self_deepsearch_media_edge_policy_enabled gauge\nself_deepsearch_media_edge_policy_enabled %d\n", edgePolicyEnabled)
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_delivery_policy_available All inputs required for the effective media delivery policy were available.\n# TYPE self_deepsearch_media_delivery_policy_available gauge\nself_deepsearch_media_delivery_policy_available %d\n", policyAvailableValue)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_http_requests_total HTTP requests by normalized route and status class.\n# TYPE self_deepsearch_http_requests_total counter\n")
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_http_request_duration_seconds Request duration histogram by normalized route and status class.\n# TYPE self_deepsearch_http_request_duration_seconds histogram\n")
	for _, item := range s.metrics.snapshot() {
		labels := fmt.Sprintf("method=%s,route=%s,status_class=%s", metricLabel(item.Key.Method), metricLabel(item.Key.Route), metricLabel(item.Key.StatusClass))
		_, _ = fmt.Fprintf(w, "self_deepsearch_http_requests_total{%s} %d\n", labels, item.Value.Count)
		for index, upperBound := range requestDurationBuckets {
			_, _ = fmt.Fprintf(w, "self_deepsearch_http_request_duration_seconds_bucket{%s,le=%s} %d\n",
				labels, metricLabel(strconv.FormatFloat(upperBound.Seconds(), 'g', -1, 64)), item.Value.DurationBucketCounts[index])
		}
		_, _ = fmt.Fprintf(w, "self_deepsearch_http_request_duration_seconds_bucket{%s,le=%s} %d\n", labels, metricLabel("+Inf"), item.Value.Count)
		_, _ = fmt.Fprintf(w, "self_deepsearch_http_request_duration_seconds_sum{%s} %.6f\n", labels, item.Value.DurationSeconds)
		_, _ = fmt.Fprintf(w, "self_deepsearch_http_request_duration_seconds_count{%s} %d\n", labels, item.Value.Count)
	}
	s.writePasswordWorkMetrics(w)
	s.writeDependencyMetrics(w, r)
}

func (s *Server) writePasswordWorkMetrics(w http.ResponseWriter) {
	provider, ok := s.identity.(interface {
		PasswordWorkStats() identity.PasswordWorkStats
	})
	if !ok {
		return
	}
	stats := provider.PasswordWorkStats()
	for _, item := range []struct {
		name, help, kind string
		value            float64
	}{
		{"active", "Password calculations holding a permit.", "gauge", float64(stats.Active)},
		{"waiting", "Password calculations waiting for a permit.", "gauge", float64(stats.Waiting)},
		{"active_limit", "Process password calculation concurrency limit.", "gauge", float64(stats.ActiveLimit)},
		{"waiting_limit", "Process password calculation queue limit.", "gauge", float64(stats.WaitingLimit)},
		{"wait_timeout_seconds", "Maximum password calculation queue wait.", "gauge", stats.WaitTimeoutSeconds},
		{"rejected_total", "Password calculations rejected by queue capacity or timeout.", "counter", float64(stats.Rejected)},
		{"canceled_total", "Password requests canceled before or during calculation.", "counter", float64(stats.Canceled)},
		{"completed_total", "Password calculations finished, including discarded canceled results.", "counter", float64(stats.Completed)},
	} {
		name := "self_deepsearch_password_work_" + item.name
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, item.help, name, item.kind, name, item.value)
	}
}

func normalizedMetricMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func (s *Server) writeDependencyMetrics(w http.ResponseWriter, r *http.Request) {
	databaseReady := 0
	if s.database != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		if s.database.Ping(ctx) == nil {
			databaseReady = 1
		}
		cancel()
	}
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_database_ready Database readiness.\n# TYPE self_deepsearch_database_ready gauge\nself_deepsearch_database_ready %d\n", databaseReady)
	s.writeDatabasePoolMetrics(w)
	if databaseReady == 1 {
		s.writeEmailDeliveryMetrics(w, r)
	}
	if s.operations == nil || databaseReady == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	health, err := s.operations.SystemHealth(ctx, s.now().UTC())
	cancel()
	if err != nil {
		return
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_schema_version Applied database schema version.\n# TYPE self_deepsearch_schema_version gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_schema_version %d\n", health.SchemaVersion)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_review_tasks_pending Pending or claimed review tasks.\n# TYPE self_deepsearch_review_tasks_pending gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_review_tasks_pending %d\n", health.PendingReviewTasks)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_revisions_reviewing Revisions awaiting a review decision.\n# TYPE self_deepsearch_revisions_reviewing gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_revisions_reviewing %d\n", health.ReviewingRevisions)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_outbox_pending Outbox events awaiting successful delivery.\n# TYPE self_deepsearch_outbox_pending gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_outbox_pending %d\n", health.PendingOutboxEvents)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_outbox_dead Outbox events that exhausted retries.\n# TYPE self_deepsearch_outbox_dead gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_outbox_dead %d\n", health.FailedOutboxEvents)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_bytes Bytes in media objects not yet physically deleted.\n# TYPE self_deepsearch_media_bytes gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_media_bytes %d\n", health.MediaBytes)
	if health.VerifiedBackupAgeSeconds != nil {
		_, _ = fmt.Fprint(w, "# HELP self_deepsearch_verified_backup_age_seconds Age of the latest verified backup.\n# TYPE self_deepsearch_verified_backup_age_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "self_deepsearch_verified_backup_age_seconds %.0f\n", *health.VerifiedBackupAgeSeconds)
	}
	if health.MediaReconcileAgeSeconds != nil {
		_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_reconcile_age_seconds Age of the latest completed media reconciliation.\n# TYPE self_deepsearch_media_reconcile_age_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "self_deepsearch_media_reconcile_age_seconds %.0f\n", *health.MediaReconcileAgeSeconds)
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_reconcile_issues Issues in the latest completed media reconciliation.\n# TYPE self_deepsearch_media_reconcile_issues gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_media_reconcile_issues %d\n", health.MediaReconcileIssues)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_reconcile_failures_24h Failed media reconciliation runs in the last 24 hours.\n# TYPE self_deepsearch_media_reconcile_failures_24h gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_media_reconcile_failures_24h %d\n", health.FailedMediaReconciles)
	if health.MediaInspectionAgeSeconds != nil {
		_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_inspection_age_seconds Age of the latest completed publication and default image inspection.\n# TYPE self_deepsearch_media_inspection_age_seconds gauge\n")
		_, _ = fmt.Fprintf(w, "self_deepsearch_media_inspection_age_seconds %.0f\n", *health.MediaInspectionAgeSeconds)
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_publication_issues Inconsistent objects or published links in the latest inspection.\n# TYPE self_deepsearch_media_publication_issues gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_media_publication_issues %d\n", health.MediaPublicationIssues)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_default_image_failures Failed fixed default image probes in the latest inspection.\n# TYPE self_deepsearch_default_image_failures gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_default_image_failures %d\n", health.DefaultImageFailures)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_media_inspection_failures_24h Failed inspection runs in the last 24 hours.\n# TYPE self_deepsearch_media_inspection_failures_24h gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_media_inspection_failures_24h %d\n", health.FailedMediaInspections)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_search_requests_24h Successful first-page searches in the last 24 hours.\n# TYPE self_deepsearch_search_requests_24h gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_search_requests_24h %d\n", health.SearchRequests24h)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_search_zero_results_24h Successful first-page searches with no results in the last 24 hours.\n# TYPE self_deepsearch_search_zero_results_24h gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_search_zero_results_24h %d\n", health.SearchZeroResults24h)
	zeroRate := 0.0
	if health.SearchRequests24h > 0 {
		zeroRate = float64(health.SearchZeroResults24h) / float64(health.SearchRequests24h)
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_search_zero_result_ratio_24h Fraction of successful first-page searches with no results.\n# TYPE self_deepsearch_search_zero_result_ratio_24h gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_search_zero_result_ratio_24h %.6f\n", zeroRate)
}

func (s *Server) writeDatabasePoolMetrics(w http.ResponseWriter) {
	provider, ok := s.database.(observability.DatabasePoolStatsProvider)
	if !ok {
		return
	}
	stats := provider.DatabasePoolStats()
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_connections PostgreSQL pool connections by state.\n# TYPE self_deepsearch_database_pool_connections gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_connections{state=%s} %d\n", metricLabel("acquired"), stats.AcquiredConnections)
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_connections{state=%s} %d\n", metricLabel("idle"), stats.IdleConnections)
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_connections{state=%s} %d\n", metricLabel("total"), stats.TotalConnections)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_max_connections Configured PostgreSQL pool connection limit.\n# TYPE self_deepsearch_database_pool_max_connections gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_max_connections %d\n", stats.MaxConnections)
	utilization := 0.0
	if stats.MaxConnections > 0 {
		utilization = float64(stats.AcquiredConnections) / float64(stats.MaxConnections)
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_utilization_ratio Fraction of the PostgreSQL pool currently acquired.\n# TYPE self_deepsearch_database_pool_utilization_ratio gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_utilization_ratio %.6f\n", utilization)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_acquire_total Successful PostgreSQL pool acquisitions.\n# TYPE self_deepsearch_database_pool_acquire_total counter\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_acquire_total %d\n", stats.AcquireCount)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_empty_acquire_total PostgreSQL acquisitions that initially found no idle connection.\n# TYPE self_deepsearch_database_pool_empty_acquire_total counter\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_empty_acquire_total %d\n", stats.EmptyAcquireCount)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_database_pool_canceled_acquire_total PostgreSQL pool acquisitions canceled by context.\n# TYPE self_deepsearch_database_pool_canceled_acquire_total counter\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_database_pool_canceled_acquire_total %d\n", stats.CanceledAcquireCount)
}

func (s *Server) writeEmailDeliveryMetrics(w http.ResponseWriter, r *http.Request) {
	if s.emailMetrics == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	metrics, err := s.emailMetrics.EmailDeliveryMetrics(ctx, s.now().UTC())
	cancel()
	if err != nil {
		return
	}

	counts := make(map[string]int64, len(metrics.Counts))
	for _, count := range metrics.Counts {
		counts[count.Purpose+"\x00"+count.Outcome] = count.Count
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_delivery_total Terminal email delivery outcomes by purpose.\n# TYPE self_deepsearch_email_delivery_total counter\n")
	for _, purpose := range []string{"signup", "password_reset", "account_close", "invitation"} {
		for _, outcome := range []string{"sent", "failed", "suppressed"} {
			_, _ = fmt.Fprintf(w, "self_deepsearch_email_delivery_total{purpose=%s,outcome=%s} %d\n",
				metricLabel(purpose), metricLabel(outcome), counts[purpose+"\x00"+outcome])
		}
	}
	usageRatio := float64(metrics.DailyReserved) / float64(s.emailDailyLimit)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_daily_reserved Emails reserved against today's UTC quota.\n# TYPE self_deepsearch_email_daily_reserved gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_daily_reserved %d\n", metrics.DailyReserved)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_daily_sent Emails sent from today's UTC quota.\n# TYPE self_deepsearch_email_daily_sent gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_daily_sent %d\n", metrics.DailySent)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_daily_failed Failed sends from today's UTC quota.\n# TYPE self_deepsearch_email_daily_failed gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_daily_failed %d\n", metrics.DailyFailed)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_daily_limit Configured UTC daily email quota.\n# TYPE self_deepsearch_email_daily_limit gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_daily_limit %d\n", s.emailDailyLimit)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_daily_usage_ratio Fraction of today's UTC email quota already reserved.\n# TYPE self_deepsearch_email_daily_usage_ratio gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_daily_usage_ratio %.6f\n", usageRatio)
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_consecutive_failures Consecutive provider send failures.\n# TYPE self_deepsearch_email_consecutive_failures gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_consecutive_failures %d\n", metrics.ConsecutiveFailure)
	circuitOpen := 0
	if metrics.CircuitOpen {
		circuitOpen = 1
	}
	_, _ = fmt.Fprint(w, "# HELP self_deepsearch_email_circuit_open Whether email delivery is currently suppressed by the circuit breaker.\n# TYPE self_deepsearch_email_circuit_open gauge\n")
	_, _ = fmt.Fprintf(w, "self_deepsearch_email_circuit_open %d\n", circuitOpen)
}

func metricLabel(value string) string {
	return strconv.Quote(value)
}
