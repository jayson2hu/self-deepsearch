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
)

const (
	publicPageSize = 24
)

type pageCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	After   string `json:"a"`
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
