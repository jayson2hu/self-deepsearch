package httpapi

import (
	"net/http"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

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

func entityItem(item operations.Entity) entityResponse {
	return entityResponse{ID: item.ID, EntityType: item.EntityType, CanonicalSlug: item.CanonicalSlug,
		Status: item.Status, RevisionID: item.RevisionID, RevisionStatus: item.RevisionStatus,
		CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)}
}
