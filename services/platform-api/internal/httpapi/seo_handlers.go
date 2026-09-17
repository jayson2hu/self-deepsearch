package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"self-deepsearch/services/platform-api/internal/operations"
)

type seoSummaryResponse struct {
	ObservedAt        string           `json:"observed_at"`
	Total             int64            `json:"total"`
	Indexable         int64            `json:"indexable"`
	Excluded          int64            `json:"excluded"`
	Unknown           int64            `json:"unknown"`
	SitemapCandidates int64            `json:"sitemap_candidates"`
	Published         int64            `json:"published"`
	Hidden            int64            `json:"hidden"`
	Takedown          int64            `json:"takedown"`
	ExclusionReasons  map[string]int64 `json:"exclusion_reasons"`
}

type seoPageResponse struct {
	EntityType            string  `json:"entity_type"`
	EntityID              string  `json:"entity_id"`
	URLPath               string  `json:"url_path"`
	Title                 string  `json:"title"`
	Description           string  `json:"description"`
	Canonical             string  `json:"canonical"`
	Robots                string  `json:"robots"`
	PublicationStatus     string  `json:"publication_status"`
	ContentClassification string  `json:"content_classification"`
	QualityStatus         string  `json:"quality_status"`
	MediaPolicy           string  `json:"media_policy"`
	Indexable             bool    `json:"indexable"`
	SitemapEligible       bool    `json:"sitemap_eligible"`
	ExclusionReason       *string `json:"exclusion_reason"`
	UpdatedAt             string  `json:"updated_at"`
	RevisionHref          string  `json:"revision_href"`
}

func (s *Server) adminSEOSummary(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.requireSEOInventory(w, r)
	if !ok {
		return
	}
	result, err := repository.SEOInventorySummary(r.Context(), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, seoSummaryResponse{ObservedAt: result.ObservedAt.Format(timeFormat), Total: result.Total,
		Indexable: result.Indexable, Excluded: result.Excluded, Unknown: result.Unknown, SitemapCandidates: result.SitemapCandidates,
		Published: result.Published, Hidden: result.Hidden, Takedown: result.Takedown, ExclusionReasons: result.Reasons})
}

func (s *Server) adminSEOPages(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.requireSEOInventory(w, r)
	if !ok {
		return
	}
	limit, limitErr := strconv.Atoi(defaultString(r.URL.Query().Get("limit"), "25"))
	offset, offsetErr := strconv.Atoi(defaultString(r.URL.Query().Get("offset"), "0"))
	entityType, status := strings.TrimSpace(r.URL.Query().Get("entity_type")), strings.TrimSpace(r.URL.Query().Get("status"))
	if limitErr != nil || offsetErr != nil || limit < 1 || limit > 100 || offset < 0 ||
		(entityType != "" && !oneOf(entityType, "work", "performer", "studio")) ||
		(status != "" && !oneOf(status, "published", "hidden", "takedown")) {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_QUERY", "SEO 页面筛选参数不符合要求")
		return
	}
	result, err := repository.ListSEOPages(r.Context(), operations.SEOPageQuery{EntityType: entityType, Status: status,
		Query: strings.TrimSpace(r.URL.Query().Get("q")), Limit: limit, Offset: offset})
	if s.writeOperationsError(w, r, err) {
		return
	}
	items := make([]seoPageResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, seoPageItem(item))
	}
	nextOffset := (*int)(nil)
	if int64(offset+len(items)) < result.Total {
		value := offset + len(items)
		nextOffset = &value
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": result.Total, "next_offset": nextOffset})
}

func (s *Server) requireSEOInventory(w http.ResponseWriter, r *http.Request) (operations.SEOInventoryRepository, bool) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return nil, false
	}
	repository, ok := s.operations.(operations.SEOInventoryRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "SEO_INVENTORY_UNAVAILABLE", "SEO 清单服务暂时不可用")
		return nil, false
	}
	return repository, true
}

func seoPageItem(item operations.SEOPage) seoPageResponse {
	return seoPageResponse{EntityType: item.EntityType, EntityID: item.EntityID, URLPath: item.URLPath,
		Title: item.Title, Description: item.Description, Canonical: item.Canonical, Robots: item.Robots,
		PublicationStatus: item.PublicationStatus, ContentClassification: item.ContentClassification,
		QualityStatus: item.QualityStatus, MediaPolicy: item.MediaPolicy, Indexable: item.Indexable,
		SitemapEligible: item.SitemapEligible, ExclusionReason: item.ExclusionReason,
		UpdatedAt: item.UpdatedAt.UTC().Format(timeFormat), RevisionHref: "/catalog/" + item.EntityType + "/" + item.EntityID}
}

const timeFormat = "2006-01-02T15:04:05Z07:00"
