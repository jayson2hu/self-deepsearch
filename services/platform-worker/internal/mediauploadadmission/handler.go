// Package mediauploadadmission exposes a private, fail-closed upload check.
// It consumes existing usage state; it cannot resume uploads or mutate reviews.
package mediauploadadmission

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const Path = "/v1/upload-admission"

var ErrConfig = errors.New("media_upload_admission_config_invalid")
var noncePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var timestampPattern = regexp.MustCompile(`^[0-9]{1,12}$`)

type Guard interface{ Allow(context.Context) error }

type Handler struct {
	secret                []byte
	guard                 Guard
	now                   func() time.Time
	active                chan struct{}
	allowed, denied, busy atomic.Uint64
}

func NewHandler(secret string, guard Guard, now func() time.Time) (*Handler, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 || len(secret) > 4096 || guard == nil {
		return nil, ErrConfig
	}
	if now == nil {
		now = time.Now
	}
	return &Handler{secret: []byte(secret), guard: guard, now: now, active: make(chan struct{}, 2)}, nil
}

func sign(secret []byte, prefix string, body []byte) string {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write(body)
	return "a1=" + hex.EncodeToString(h.Sum(nil))
}

func requestPrefix(timestamp, nonce string) string {
	return "sd-upload-admission-request-v1\nPOST\n" + Path + "\n" + timestamp + "\n" + nonce + "\n"
}

func responsePrefix(timestamp, nonce string, request []byte, status int) string {
	digest := sha256.Sum256(request)
	return "sd-upload-admission-response-v1\nPOST\n" + Path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(digest[:]) + "\n" + strconv.Itoa(status) + "\n"
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != Path || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for _, name := range []string{"Content-Type", "X-SD-Admission-Timestamp", "X-SD-Admission-Nonce", "X-SD-Admission-Signature"} {
		if len(r.Header.Values(name)) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	if r.ContentLength != 2 || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	timestamp, nonce := r.Header.Get("X-SD-Admission-Timestamp"), r.Header.Get("X-SD-Admission-Nonce")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	now := h.now().UTC()
	if err != nil || !timestampPattern.MatchString(timestamp) || !noncePattern.MatchString(nonce) || now.Sub(time.Unix(seconds, 0)).Abs() > 30*time.Second {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	body, err := io.ReadAll(io.LimitReader(r.Body, 3))
	if err != nil || !bytes.Equal(body, []byte(`{}`)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !hmac.Equal([]byte(r.Header.Get("X-SD-Admission-Signature")), []byte(sign(h.secret, requestPrefix(timestamp, nonce), body))) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	respond := func(status int, decision string) {
		ack, _ := json.Marshal(struct {
			Decision  string `json:"decision"`
			CheckedAt string `json:"checked_at"`
			Nonce     string `json:"request_nonce"`
		}{decision, h.now().UTC().Format("2006-01-02T15:04:05Z"), nonce})
		w.Header().Set("X-SD-Admission-Response", sign(h.secret, responsePrefix(timestamp, nonce, body, status), ack))
		w.Header().Set("Content-Length", strconv.Itoa(len(ack)))
		w.WriteHeader(status)
		_, _ = w.Write(ack)
	}
	select {
	case h.active <- struct{}{}:
		defer func() { <-h.active }()
	default:
		h.busy.Add(1)
		respond(http.StatusTooManyRequests, "deny")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	err = h.guard.Allow(ctx)
	// Recheck the clock and deadline after the bounded database read; no
	// positive response may outlive its request or a backward clock jump.
	finished := h.now().UTC()
	if err != nil || ctx.Err() != nil || finished.Before(now) || finished.Sub(now) > 3*time.Second {
		h.denied.Add(1)
		respond(http.StatusOK, "deny")
		return
	}
	h.allowed.Add(1)
	respond(http.StatusOK, "allow")
}

func (h *Handler) WriteMetrics(_ context.Context, w io.Writer, _ time.Time) {
	_, _ = fmt.Fprintf(w, "# HELP self_deepsearch_media_upload_admission_enabled Private upload admission, not billing or image delivery control.\n# TYPE self_deepsearch_media_upload_admission_enabled gauge\nself_deepsearch_media_upload_admission_enabled 1\n"+
		"# TYPE self_deepsearch_media_upload_admission_checks_total counter\nself_deepsearch_media_upload_admission_checks_total{outcome=\"allowed\"} %d\nself_deepsearch_media_upload_admission_checks_total{outcome=\"denied\"} %d\nself_deepsearch_media_upload_admission_checks_total{outcome=\"busy\"} %d\n", h.allowed.Load(), h.denied.Load(), h.busy.Load())
}
