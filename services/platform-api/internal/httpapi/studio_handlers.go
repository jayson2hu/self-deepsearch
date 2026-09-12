package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/catalog"
)

type studioSummaryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Href string `json:"href"`
}

type studioListResponse struct {
	Items      []studioSummaryResponse `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

type studioDetailResponse struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Works []homeItem `json:"works"`
}

func (s *Server) studioList(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	afterID, ok := readPageCursor(w, r, "studios.list")
	if !ok {
		return
	}
	items, err := s.catalog.ListStudios(r.Context(), afterID, publicPageSize+1)
	if err != nil {
		s.writeCatalogError(w, r, "list_studios", err)
		return
	}
	page, nextCursor := pageValues(items, "studios.list")
	response := studioListResponse{Items: make([]studioSummaryResponse, 0, len(page)), NextCursor: nextCursor}
	for _, item := range page {
		response.Items = append(response.Items, studioSummaryResponse{ID: item.ID, Name: item.Name, Href: "/studios/" + item.Slug})
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}

func (s *Server) studioDetail(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	slug := chi.URLParam(r, "slug")
	if !slugPattern.MatchString(slug) {
		writeError(w, r, http.StatusNotFound, "STUDIO_NOT_FOUND", "未找到该厂牌资料")
		return
	}
	detail, err := s.catalog.StudioBySlug(r.Context(), slug)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "STUDIO_NOT_FOUND", "未找到该厂牌资料")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_studio_detail", err)
		return
	}
	s.writeMediaJSON(w, r, http.StatusOK, studioDetailResponse{ID: detail.ID, Name: detail.Name, Works: workItems(detail.Works)})
}
