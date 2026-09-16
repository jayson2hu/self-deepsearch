package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func (s *Server) performerWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	s.relatedWorks(w, r, "performer", "PERFORMER_NOT_FOUND", "未找到该人物资料", s.catalog.PerformerWorksBySlug)
}

func (s *Server) studioWorks(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	s.relatedWorks(w, r, "studio", "STUDIO_NOT_FOUND", "未找到该厂牌资料", s.catalog.StudioWorksBySlug)
}

func (s *Server) relatedWorks(
	w http.ResponseWriter,
	r *http.Request,
	entityType string,
	notFoundCode string,
	notFoundMessage string,
	query func(context.Context, string, *catalog.RelatedWorkCursor, int) ([]catalog.RelatedWorkResult, error),
) {
	slug := chi.URLParam(r, "slug")
	if !slugPattern.MatchString(slug) {
		writeError(w, r, http.StatusNotFound, notFoundCode, notFoundMessage)
		return
	}
	scope := relatedWorksCursorScope(entityType, slug)
	after, ok := readRelatedWorksCursor(w, r, scope)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := query(ctx, slug, after, publicPageSize+1)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, notFoundCode, notFoundMessage)
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_"+entityType+"_works", err)
		return
	}
	page, nextCursor := pageRelatedWorks(items, scope)
	works := make([]catalog.WorkSummary, 0, len(page))
	for _, item := range page {
		works = append(works, item.WorkSummary)
	}
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: workItems(works), NextCursor: nextCursor})
}
