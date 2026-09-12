package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

type usageOperations struct {
	*fakeOperations
	calls  int
	input  operations.MediaUsageReviewInput
	replay bool
}

func (r *usageOperations) GetMediaUsage(context.Context, time.Time) (operations.MediaUsageOverview, error) {
	r.calls++
	return operations.MediaUsageOverview{Reviews: []operations.MediaUsageReview{}, Enforcement: "not_connected"}, r.operationErr
}
func (r *usageOperations) SubmitMediaUsageReview(_ context.Context, in operations.MediaUsageReviewInput, actor, id string, now time.Time) (operations.MediaUsageReview, error) {
	r.calls++
	r.input = in
	return operations.MediaUsageReview{ID: previousPrimaryID, Action: in.Action, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), Reason: in.Reason, IdempotentReplay: r.replay}, r.operationErr
}
func usageBody() string {
	return `{"idempotency_key":"` + previousPrimaryID + `","action":"acknowledge","expected_digest":"` + strings.Repeat("a", 64) + `","expected_updated_at":"2026-09-11T00:00:00.123456Z","reason":"人工复核","confirmed":true}`
}

func TestUsageControlsRequireRolesRecentAuthAndExactConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, role, body      string
		authenticated, recent bool
		err                   error
		status                int
	}{
		{"pending", "owner", usageBody(), true, true, nil, 202}, {"admin", "admin", usageBody(), true, true, nil, 202},
		{"anonymous", "owner", usageBody(), false, false, nil, 401}, {"editor", "editor", usageBody(), true, true, nil, 403}, {"user", "user", usageBody(), true, true, nil, 403},
		{"recent-auth", "owner", usageBody(), true, false, nil, 401},
		{"confirmation", "owner", strings.Replace(usageBody(), `"confirmed":true`, `"confirmed":false`, 1), true, true, nil, 400},
		{"bad-date", "owner", strings.Replace(usageBody(), "2026-09-11T00:00:00.123456Z", "bad", 1), true, true, nil, 400},
		{"stale", "owner", usageBody(), true, true, operations.ErrConflict, 409},
		{"unavailable", "owner", usageBody(), true, true, errors.New("private credential detail"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &usageOperations{fakeOperations: &fakeOperations{operationErr: tc.err}}
			id := &fakeRecentIdentityService{fakeIdentityService: &fakeIdentityService{user: identity.User{ID: previousPrimaryID, Role: tc.role}}}
			h := New(Options{Identity: id, Operations: repo})
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/media/usage/reviews", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authenticated {
				req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
			}
			if tc.recent {
				req.AddCookie(&http.Cookie{Name: identity.ReauthCookieName, Value: "reauth"})
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != tc.status || res.Header().Get("Cache-Control") != "no-store" || strings.Contains(res.Body.String(), "private credential detail") {
				t.Fatalf("unsafe response: %d %s", res.Code, res.Body.String())
			}
			if tc.status == 202 && !strings.Contains(res.Body.String(), `"enforcement":"not_connected"`) {
				t.Fatal("queued review claimed enforcement")
			}
			if tc.err == nil && tc.status != 202 && repo.calls != 0 {
				t.Fatal("rejected request reached database")
			}
		})
	}
}
func TestUsageReadIsPrivateNullableAndReplayIsNotAnotherMutation(t *testing.T) {
	for _, role := range []string{"owner", "admin", "editor", "user"} {
		repo := &usageOperations{fakeOperations: &fakeOperations{}}
		res := httptest.NewRecorder()
		adminHandler(role, repo).ServeHTTP(res, authenticatedAdminRequest(http.MethodGet, "/admin/v1/media/usage", ""))
		want := 200
		if role == "editor" || role == "user" {
			want = 403
		}
		if res.Code != want {
			t.Fatalf("role %s %d", role, res.Code)
		}
		if want == 200 && (!strings.Contains(res.Body.String(), `"state":null`) || strings.Contains(res.Body.String(), "token")) {
			t.Fatal("uninitialized state or secrets mishandled")
		}
	}
	repo := &usageOperations{fakeOperations: &fakeOperations{}, replay: true}
	res := httptest.NewRecorder()
	adminHandler("owner", repo).ServeHTTP(res, authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/usage/reviews", usageBody()))
	var body struct {
		Review operations.MediaUsageReview `json:"review"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if res.Code != 200 || !body.Review.IdempotentReplay || body.Review.Status != "pending" {
		t.Fatal("replay did not preserve prior result")
	}
	request := authenticatedAdminRequest(http.MethodPost, "/admin/v1/media/usage/reviews", usageBody())
	request.Header.Set("Origin", "https://evil.example.test")
	res = httptest.NewRecorder()
	adminHandler("owner", repo).ServeHTTP(res, request)
	if res.Code != 403 {
		t.Fatal("cross-origin review accepted")
	}
}
