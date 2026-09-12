package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type uploadOperations struct {
	*fakeOperations
	calls          int
	replay         bool
	actor, request string
	bounded        bool
}

func (r *uploadOperations) GetMediaUploadControl(ctx context.Context, _ time.Time) (operations.MediaUploadOverview, error) {
	r.calls++
	_, r.bounded = ctx.Deadline()
	return operations.MediaUploadOverview{Commands: []operations.MediaUploadCommand{}}, r.operationErr
}
func (r *uploadOperations) SubmitMediaUploadCommand(ctx context.Context, in operations.MediaUploadInput, actor, request string, now time.Time) (operations.MediaUploadCommand, error) {
	r.calls++
	r.actor, r.request = actor, request
	_, r.bounded = ctx.Deadline()
	return operations.MediaUploadCommand{ID: previousPrimaryID, ActorID: actor, Mode: in.Mode, Reason: in.Reason, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), IdempotentReplay: r.replay}, r.operationErr
}
func uploadBody() string {
	return `{"idempotency_key":"` + previousPrimaryID + `","target_ref":"` + strings.Repeat("a", 64) + `","expected_epoch":"","expected_generation":0,"expected_observed_at":"2026-09-11T00:00:00.123456Z","mode":"paused","reason":"人工暂停","confirmed":true}`
}
func TestUploadCommandAuthAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, role, body      string
		authenticated, recent bool
		err                   error
		status                int
	}{
		{"owner", "owner", uploadBody(), true, true, nil, 202},
		{"admin", "admin", uploadBody(), true, true, nil, 202},
		{"anonymous", "owner", uploadBody(), false, false, nil, 401},
		{"editor", "editor", uploadBody(), true, true, nil, 403},
		{"user", "user", uploadBody(), true, true, nil, 403},
		{"recent", "owner", uploadBody(), true, false, nil, 401},
		{"unconfirmed", "owner", strings.Replace(uploadBody(), `"confirmed":true`, `"confirmed":false`, 1), true, true, nil, 400},
		{"missing_generation", "owner", strings.Replace(uploadBody(), `"expected_generation":0,`, "", 1), true, true, nil, 400},
		{"null_generation", "owner", strings.Replace(uploadBody(), `"expected_generation":0`, `"expected_generation":null`, 1), true, true, nil, 400},
		{"fractional_generation", "owner", strings.Replace(uploadBody(), `"expected_generation":0`, `"expected_generation":0.5`, 1), true, true, nil, 400},
		{"initial_resume", "owner", strings.Replace(uploadBody(), `"mode":"paused"`, `"mode":"enabled"`, 1), true, true, nil, 400},
		{"unknown_field", "owner", strings.Replace(uploadBody(), `"confirmed":true`, `"confirmed":true,"secret":"invalid"`, 1), true, true, nil, 400},
		{"stale", "owner", uploadBody(), true, true, operations.ErrConflict, 409},
		{"not_initialized", "owner", uploadBody(), true, true, operations.ErrNotFound, 404},
		{"database_failure", "owner", uploadBody(), true, true, errors.New("synthetic private credential"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &uploadOperations{fakeOperations: &fakeOperations{operationErr: tc.err}}
			id := &fakeRecentIdentityService{fakeIdentityService: &fakeIdentityService{user: identity.User{ID: previousPrimaryID, Role: tc.role}}}
			h := New(Options{Identity: id, Operations: repo})
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/media/upload-control/commands", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authenticated {
				req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
			}
			if tc.recent {
				req.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "reauth"})
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != tc.status || res.Header().Get("Cache-Control") != "no-store" || strings.Contains(res.Body.String(), "synthetic private credential") {
				t.Fatalf("unsafe response %d %s", res.Code, res.Body.String())
			}
			if tc.status == 202 && (!strings.Contains(res.Body.String(), `"status":"pending"`) || repo.actor != previousPrimaryID || repo.request == "" || !repo.bounded) {
				t.Fatal("unbounded or unattributed request, or queue claimed success")
			}
			if tc.err == nil && tc.status != 202 && repo.calls != 0 {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}
func TestUploadReadReplayCSRFAndMissingRepository(t *testing.T) {
	for _, role := range []string{"owner", "admin", "editor", "user"} {
		repo := &uploadOperations{fakeOperations: &fakeOperations{}}
		res := httptest.NewRecorder()
		adminHandler(role, repo).ServeHTTP(res, authenticatedAdminRequest(http.MethodGet, "/admin/v1/media/upload-control", ""))
		want := 200
		if role == "editor" || role == "user" {
			want = 403
		}
		if res.Code != want {
			t.Fatalf("wrong read access %s %d", role, res.Code)
		}
		if want == 200 && (!strings.Contains(res.Body.String(), `"state":null`) || !repo.bounded) {
			t.Fatal("uninitialized state must be nullable and read bounded")
		}
	}
	repo := &uploadOperations{fakeOperations: &fakeOperations{}, replay: true}
	res := httptest.NewRecorder()
	adminHandler("owner", repo).ServeHTTP(res, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/upload-control/commands", uploadBody()))
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"idempotent_replay":true`) || !strings.Contains(res.Body.String(), `"status":"pending"`) {
		t.Fatal("replay changed meaning")
	}
	req := authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/upload-control/commands", uploadBody())
	req.Header.Set("Origin", "https://evil.example.test")
	res = httptest.NewRecorder()
	adminHandler("owner", repo).ServeHTTP(res, req)
	if res.Code != 403 || repo.calls != 1 {
		t.Fatal("CSRF reached repository")
	}
	res = httptest.NewRecorder()
	adminHandler("owner", &fakeOperations{}).ServeHTTP(res, authenticatedAdminRequest(http.MethodGet, "/admin/v1/media/upload-control", ""))
	if res.Code != 503 {
		t.Fatal("missing repository must not fabricate state")
	}
}
