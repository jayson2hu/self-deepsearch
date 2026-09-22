package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type visitorCatalog struct {
	*fakeCatalog
	hash  string
	calls int
}

func (r *visitorCatalog) RecordPageViewWithVisitor(_ context.Context, _, _, hash string) error {
	r.hash = hash
	r.calls++
	return r.repositoryErr
}

func TestVisitorTokenIsOptionalHashedAndOptIn(t *testing.T) {
	const token = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, tc := range []struct {
		enabled           bool
		token             string
		status, calls, pv int
	}{{true, token, 204, 1, 0}, {false, token, 204, 0, 1}, {true, "", 204, 0, 1}, {true, "1.2.3.4", 400, 0, 0}} {
		repo := &visitorCatalog{fakeCatalog: &fakeCatalog{}}
		handler := New(Options{Catalog: repo, VisitorMetricsEnabled: tc.enabled})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/page-view", strings.NewReader(`{"content_type":"work","content_id":"10000000-0000-4000-8000-000000000001","visitor_token":"`+tc.token+`"}`))
		req.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != tc.status || repo.calls != tc.calls || repo.pageViews != tc.pv {
			t.Fatalf("unexpected %+v code=%d visitor=%d pv=%d", tc, result.Code, repo.calls, repo.pageViews)
		}
		if repo.calls > 0 {
			digest := sha256.Sum256([]byte(token))
			if repo.hash != hex.EncodeToString(digest[:]) || strings.Contains(result.Body.String(), token) {
				t.Fatal("raw visitor token retained or leaked")
			}
		}
	}
}
