package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/logsafe"
	"self-deepsearch/services/platform-api/internal/operations"
)

var adminSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type reviewTaskResponse struct {
	ID          string  `json:"id"`
	TaskType    string  `json:"task_type"`
	EntityType  *string `json:"entity_type"`
	EntityID    *string `json:"entity_id"`
	Priority    int     `json:"priority"`
	Status      string  `json:"status"`
	AssigneeID  *string `json:"assignee_id"`
	Assignee    *string `json:"assignee"`
	TargetLabel string  `json:"target_label"`
	DueAt       *string `json:"due_at"`
	ClaimedAt   *string `json:"claimed_at"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type reviewTaskListResponse struct {
	Items []reviewTaskResponse `json:"items"`
}

type reviewDecisionRequest struct {
	Reason string `json:"reason"`
}

type reviewReassignmentRequest struct {
	AssigneeID string `json:"assignee_id"`
	Reason     string `json:"reason"`
}

type conflictReviewResponse struct {
	ID             string  `json:"id"`
	ReviewTaskID   string  `json:"review_task_id"`
	EntityType     string  `json:"entity_type"`
	EntityID       string  `json:"entity_id"`
	FieldName      string  `json:"field_name"`
	CurrentValue   any     `json:"current_value"`
	CandidateValue any     `json:"candidate_value"`
	SourceID       *string `json:"source_id"`
	SourceName     *string `json:"source_name"`
	SourcePriority *int    `json:"source_priority"`
	Resolution     *string `json:"resolution"`
	ResolvedBy     *string `json:"resolved_by"`
	ResolvedAt     *string `json:"resolved_at"`
	CreatedAt      string  `json:"created_at"`
}

type conflictReviewListResponse struct {
	Items []conflictReviewResponse `json:"items"`
}

type resolveConflictRequest struct {
	Resolution string `json:"resolution"`
	Reason     string `json:"reason"`
}

type createWorkRequest struct {
	Code          string                  `json:"code"`
	Title         string                  `json:"title"`
	TitleOriginal *string                 `json:"title_original"`
	ReleaseDate   *string                 `json:"release_date"`
	StudioID      *string                 `json:"studio_id"`
	Summary       *string                 `json:"summary"`
	PerformerIDs  []string                `json:"performer_ids"`
	Sources       []sourceEvidenceRequest `json:"sources"`
	Reason        string                  `json:"reason"`
}

type sourceEvidenceRequest struct {
	SourceType  string  `json:"source_type"`
	SourceURL   *string `json:"source_url"`
	SourceTitle *string `json:"source_title"`
	CheckedAt   string  `json:"checked_at"`
}

type createStudioRequest struct {
	Name          string                  `json:"name"`
	CanonicalSlug string                  `json:"canonical_slug"`
	Sources       []sourceEvidenceRequest `json:"sources"`
	Reason        string                  `json:"reason"`
}

type createPerformerRequest struct {
	DisplayName    string                  `json:"display_name"`
	NameOriginal   *string                 `json:"name_original"`
	RomanizedName  *string                 `json:"romanized_name"`
	Aliases        []string                `json:"aliases"`
	AdultStatus    string                  `json:"adult_status"`
	ActivityStatus string                  `json:"activity_status"`
	Agency         *string                 `json:"agency"`
	BirthYear      *int                    `json:"birth_year"`
	HeightCM       *int                    `json:"height_cm"`
	Measurements   *string                 `json:"measurements"`
	DebutYear      *int                    `json:"debut_year"`
	Sources        []sourceEvidenceRequest `json:"sources"`
	Reason         string                  `json:"reason"`
}

type createRevisionRequest struct {
	EntityType string                  `json:"entity_type"`
	Payload    map[string]any          `json:"payload"`
	Sources    []sourceEvidenceRequest `json:"sources"`
	Reason     string                  `json:"reason"`
}

type publishEntityRequest struct {
	EntityType    string `json:"entity_type"`
	RevisionID    string `json:"revision_id"`
	CanonicalSlug string `json:"canonical_slug"`
	Reason        string `json:"reason"`
}

type hideEntityRequest struct {
	EntityType string `json:"entity_type"`
	Reason     string `json:"reason"`
}

type createTakedownRequest struct {
	RequesterReference string  `json:"requester_reference"`
	EntityType         string  `json:"entity_type"`
	EntityID           string  `json:"entity_id"`
	EvidenceReference  *string `json:"evidence_reference"`
}

type completeTakedownRequest struct {
	ResultSummary string `json:"result_summary"`
}

type takedownResponse struct {
	ID                 string  `json:"id"`
	RequesterReference string  `json:"requester_reference"`
	EntityType         string  `json:"entity_type"`
	EntityID           string  `json:"entity_id"`
	EvidenceReference  *string `json:"evidence_reference"`
	Status             string  `json:"status"`
	AssignedTo         *string `json:"assigned_to"`
	ReceivedAt         string  `json:"received_at"`
	DecidedAt          *string `json:"decided_at"`
	CompletedAt        *string `json:"completed_at"`
	ResultSummary      *string `json:"result_summary"`
	UpdatedAt          string  `json:"updated_at"`
}

type takedownListResponse struct {
	Items []takedownResponse `json:"items"`
}

type entityResponse struct {
	ID             string `json:"id"`
	EntityType     string `json:"entity_type"`
	CanonicalSlug  string `json:"canonical_slug,omitempty"`
	Status         string `json:"status"`
	RevisionID     string `json:"revision_id"`
	RevisionStatus string `json:"revision_status"`
	CreatedAt      string `json:"created_at"`
}

type catalogEntityResponse struct {
	ID                string  `json:"id"`
	EntityType        string  `json:"entity_type"`
	Label             string  `json:"label"`
	Status            string  `json:"status"`
	CanonicalSlug     *string `json:"canonical_slug"`
	CurrentRevisionID *string `json:"current_revision_id"`
	UpdatedAt         string  `json:"updated_at"`
}

type catalogEntityListResponse struct {
	Items []catalogEntityResponse `json:"items"`
}

type revisionResponse struct {
	ID            string                      `json:"id"`
	EntityType    string                      `json:"entity_type"`
	EntityID      string                      `json:"entity_id"`
	Version       int                         `json:"version"`
	Payload       map[string]any              `json:"payload"`
	Status        string                      `json:"status"`
	Author        string                      `json:"author"`
	Reviewer      *string                     `json:"reviewer"`
	Reason        string                      `json:"reason"`
	CreatedAt     string                      `json:"created_at"`
	ReviewedAt    *string                     `json:"reviewed_at"`
	Sources       []operations.SourceEvidence `json:"sources"`
	IsCurrent     bool                        `json:"is_current"`
	CanonicalSlug *string                     `json:"canonical_slug"`
}

type revisionListResponse struct {
	Items []revisionResponse `json:"items"`
}

type adminUserResponse struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	Role          string  `json:"role"`
	AccountStatus string  `json:"account_status"`
	CreatedAt     string  `json:"created_at"`
	LastLoginAt   *string `json:"last_login_at"`
}

type adminUserListResponse struct {
	Items []adminUserResponse `json:"items"`
}

type roleChangeRequest struct {
	Role string `json:"role"`
}

type userStatusChangeRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type createInvitationRequest struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

type revokeInvitationRequest struct {
	Reason string `json:"reason"`
}

type invitationResponse struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	Status     string  `json:"status"`
	InvitedBy  string  `json:"invited_by"`
	Inviter    string  `json:"inviter"`
	SentAt     string  `json:"sent_at"`
	ExpiresAt  string  `json:"expires_at"`
	ConsumedAt *string `json:"consumed_at"`
	CreatedAt  string  `json:"created_at"`
}

type invitationListResponse struct {
	Items []invitationResponse `json:"items"`
}

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

func (s *Server) adminListReviewTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "open"
	}
	if !oneOf(status, "open", "pending", "claimed", "approved", "rejected", "cancelled") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "审核状态不符合要求")
		return
	}
	items, err := s.operations.ListReviewTasks(r.Context(), status, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := reviewTaskListResponse{Items: make([]reviewTaskResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, reviewTaskItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminListConflicts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "open"
	}
	if !oneOf(status, "open", "resolved", "all") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "冲突状态不符合要求")
		return
	}
	items, err := s.operations.ListConflictReviews(r.Context(), status, 200)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := conflictReviewListResponse{Items: make([]conflictReviewResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, conflictReviewItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminResolveConflict(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	conflictID := chi.URLParam(r, "conflictID")
	if !uuidPattern.MatchString(conflictID) {
		writeError(w, r, http.StatusNotFound, "CONFLICT_NOT_FOUND", "未找到该冲突记录")
		return
	}
	var request resolveConflictRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Resolution = strings.TrimSpace(request.Resolution)
	request.Reason = strings.TrimSpace(request.Reason)
	if !oneOf(request.Resolution, "keep_current", "accept_candidate", "mark_unknown", "merge", "reject") ||
		len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_RESOLUTION", "裁决类型或理由不符合要求")
		return
	}
	item, err := s.operations.ResolveConflictReview(r.Context(), conflictID, request.Resolution, user.ID,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, conflictReviewItem(item))
}

func (s *Server) adminClaimReviewTask(w http.ResponseWriter, r *http.Request) {
	// A claimed task is locked to the operator who will make the decision.
	// Editors may prepare and submit revisions, but cannot take a review lock
	// they are not authorized to complete.
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	item, err := s.operations.ClaimReviewTask(r.Context(), taskID, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func (s *Server) adminReassignReviewTask(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	var request reviewReassignmentRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.AssigneeID = strings.TrimSpace(request.AssigneeID)
	request.Reason = strings.TrimSpace(request.Reason)
	if !uuidPattern.MatchString(request.AssigneeID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ASSIGNEE", "请选择有效的目标审核人")
		return
	}
	if len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请填写 2 至 1000 个字符的改派理由")
		return
	}
	item, err := s.operations.ReassignReviewTask(r.Context(), taskID, request.AssigneeID, user.ID,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func (s *Server) adminApproveReviewTask(w http.ResponseWriter, r *http.Request) {
	s.adminDecideReviewTask(w, r, true)
}

func (s *Server) adminRejectReviewTask(w http.ResponseWriter, r *http.Request) {
	s.adminDecideReviewTask(w, r, false)
}

func (s *Server) adminDecideReviewTask(w http.ResponseWriter, r *http.Request, approve bool) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if !uuidPattern.MatchString(taskID) {
		writeError(w, r, http.StatusNotFound, "REVIEW_TASK_NOT_FOUND", "未找到审核任务")
		return
	}
	var request reviewDecisionRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请填写 2 至 1000 个字符的审核理由")
		return
	}
	item, err := s.operations.DecideReviewTask(r.Context(), taskID, user.ID, approve, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, reviewTaskItem(item))
}

func (s *Server) adminCreateWork(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request createWorkRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Code, request.Title, request.Reason = strings.TrimSpace(request.Code), strings.TrimSpace(request.Title), strings.TrimSpace(request.Reason)
	now := s.now().UTC()
	sources, sourcesOK := normalizeSourceEvidence(request.Sources, now)
	if len(request.Code) < 1 || len(request.Code) > 100 || len(request.Title) < 1 || len(request.Title) > 500 ||
		len(request.Reason) < 2 || len(request.Reason) > 1000 || !sourcesOK {
		writeError(w, r, http.StatusBadRequest, "INVALID_WORK", "作品番号、标题或录入理由不符合要求")
		return
	}
	if request.StudioID != nil && !uuidPattern.MatchString(*request.StudioID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_WORK", "厂牌 ID 不符合要求")
		return
	}
	if len(request.PerformerIDs) > 20 || !validUniqueUUIDs(request.PerformerIDs) {
		writeError(w, r, http.StatusBadRequest, "INVALID_WORK", "人物 ID 必须是最多 20 个不重复 UUID")
		return
	}
	var releaseDate *time.Time
	if request.ReleaseDate != nil && *request.ReleaseDate != "" {
		value, err := time.Parse(time.DateOnly, *request.ReleaseDate)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_WORK", "实际发行日期必须使用 YYYY-MM-DD")
			return
		}
		releaseDate = &value
	}
	item, err := s.operations.CreateWork(r.Context(), operations.WorkInput{
		Code: request.Code, Title: request.Title, TitleOriginal: cleanOptional(request.TitleOriginal),
		ReleaseDate: releaseDate, StudioID: request.StudioID, Summary: cleanOptional(request.Summary), PerformerIDs: request.PerformerIDs,
		Sources: sources, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), now)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, entityItem(item))
}

func (s *Server) adminCreateStudio(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request createStudioRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.CanonicalSlug = strings.TrimSpace(request.CanonicalSlug)
	request.Reason = strings.TrimSpace(request.Reason)
	now := s.now().UTC()
	sources, sourcesOK := normalizeSourceEvidence(request.Sources, now)
	if len(request.Name) < 1 || len(request.Name) > 200 || !adminSlugPattern.MatchString(request.CanonicalSlug) ||
		len(request.CanonicalSlug) > 200 || len(request.Reason) < 2 || len(request.Reason) > 1000 || !sourcesOK {
		writeError(w, r, http.StatusBadRequest, "INVALID_STUDIO", "厂牌名称、slug 或录入理由不符合要求")
		return
	}
	item, err := s.operations.CreateStudio(r.Context(), operations.StudioInput{Name: request.Name,
		CanonicalSlug: request.CanonicalSlug, Sources: sources, Reason: request.Reason}, user.ID, requestIDFromContext(r.Context()), now)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, entityItem(item))
}

func (s *Server) adminCreatePerformer(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request createPerformerRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.DisplayName, request.Reason = strings.TrimSpace(request.DisplayName), strings.TrimSpace(request.Reason)
	now := s.now().UTC()
	sources, sourcesOK := normalizeSourceEvidence(request.Sources, now)
	aliases, aliasesOK := normalizePerformerAliases(request.Aliases)
	if request.AdultStatus == "" {
		request.AdultStatus = "unverified"
	}
	if request.ActivityStatus == "" {
		request.ActivityStatus = "unknown"
	}
	if len(request.DisplayName) < 1 || len(request.DisplayName) > 200 || len(request.Reason) < 2 || len(request.Reason) > 1000 || !aliasesOK || !sourcesOK ||
		!oneOf(request.AdultStatus, "unverified", "verified", "restricted") || !oneOf(request.ActivityStatus, "active", "retired", "unknown") ||
		!validOptionalRange(request.BirthYear, 1900, 2200) || !validOptionalRange(request.HeightCM, 100, 250) || !validOptionalRange(request.DebutYear, 1900, 2200) {
		writeError(w, r, http.StatusBadRequest, "INVALID_PERFORMER", "人物资料不符合要求")
		return
	}
	item, err := s.operations.CreatePerformer(r.Context(), operations.PerformerInput{
		DisplayName: request.DisplayName, NameOriginal: cleanOptional(request.NameOriginal), RomanizedName: cleanOptional(request.RomanizedName),
		Aliases:     aliases,
		AdultStatus: request.AdultStatus, ActivityStatus: request.ActivityStatus, Agency: cleanOptional(request.Agency),
		BirthYear: request.BirthYear, HeightCM: request.HeightCM, Measurements: cleanOptional(request.Measurements), DebutYear: request.DebutYear,
		Sources: sources, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), now)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, entityItem(item))
}

func (s *Server) adminCreateRevision(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request createRevisionRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	now := s.now().UTC()
	sources, sourcesOK := normalizeSourceEvidence(request.Sources, now)
	if request.EntityType == "work" {
		if code, ok := request.Payload["canonical_code"].(string); ok {
			request.Payload["canonical_code"] = strings.TrimSpace(code)
			request.Payload["compact_code"] = compactCode(code)
		}
	}
	if request.EntityType == "studio" {
		if name, ok := request.Payload["name"].(string); ok {
			name = strings.TrimSpace(name)
			request.Payload["name"] = name
			request.Payload["normalized_name"] = strings.ToLower(name)
		}
	}
	aliasesOK := request.EntityType != "performer" || normalizePerformerAliasRevisionPayload(request.Payload)
	if !validEntityType(request.EntityType) || len(request.Payload) == 0 || len(request.Payload) > 24 ||
		len(request.Reason) < 2 || len(request.Reason) > 1000 || !sourcesOK || !aliasesOK || !validRevisionPayload(request.EntityType, request.Payload) ||
		!validRevisionPayloadValues(request.EntityType, request.Payload) {
		writeError(w, r, http.StatusBadRequest, "INVALID_REVISION", "修订内容不符合要求")
		return
	}
	item, err := s.operations.CreateRevision(r.Context(), entityID, operations.RevisionInput{
		EntityType: request.EntityType, Payload: request.Payload, Sources: sources, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), now)
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, entityItem(item))
}

func (s *Server) adminPublishEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request publishEntityRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.CanonicalSlug, request.Reason = strings.TrimSpace(request.CanonicalSlug), strings.TrimSpace(request.Reason)
	if !validEntityType(request.EntityType) || !adminSlugPattern.MatchString(request.CanonicalSlug) || len(request.CanonicalSlug) > 200 ||
		(request.RevisionID != "" && !uuidPattern.MatchString(request.RevisionID)) || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_PUBLICATION", "发布参数不符合要求")
		return
	}
	item, err := s.operations.PublishEntity(r.Context(), entityID, operations.PublishInput{
		EntityType: request.EntityType, RevisionID: request.RevisionID, CanonicalSlug: request.CanonicalSlug, Reason: request.Reason,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, entityItem(item))
}

func (s *Server) adminHideEntity(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	var request hideEntityRequest
	if !uuidPattern.MatchString(entityID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(entityID) {
			writeError(w, r, http.StatusNotFound, "ENTITY_NOT_FOUND", "未找到资料")
		}
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if !validEntityType(request.EntityType) || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_HIDE_REQUEST", "隐藏参数不符合要求")
		return
	}
	item, err := s.operations.HideEntity(r.Context(), entityID, request.EntityType, user.ID, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, entityItem(item))
}

func (s *Server) adminListEntities(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if entityType == "" {
		entityType = "all"
	}
	if status == "" {
		status = "all"
	}
	if !oneOf(entityType, "all", "work", "performer", "studio") ||
		!oneOf(status, "all", "draft", "reviewing", "approved", "published", "hidden", "takedown", "merged") {
		writeError(w, r, http.StatusBadRequest, "INVALID_CATALOG_FILTER", "资料筛选条件不符合要求")
		return
	}
	items, err := s.operations.ListEntities(r.Context(), entityType, status, 200)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := catalogEntityListResponse{Items: make([]catalogEntityResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, catalogEntityResponse{ID: item.ID, EntityType: item.EntityType,
			Label: item.Label, Status: item.Status, CanonicalSlug: item.CanonicalSlug,
			CurrentRevisionID: item.CurrentRevisionID, UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminListRevisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	entityID := chi.URLParam(r, "entityID")
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	if !uuidPattern.MatchString(entityID) || !validEntityType(entityType) {
		writeError(w, r, http.StatusBadRequest, "INVALID_ENTITY", "资料类型或 ID 不符合要求")
		return
	}
	items, err := s.operations.ListRevisions(r.Context(), entityID, entityType, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := revisionListResponse{Items: make([]revisionResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, revisionResponse{ID: item.ID, EntityType: item.EntityType,
			EntityID: item.EntityID, Version: item.Version, Payload: item.Payload, Status: item.Status,
			Author: item.Author, Reviewer: item.Reviewer, Reason: item.Reason,
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), ReviewedAt: formatRFC3339(item.ReviewedAt),
			Sources: item.Sources, IsCurrent: item.IsCurrent, CanonicalSlug: item.CanonicalSlug})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminListTakedowns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "received", "validating", "approved", "rejected", "completed") {
		writeError(w, r, http.StatusBadRequest, "INVALID_STATUS", "下架请求状态不符合要求")
		return
	}
	items, err := s.operations.ListTakedownRequests(r.Context(), status, 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := takedownListResponse{Items: make([]takedownResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, takedownItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateTakedown(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	var request createTakedownRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.RequesterReference = strings.TrimSpace(request.RequesterReference)
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.EntityID = strings.TrimSpace(request.EntityID)
	request.EvidenceReference = cleanOptional(request.EvidenceReference)
	if len(request.RequesterReference) < 1 || len(request.RequesterReference) > 500 ||
		!oneOf(request.EntityType, "work", "performer", "studio", "media") ||
		!uuidPattern.MatchString(request.EntityID) ||
		(request.EvidenceReference != nil && len(*request.EvidenceReference) > 2000) {
		writeError(w, r, http.StatusBadRequest, "INVALID_TAKEDOWN", "权利下架登记信息不符合要求")
		return
	}
	item, err := s.operations.CreateTakedownRequest(r.Context(), operations.TakedownInput{
		RequesterReference: request.RequesterReference, EntityType: request.EntityType,
		EntityID: request.EntityID, EvidenceReference: request.EvidenceReference,
	}, user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, takedownItem(item))
}

func (s *Server) adminCompleteTakedown(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	takedownID := chi.URLParam(r, "takedownID")
	if !uuidPattern.MatchString(takedownID) {
		writeError(w, r, http.StatusNotFound, "TAKEDOWN_NOT_FOUND", "未找到权利下架请求")
		return
	}
	var request completeTakedownRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.ResultSummary = strings.TrimSpace(request.ResultSummary)
	if len(request.ResultSummary) < 2 || len(request.ResultSummary) > 2000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_TAKEDOWN_RESULT", "请填写 2 至 2000 个字符的处理结论")
		return
	}
	item, err := s.operations.CompleteTakedownRequest(r.Context(), takedownID, user.ID,
		request.ResultSummary, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, takedownItem(item))
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	items, err := s.operations.ListUsers(r.Context(), 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := adminUserListResponse{Items: make([]adminUserResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, adminUserItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminChangeUserRole(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	userID := chi.URLParam(r, "userID")
	var request roleChangeRequest
	if !uuidPattern.MatchString(userID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(userID) {
			writeError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "未找到用户")
		}
		return
	}
	if !oneOf(request.Role, "admin", "editor", "user") {
		writeError(w, r, http.StatusBadRequest, "INVALID_ROLE", "角色不符合要求")
		return
	}
	item, err := s.operations.ChangeUserRole(r.Context(), userID, request.Role, user.ID,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserItem(item))
}

func (s *Server) adminChangeUserStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	userID := chi.URLParam(r, "userID")
	var request userStatusChangeRequest
	if !uuidPattern.MatchString(userID) || !s.decodeJSON(w, r, &request) {
		if !uuidPattern.MatchString(userID) {
			writeError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "未找到用户")
		}
		return
	}
	request.Status, request.Reason = strings.TrimSpace(request.Status), strings.TrimSpace(request.Reason)
	if !oneOf(request.Status, "active", "locked", "suspended") || len(request.Reason) < 2 || len(request.Reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_USER_STATUS", "账号状态参数不符合要求")
		return
	}
	item, err := s.operations.ChangeUserStatus(r.Context(), userID, request.Status, user.ID, user.Role,
		request.Reason, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, adminUserItem(item))
}

func (s *Server) adminListInvitations(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "all"
	}
	if !oneOf(status, "all", "pending", "accepted", "revoked", "expired") {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION_FILTER", "邀请筛选条件不符合要求")
		return
	}
	items, err := s.identity.ListInvitations(r.Context(), status, 100)
	if s.writeIdentityError(w, r, err) {
		return
	}
	response := invitationListResponse{Items: make([]invitationResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, invitationItem(item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) adminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	var request createInvitationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Email, request.Role, request.Reason = strings.TrimSpace(request.Email), strings.TrimSpace(request.Role), strings.TrimSpace(request.Reason)
	if !oneOf(request.Role, "admin", "editor", "user") || len(request.Reason) < 2 || len(request.Reason) > 500 {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION", "邀请角色或理由不符合要求")
		return
	}
	item, err := s.identity.CreateInvitation(r.Context(), request.Email, request.Role, request.Reason, actor.ID, actor.Role,
		requestIDFromContext(r.Context()), s.remoteIP(r))
	if s.writeIdentityError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, invitationItem(item))
}

func (s *Server) adminRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil {
		writeError(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "账号服务暂时不可用")
		return
	}
	actor, ok := s.requireAdminRole(w, r, "admin", "owner")
	if !ok {
		return
	}
	invitationID := chi.URLParam(r, "invitationID")
	if !uuidPattern.MatchString(invitationID) {
		writeError(w, r, http.StatusNotFound, "INVITATION_NOT_FOUND", "未找到邀请")
		return
	}
	var request revokeInvitationRequest
	if !s.decodeJSON(w, r, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) < 2 || len(request.Reason) > 500 {
		writeError(w, r, http.StatusBadRequest, "INVALID_INVITATION_REASON", "撤销理由不符合要求")
		return
	}
	item, err := s.identity.RevokeInvitation(r.Context(), invitationID, actor.ID, request.Reason,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeIdentityError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, invitationItem(item))
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

func (s *Server) requireAdminRole(w http.ResponseWriter, r *http.Request, roles ...string) (identity.User, bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return identity.User{}, false
	}
	if !oneOf(user.Role, roles...) {
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "当前账号无权执行此操作")
		return identity.User{}, false
	}
	return user, true
}

func (s *Server) requireOperations(w http.ResponseWriter, r *http.Request) bool {
	if s.operations == nil {
		writeError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "运营服务暂时不可用")
		return false
	}
	return true
}

func (s *Server) writeOperationsError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, operations.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "OPERATIONS_OBJECT_NOT_FOUND", "未找到运营对象")
	case errors.Is(err, operations.ErrConflict):
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "对象状态已变化，请刷新后重试")
	case errors.Is(err, operations.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "当前账号无权执行此操作")
	case errors.Is(err, operations.ErrInvalidInput):
		writeError(w, r, http.StatusBadRequest, "INVALID_INPUT", "提交的数据不符合要求")
	default:
		s.logger.ErrorContext(r.Context(), "operations_failed", "request_id", requestIDFromContext(r.Context()), "error_class", logsafe.ErrorClass(err))
		writeError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "运营服务暂时不可用")
	}
	return true
}

func reviewTaskItem(item operations.ReviewTask) reviewTaskResponse {
	return reviewTaskResponse{
		ID: item.ID, TaskType: item.TaskType, EntityType: item.EntityType, EntityID: item.EntityID,
		Priority: item.Priority, Status: item.Status, AssigneeID: item.AssigneeID, Assignee: item.Assignee,
		TargetLabel: item.TargetLabel, DueAt: formatRFC3339(item.DueAt), ClaimedAt: formatRFC3339(item.ClaimedAt),
		CompletedAt: formatRFC3339(item.CompletedAt), CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func entityItem(item operations.Entity) entityResponse {
	return entityResponse{ID: item.ID, EntityType: item.EntityType, CanonicalSlug: item.CanonicalSlug,
		Status: item.Status, RevisionID: item.RevisionID, RevisionStatus: item.RevisionStatus,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)}
}

func adminUserItem(item operations.User) adminUserResponse {
	return adminUserResponse{ID: item.ID, Email: item.Email, Role: item.Role, AccountStatus: item.AccountStatus,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), LastLoginAt: formatRFC3339(item.LastLoginAt)}
}

func invitationItem(item identity.Invitation) invitationResponse {
	return invitationResponse{
		ID: item.ID, Email: item.Email, Role: item.Role, Status: item.Status,
		InvitedBy: item.InvitedBy, Inviter: item.Inviter,
		SentAt: item.SentAt.UTC().Format(time.RFC3339), ExpiresAt: item.ExpiresAt.UTC().Format(time.RFC3339),
		ConsumedAt: formatRFC3339(item.ConsumedAt), CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func takedownItem(item operations.TakedownRequest) takedownResponse {
	return takedownResponse{
		ID: item.ID, RequesterReference: item.RequesterReference, EntityType: item.EntityType,
		EntityID: item.EntityID, EvidenceReference: item.EvidenceReference, Status: item.Status,
		AssignedTo: item.AssignedTo, ReceivedAt: item.ReceivedAt.UTC().Format(time.RFC3339),
		DecidedAt: formatRFC3339(item.DecidedAt), CompletedAt: formatRFC3339(item.CompletedAt),
		ResultSummary: item.ResultSummary, UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func conflictReviewItem(item operations.ConflictReview) conflictReviewResponse {
	return conflictReviewResponse{
		ID: item.ID, ReviewTaskID: item.ReviewTaskID, EntityType: item.EntityType, EntityID: item.EntityID,
		FieldName: item.FieldName, CurrentValue: item.CurrentValue, CandidateValue: item.CandidateValue,
		SourceID: item.SourceID, SourceName: item.SourceName, SourcePriority: item.SourcePriority,
		Resolution: item.Resolution, ResolvedBy: item.ResolvedBy, ResolvedAt: formatRFC3339(item.ResolvedAt),
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func formatRFC3339(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func validEntityType(value string) bool {
	return oneOf(value, "work", "performer", "studio")
}

func validUniqueUUIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !uuidPattern.MatchString(value) {
			return false
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			return false
		}
		seen[normalized] = struct{}{}
	}
	return true
}

func normalizePerformerAliases(values []string) ([]string, bool) {
	if len(values) > 30 {
		return nil, false
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if length := len([]rune(value)); length < 1 || length > 300 {
			return nil, false
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			return nil, false
		}
		seen[normalized] = struct{}{}
		result = append(result, value)
	}
	return result, true
}

func normalizeSourceEvidence(values []sourceEvidenceRequest, now time.Time) ([]operations.SourceEvidenceInput, bool) {
	if len(values) < 1 || len(values) > 5 {
		return nil, false
	}
	result := make([]operations.SourceEvidenceInput, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sourceType := strings.ToLower(strings.TrimSpace(value.SourceType))
		if !oneOf(sourceType, "web_page", "search_result", "official", "other") {
			return nil, false
		}
		sourceURL := cleanOptional(value.SourceURL)
		if sourceURL != nil {
			parsed, err := url.ParseRequestURI(*sourceURL)
			if err != nil || !oneOf(strings.ToLower(parsed.Scheme), "http", "https") || parsed.Host == "" || len([]rune(*sourceURL)) > 2000 {
				return nil, false
			}
		}
		sourceTitle := cleanOptional(value.SourceTitle)
		if sourceTitle != nil && len([]rune(*sourceTitle)) > 300 {
			return nil, false
		}
		checkedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value.CheckedAt))
		if err != nil || checkedAt.After(now.Add(5*time.Minute)) {
			return nil, false
		}
		checkedAt = checkedAt.UTC().Truncate(time.Second)
		key := sourceType + "\x00" + strings.ToLower(optionalStringValue(sourceURL)) + "\x00" + strings.ToLower(optionalStringValue(sourceTitle))
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		result = append(result, operations.SourceEvidenceInput{
			SourceType: sourceType, SourceURL: sourceURL, SourceTitle: sourceTitle, CheckedAt: checkedAt,
		})
	}
	return result, true
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizePerformerAliasRevisionPayload(payload map[string]any) bool {
	value, exists := payload["aliases"]
	if !exists {
		return true
	}
	aliases, ok := performerAliasesFromPayload(value)
	if !ok {
		return false
	}
	normalized := make([]any, len(aliases))
	for index, alias := range aliases {
		normalized[index] = alias
	}
	payload["aliases"] = normalized
	return true
}

func performerAliasesFromPayload(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		values = append(values, text)
	}
	return normalizePerformerAliases(values)
}

func validOptionalRange(value *int, minimum, maximum int) bool {
	return value == nil || (*value >= minimum && *value <= maximum)
}

func validRevisionPayload(entityType string, payload map[string]any) bool {
	allowed := map[string]map[string]bool{
		"work":      {"canonical_code": true, "compact_code": true, "title": true, "title_original": true, "release_date": true, "studio_id": true, "summary": true, "performer_ids": true},
		"performer": {"display_name": true, "name_original": true, "romanized_name": true, "aliases": true, "adult_status": true, "activity_status": true, "agency": true, "birth_year": true, "height_cm": true, "measurements": true, "debut_year": true},
		"studio":    {"name": true, "normalized_name": true},
	}[entityType]
	if allowed == nil {
		return false
	}
	for key, value := range payload {
		if !allowed[key] {
			return false
		}
		if value == nil {
			// A null patch explicitly clears an optional field. Required fields
			// remain non-null so a revision cannot make a published record
			// unusable. The database apply path already understands JSON null.
			return revisionFieldAllowsNull(entityType, key)
		}
		switch value.(type) {
		case string, float64:
		case []any:
			if !((entityType == "work" && key == "performer_ids") || (entityType == "performer" && key == "aliases")) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func revisionFieldAllowsNull(entityType, field string) bool {
	switch entityType {
	case "work":
		return oneOf(field, "title_original", "release_date", "studio_id", "summary", "performer_ids")
	case "performer":
		return oneOf(field, "name_original", "romanized_name", "agency", "birth_year", "height_cm", "measurements", "debut_year")
	default:
		return false
	}
}

func validRevisionPayloadValues(entityType string, payload map[string]any) bool {
	switch entityType {
	case "work":
		_, hasCode := payload["canonical_code"]
		_, hasCompactCode := payload["compact_code"]
		if hasCompactCode && !hasCode {
			return false
		}
		return validRequiredString(payload, "canonical_code", 100) && validRequiredString(payload, "compact_code", 100) &&
			validRequiredString(payload, "title", 500) && validOptionalString(payload, "title_original", 500) &&
			validOptionalString(payload, "summary", 5000) && validOptionalUUID(payload, "studio_id") &&
			validOptionalDate(payload, "release_date") && validPerformerIDPayload(payload)
	case "performer":
		return validRequiredString(payload, "display_name", 200) && validOptionalString(payload, "name_original", 200) &&
			validOptionalString(payload, "romanized_name", 200) && validOptionalString(payload, "agency", 200) &&
			validOptionalString(payload, "measurements", 200) && validOptionalEnum(payload, "adult_status", "unverified", "verified", "restricted") &&
			validOptionalEnum(payload, "activity_status", "active", "retired", "unknown") &&
			validOptionalNumber(payload, "birth_year", 1900, 2200) && validOptionalNumber(payload, "height_cm", 100, 250) &&
			validOptionalNumber(payload, "debut_year", 1900, 2200) && validPerformerAliasPayload(payload)
	case "studio":
		_, hasName := payload["name"]
		_, hasNormalizedName := payload["normalized_name"]
		if hasNormalizedName && !hasName {
			return false
		}
		return validRequiredString(payload, "name", 200) && validRequiredString(payload, "normalized_name", 200)
	default:
		return false
	}
}

func validRequiredString(payload map[string]any, key string, maximum int) bool {
	value, exists := payload[key]
	if !exists {
		return true
	}
	text, ok := value.(string)
	length := len([]rune(strings.TrimSpace(text)))
	return ok && length >= 1 && length <= maximum
}

func validOptionalString(payload map[string]any, key string, maximum int) bool {
	value, exists := payload[key]
	if !exists || value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && len([]rune(text)) <= maximum
}

func validOptionalUUID(payload map[string]any, key string) bool {
	value, exists := payload[key]
	if !exists || value == nil || value == "" {
		return true
	}
	text, ok := value.(string)
	return ok && uuidPattern.MatchString(text)
}

func validOptionalDate(payload map[string]any, key string) bool {
	value, exists := payload[key]
	if !exists || value == nil || value == "" {
		return true
	}
	text, ok := value.(string)
	if !ok {
		return false
	}
	_, err := time.Parse(time.DateOnly, text)
	return err == nil
}

func validOptionalEnum(payload map[string]any, key string, allowed ...string) bool {
	value, exists := payload[key]
	if !exists {
		return true
	}
	text, ok := value.(string)
	return ok && oneOf(text, allowed...)
}

func validOptionalNumber(payload map[string]any, key string, minimum, maximum int) bool {
	value, exists := payload[key]
	if !exists || value == nil {
		return true
	}
	number, ok := value.(float64)
	return ok && number == float64(int(number)) && int(number) >= minimum && int(number) <= maximum
}

func validPerformerIDPayload(payload map[string]any) bool {
	value, exists := payload["performer_ids"]
	if !exists {
		return true
	}
	if value == nil {
		return true
	}
	items, ok := value.([]any)
	if !ok || len(items) > 20 {
		return false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return false
		}
		values = append(values, text)
	}
	return validUniqueUUIDs(values)
}

func validPerformerAliasPayload(payload map[string]any) bool {
	value, exists := payload["aliases"]
	if !exists {
		return true
	}
	_, ok := performerAliasesFromPayload(value)
	return ok
}
