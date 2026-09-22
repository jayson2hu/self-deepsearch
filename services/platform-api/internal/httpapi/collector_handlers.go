package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"self-deepsearch/services/platform-api/internal/collectoringest"
)

func (s *Server) ingestPerformers(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, code string) { writeError(w, r, status, code, "采集批次未接收") }
	c := s.collectorCredentials
	if !c.Valid() || isTypedNil(s.collector) {
		fail(404, "COLLECTOR_DISABLED")
		return
	}
	// Credentials and integrity metadata must have a single interpretation across
	// the client, any proxy, and this handler. Header.Get otherwise ignores extras.
	authorization := r.Header.Values("Authorization")
	supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	wantHash, gotHash := sha256.Sum256([]byte(c.Token)), sha256.Sum256([]byte(supplied))
	if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") || subtle.ConstantTimeCompare(wantHash[:], gotHash[:]) != 1 {
		fail(401, "COLLECTOR_UNAUTHORIZED")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" || len(r.Header.Values("Content-Encoding")) > 1 || r.Header.Get("Content-Encoding") != "" {
		fail(415, "COLLECTOR_MEDIA_TYPE")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, collectoringest.MaxBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			fail(413, "COLLECTOR_TOO_LARGE")
		} else {
			fail(400, "COLLECTOR_INVALID")
		}
		return
	}
	b, err := collectoringest.Decode(raw, s.now())
	if err != nil {
		fail(400, "COLLECTOR_INVALID")
		return
	}
	if b.SourceID != c.SourceID {
		fail(403, "COLLECTOR_SOURCE_FORBIDDEN")
		return
	}
	hash := collectoringest.Hash(raw)
	if len(r.Header.Values("X-Content-SHA256")) != 1 || r.Header.Get("X-Content-SHA256") != hash {
		fail(400, "COLLECTOR_HASH_MISMATCH")
		return
	}
	receipt, replay, err := s.collector.IngestPerformers(r.Context(), b, hash)
	switch {
	case errors.Is(err, collectoringest.ErrConflict):
		fail(409, "COLLECTOR_CONFLICT")
	case errors.Is(err, collectoringest.ErrSource):
		fail(403, "COLLECTOR_SOURCE_UNAVAILABLE")
	case errors.Is(err, collectoringest.ErrInvalid):
		fail(400, "COLLECTOR_INVALID")
	case err != nil:
		fail(503, "COLLECTOR_UNAVAILABLE")
	default:
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		writeJSON(w, status, receipt)
	}
}
