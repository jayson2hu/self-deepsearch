package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"self-deepsearch/services/platform-api/internal/catalog"
)

type performerDetailResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	NameOriginal   *string         `json:"name_original"`
	RomanizedName  *string         `json:"romanized_name"`
	Aliases        []string        `json:"aliases"`
	ActivityStatus string          `json:"activity_status"`
	Agency         *string         `json:"agency"`
	BirthYear      *int            `json:"birth_year"`
	HeightCM       *int            `json:"height_cm"`
	Measurements   *string         `json:"measurements"`
	DebutYear      *int            `json:"debut_year"`
	ImageURL       *string         `json:"image_url"`
	Works          []homeItem      `json:"works"`
	Images         []imageResponse `json:"images"`
}

func (s *Server) performerList(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	afterID, ok := readPageCursor(w, r, "performers.list")
	if !ok {
		return
	}
	items, err := s.catalog.ListPerformers(r.Context(), afterID, publicPageSize+1)
	if err != nil {
		s.writeCatalogError(w, r, "list_performers", err)
		return
	}
	page, nextCursor := pageValues(items, "performers.list")
	s.writeMediaJSON(w, r, http.StatusOK, itemListResponse{Items: performerItems(page), NextCursor: nextCursor})
}

func (s *Server) performerDetail(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "资料服务暂时不可用")
		return
	}
	slug := chi.URLParam(r, "slug")
	if !slugPattern.MatchString(slug) {
		writeError(w, r, http.StatusNotFound, "PERFORMER_NOT_FOUND", "未找到该人物资料")
		return
	}
	detail, err := s.catalog.PerformerBySlug(r.Context(), slug)
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "PERFORMER_NOT_FOUND", "未找到该人物资料")
		return
	}
	if err != nil {
		s.writeCatalogError(w, r, "query_performer_detail", err)
		return
	}
	response := performerDetailResponse{
		ID: detail.ID, Name: detail.Name, NameOriginal: detail.NameOriginal, RomanizedName: detail.RomanizedName, Aliases: detail.Aliases,
		ActivityStatus: detail.ActivityStatus, Agency: detail.Agency, BirthYear: detail.BirthYear,
		HeightCM: detail.HeightCM, Measurements: detail.Measurements, DebutYear: detail.DebutYear,
		ImageURL: detail.ImageURL, Works: workItems(detail.Works), Images: make([]imageResponse, 0, len(detail.Images)),
	}
	for _, image := range detail.Images {
		response.Images = append(response.Images, *imageResponseFromCatalog(&image))
	}
	s.writeMediaJSON(w, r, http.StatusOK, response)
}
