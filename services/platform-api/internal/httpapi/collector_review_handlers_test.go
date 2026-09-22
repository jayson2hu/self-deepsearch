package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/collectorreview"
	"self-deepsearch/services/platform-api/internal/identity"
)

type collectorReviewStub struct {
	collectorStub
	query   collectorreview.Query
	reads   int
	failure error
}

func (stub *collectorReviewStub) ListCollectorBatches(_ context.Context, q collectorreview.Query) (collectorreview.BatchPage, error) {
	stub.query = q
	stub.reads++
	return collectorreview.BatchPage{Items: []collectorreview.Batch{{BatchID: "12345678-1234-1234-1234-123456789abc", InputCount: 2, InsertedCount: 1, DuplicateCount: 1, CandidateCount: 1, PendingCount: 1}}, Total: 1}, stub.failure
}
func (stub *collectorReviewStub) ListCollectorCandidates(_ context.Context, q collectorreview.Query) (collectorreview.CandidatePage, error) {
	stub.query = q
	stub.reads++
	return collectorreview.CandidatePage{Items: []collectorreview.Candidate{{Name: "<script>alert(1)</script>", Aliases: []string{}, CandidateStatus: "pending", AdultStatus: "unknown", RightsStatus: "needs_review", PublicationStatus: "staging"}}, Total: 1}, stub.failure
}
func (stub *collectorReviewStub) CollectorCandidate(_ context.Context, _ string) (collectorreview.Candidate, error) {
	stub.reads++
	return collectorreview.Candidate{Name: "<script>alert(1)</script>", Aliases: []string{}, CandidateStatus: "pending"}, stub.failure
}

func TestCollectorReviewAuthorizationAndReadOnly(t *testing.T) {
	for _, path := range []string{"/admin/v1/collector/batches", "/admin/v1/collector/candidates", "/admin/v1/collector/candidates/12345678-1234-1234-1234-123456789abc"} {
		for _, role := range []string{"", "user", "editor", "admin", "owner"} {
			t.Run(path+"/"+role, func(t *testing.T) {
				stub := &collectorReviewStub{}
				handler := New(Options{Collector: stub, Identity: &fakeIdentityService{user: identity.User{Role: role}}})
				r := httptest.NewRequest(http.MethodGet, path, nil)
				if role != "" {
					r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "operator-session"})
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := http.StatusOK
				if role == "" {
					want = http.StatusUnauthorized
				}
				if role == "user" {
					want = http.StatusForbidden
				}
				if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d body=%s headers=%v", w.Code, w.Body, w.Header())
				}
				if want != http.StatusOK && stub.reads != 0 {
					t.Fatal("unauthorized caller reached store")
				}
				if stub.calls != 0 {
					t.Fatal("read invoked ingestion")
				}
				if strings.Contains(w.Body.String(), "<script>") {
					t.Fatal("HTML not escaped in JSON")
				}
			})
		}
	}
}

func TestCollectorReviewQueryAndErrors(t *testing.T) {
	for _, tc := range []struct {
		path    string
		failure error
		want    int
		read    bool
	}{
		{"/candidates?q=Alias&status=pending&limit=2&offset=4", nil, 200, true},
		{"/candidates?limit=101", nil, 400, false}, {"/candidates?offset=-1", nil, 400, false},
		{"/candidates?offset=1000001", nil, 400, false}, {"/candidates?source_id=nope", nil, 400, false},
		{"/candidates?batch_id=nope", nil, 400, false}, {"/candidates?status=published", nil, 400, false},
		{"/candidates?q=" + strings.Repeat("x", 201), nil, 400, false},
		{"/candidates?q=%00", nil, 400, false},
		{"/candidates/not-a-uuid", nil, 404, false},
		{"/candidates/12345678-1234-1234-1234-123456789abc", collectorreview.ErrNotFound, 404, true},
		{"/batches", errors.New("secret database detail"), 503, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			stub := &collectorReviewStub{failure: tc.failure}
			handler := New(Options{Collector: stub, Identity: &fakeIdentityService{user: identity.User{Role: "editor"}}})
			r := httptest.NewRequest(http.MethodGet, "/admin/v1/collector"+tc.path, nil)
			r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want || (stub.reads > 0) != tc.read || strings.Contains(w.Body.String(), "secret database detail") {
				t.Fatalf("status=%d reads=%d body=%s", w.Code, stub.reads, w.Body)
			}
			if tc.want == 200 {
				if stub.query.Search != "Alias" || stub.query.Status != "pending" || stub.query.Limit != 2 || stub.query.Offset != 4 {
					t.Fatalf("query lost: %+v", stub.query)
				}
				var page collectorreview.CandidatePage
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Items[0].PublicationStatus != "staging" || page.Items[0].AdultStatus != "unknown" {
					t.Fatalf("invalid result: %s", w.Body)
				}
			}
		})
	}
}

func TestCollectorReviewDisabledRepository(t *testing.T) {
	handler := New(Options{Identity: &fakeIdentityService{user: identity.User{Role: "owner"}}})
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/collector/batches", nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("got %d: %s", w.Code, w.Body)
	}
}
