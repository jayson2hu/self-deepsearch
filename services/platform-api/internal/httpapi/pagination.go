package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"self-deepsearch/services/platform-api/internal/catalog"
)

const (
	publicPageSize = 24
)

type pageCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	After   string `json:"a"`
}

type rankedSearchCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Rank    int    `json:"r"`
	Name    string `json:"n"`
	ID      string `json:"i"`
}

type relatedWorksCursor struct {
	Version     int    `json:"v"`
	Scope       string `json:"s"`
	ReleaseDate string `json:"d"`
	PublishedAt string `json:"p"`
	ID          string `json:"i"`
}

func readPageCursor(w http.ResponseWriter, r *http.Request, scope string) (string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		return "", true
	}
	if len(raw) > 256 {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return "", false
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor pageCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Scope != scope || !uuidPattern.MatchString(cursor.After) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return "", false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return "", false
	}
	return strings.ToLower(cursor.After), true
}

type cursorValue interface{ CursorID() string }

func pageValues[T cursorValue](values []T, scope string) ([]T, *string) {
	if len(values) <= publicPageSize {
		return values, nil
	}
	page := values[:publicPageSize]
	encoded, _ := json.Marshal(pageCursor{Version: 1, Scope: scope, After: page[len(page)-1].CursorID()})
	next := base64.RawURLEncoding.EncodeToString(encoded)
	return page, &next
}

func searchCursorScope(query string) string {
	digest := sha256.Sum256([]byte(query))
	return "works.search." + hex.EncodeToString(digest[:8])
}

func entitySearchCursorScope(entityType, query string) string {
	digest := sha256.Sum256([]byte(query))
	return entityType + ".search." + hex.EncodeToString(digest[:8])
}

func relatedWorksCursorScope(entityType, slug string) string {
	digest := sha256.Sum256([]byte(entityType + "\x00" + slug))
	return entityType + ".works." + hex.EncodeToString(digest[:8])
}

func readRelatedWorksCursor(w http.ResponseWriter, r *http.Request, scope string) (*catalog.RelatedWorkCursor, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		return nil, true
	}
	if len(raw) > 256 {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor relatedWorksCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Scope != scope || !uuidPattern.MatchString(cursor.ID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	publishedAt, err := time.Parse(time.RFC3339Nano, cursor.PublishedAt)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	var releaseDate *time.Time
	if cursor.ReleaseDate != "" {
		parsed, err := time.Parse("2006-01-02", cursor.ReleaseDate)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
			return nil, false
		}
		releaseDate = &parsed
	}
	return &catalog.RelatedWorkCursor{ReleaseDate: releaseDate, PublishedAt: publishedAt, ID: strings.ToLower(cursor.ID)}, true
}

type relatedCursorValue interface {
	RelatedCursor() catalog.RelatedWorkCursor
}

func pageRelatedWorks[T relatedCursorValue](values []T, scope string) ([]T, *string) {
	if len(values) <= publicPageSize {
		return values, nil
	}
	page := values[:publicPageSize]
	last := page[len(page)-1].RelatedCursor()
	releaseDate := ""
	if last.ReleaseDate != nil {
		releaseDate = last.ReleaseDate.UTC().Format("2006-01-02")
	}
	encoded, _ := json.Marshal(relatedWorksCursor{
		Version: 1, Scope: scope, ReleaseDate: releaseDate,
		PublishedAt: last.PublishedAt.UTC().Format(time.RFC3339Nano), ID: last.ID,
	})
	next := base64.RawURLEncoding.EncodeToString(encoded)
	return page, &next
}

func readRankedSearchCursor(w http.ResponseWriter, r *http.Request, scope string) (*catalog.SearchCursor, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		return nil, true
	}
	if len(raw) > 1024 {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor rankedSearchCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Scope != scope || cursor.Rank < 0 || cursor.Rank > 3 ||
		cursor.Name == "" || utf8.RuneCountInString(cursor.Name) > 500 || !uuidPattern.MatchString(cursor.ID) {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, r, http.StatusBadRequest, "INVALID_CURSOR", "分页游标不符合要求")
		return nil, false
	}
	return &catalog.SearchCursor{Rank: cursor.Rank, Name: cursor.Name, ID: strings.ToLower(cursor.ID)}, true
}

type rankedCursorValue interface {
	RankedCursor() catalog.SearchCursor
}

func pageRankedSearchValues[T rankedCursorValue](values []T, scope string) ([]T, *string) {
	if len(values) <= publicPageSize {
		return values, nil
	}
	page := values[:publicPageSize]
	last := page[len(page)-1].RankedCursor()
	encoded, _ := json.Marshal(rankedSearchCursor{
		Version: 1, Scope: scope, Rank: last.Rank, Name: last.Name, ID: last.ID,
	})
	next := base64.RawURLEncoding.EncodeToString(encoded)
	return page, &next
}
