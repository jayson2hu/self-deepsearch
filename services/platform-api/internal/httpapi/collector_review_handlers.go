package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"self-deepsearch/services/platform-api/internal/collectorreview"
)

// Collector source IDs use the same UUID syntax accepted by the ingest contract.
var collectorReviewUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Server) requireCollectorReview(w http.ResponseWriter, r *http.Request) (collectorreview.Repository, bool) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok {
		return nil, false
	}
	repository, ok := s.collector.(collectorreview.Repository)
	if !ok || isTypedNil(repository) {
		writeError(w, r, http.StatusServiceUnavailable, "COLLECTOR_REVIEW_UNAVAILABLE", "采集验收服务暂时不可用")
		return nil, false
	}
	return repository, true
}

func (s *Server) adminListCollectorBatches(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.requireCollectorReview(w, r)
	if !ok {
		return
	}
	query, ok := readCollectorReviewQuery(w, r)
	if !ok {
		return
	}
	result, err := repository.ListCollectorBatches(r.Context(), query)
	if s.writeCollectorReviewError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminListCollectorCandidates(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.requireCollectorReview(w, r)
	if !ok {
		return
	}
	query, ok := readCollectorReviewQuery(w, r)
	if !ok {
		return
	}
	result, err := repository.ListCollectorCandidates(r.Context(), query)
	if s.writeCollectorReviewError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminCollectorCandidate(w http.ResponseWriter, r *http.Request) {
	repository, ok := s.requireCollectorReview(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "candidateID")
	if !collectorReviewUUIDPattern.MatchString(id) {
		s.writeCollectorReviewError(w, r, collectorreview.ErrNotFound)
		return
	}
	result, err := repository.CollectorCandidate(r.Context(), id)
	if s.writeCollectorReviewError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func readCollectorReviewQuery(w http.ResponseWriter, r *http.Request) (collectorreview.Query, bool) {
	values := r.URL.Query()
	limit, limitErr := strconv.Atoi(defaultString(values.Get("limit"), "25"))
	offset, offsetErr := strconv.Atoi(defaultString(values.Get("offset"), "0"))
	q := collectorreview.Query{SourceID: strings.ToLower(strings.TrimSpace(values.Get("source_id"))), BatchID: strings.ToLower(strings.TrimSpace(values.Get("batch_id"))),
		Status: strings.TrimSpace(values.Get("status")), Search: strings.TrimSpace(values.Get("q")), Limit: limit, Offset: offset}
	if limitErr != nil || offsetErr != nil || limit < 1 || limit > 100 || offset < 0 || offset > 1000000 ||
		(q.SourceID != "" && !collectorReviewUUIDPattern.MatchString(q.SourceID)) || (q.BatchID != "" && !collectorReviewUUIDPattern.MatchString(q.BatchID)) ||
		(q.Status != "" && !oneOf(q.Status, "pending", "valid", "invalid", "duplicate", "conflict", "reviewing", "accepted", "rejected")) || utf8.RuneCountInString(q.Search) > 200 || strings.ContainsRune(q.Search, 0) {
		writeError(w, r, http.StatusBadRequest, "INVALID_COLLECTOR_QUERY", "采集验收筛选参数不符合要求")
		return q, false
	}
	return q, true
}

func (s *Server) writeCollectorReviewError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, collectorreview.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "COLLECTOR_CANDIDATE_NOT_FOUND", "未找到采集候选资料")
	} else {
		writeError(w, r, http.StatusServiceUnavailable, "COLLECTOR_REVIEW_UNAVAILABLE", "采集验收服务暂时不可用")
	}
	return true
}
