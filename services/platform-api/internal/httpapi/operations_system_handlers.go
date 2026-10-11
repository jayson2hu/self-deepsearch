package httpapi

import (
	"net/http"
	"strings"
	"time"
)

type systemHealthResponse struct {
	Status                    string   `json:"status"`
	SchemaVersion             int      `json:"schema_version"`
	PendingReviewTasks        int64    `json:"pending_review_tasks"`
	ReviewingRevisions        int64    `json:"reviewing_revisions"`
	PendingOutboxEvents       int64    `json:"pending_outbox_events"`
	FailedOutboxEvents        int64    `json:"failed_outbox_events"`
	MediaBytes                int64    `json:"media_bytes"`
	VerifiedBackupAgeSeconds  *float64 `json:"verified_backup_age_seconds"`
	MediaReconcileAgeSeconds  *float64 `json:"media_reconcile_age_seconds"`
	MediaReconcileIssues      int64    `json:"media_reconcile_issues"`
	FailedMediaReconciles     int64    `json:"failed_media_reconciles"`
	MediaInspectionAgeSeconds *float64 `json:"media_inspection_age_seconds"`
	MediaPublicationIssues    int64    `json:"media_publication_issues"`
	DefaultImageFailures      int64    `json:"default_image_failures"`
	FailedMediaInspections    int64    `json:"failed_media_inspections"`
	SearchRequests24h         int64    `json:"search_requests_24h"`
	SearchZeroResults24h      int64    `json:"search_zero_results_24h"`
	CheckedAt                 string   `json:"checked_at"`
}

type auditLogResponse struct {
	ID         string  `json:"id"`
	ActorType  string  `json:"actor_type"`
	ActorID    *string `json:"actor_id"`
	Actor      *string `json:"actor"`
	Action     string  `json:"action"`
	ObjectType string  `json:"object_type"`
	ObjectID   *string `json:"object_id"`
	Reason     *string `json:"reason"`
	RequestID  string  `json:"request_id"`
	OccurredAt string  `json:"occurred_at"`
}

type auditLogListResponse struct {
	Items     []auditLogResponse `json:"items"`
	Truncated bool               `json:"truncated"`
}

func (s *Server) adminSystemHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	item, err := s.operations.SystemHealth(r.Context(), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	status := "ok"
	if item.FailedOutboxEvents > 0 || item.FailedMediaReconciles > 0 || item.MediaReconcileIssues > 0 ||
		item.MediaPublicationIssues > 0 || item.DefaultImageFailures > 0 || item.FailedMediaInspections > 0 ||
		item.MediaInspectionAgeSeconds == nil || *item.MediaInspectionAgeSeconds > 2*24*60*60 {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, systemHealthResponse{
		Status: status, SchemaVersion: item.SchemaVersion, PendingReviewTasks: item.PendingReviewTasks,
		ReviewingRevisions: item.ReviewingRevisions, PendingOutboxEvents: item.PendingOutboxEvents,
		FailedOutboxEvents: item.FailedOutboxEvents, MediaBytes: item.MediaBytes,
		VerifiedBackupAgeSeconds: item.VerifiedBackupAgeSeconds, MediaReconcileAgeSeconds: item.MediaReconcileAgeSeconds,
		MediaReconcileIssues: item.MediaReconcileIssues, FailedMediaReconciles: item.FailedMediaReconciles,
		MediaInspectionAgeSeconds: item.MediaInspectionAgeSeconds, MediaPublicationIssues: item.MediaPublicationIssues,
		DefaultImageFailures: item.DefaultImageFailures, FailedMediaInspections: item.FailedMediaInspections,
		SearchRequests24h: item.SearchRequests24h, SearchZeroResults24h: item.SearchZeroResults24h,
		CheckedAt: item.CheckedAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) adminListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	if !validRequestID(requestID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST_ID", "请输入有效的请求编号")
		return
	}
	items, err := s.operations.ListAuditLogs(r.Context(), requestID, 201)
	if s.writeOperationsError(w, r, err) {
		return
	}
	truncated := len(items) > 200
	if truncated {
		items = items[:200]
	}
	response := auditLogListResponse{Items: make([]auditLogResponse, 0, len(items)), Truncated: truncated}
	for _, item := range items {
		response.Items = append(response.Items, auditLogResponse{
			ID: item.ID, ActorType: item.ActorType, ActorID: item.ActorID, Actor: item.Actor,
			Action: item.Action, ObjectType: item.ObjectType, ObjectID: item.ObjectID, Reason: item.Reason,
			RequestID: item.RequestID, OccurredAt: item.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, response)
}
