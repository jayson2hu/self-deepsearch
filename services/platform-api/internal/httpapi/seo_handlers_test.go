package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

type fakeSEOInventory struct {
	*fakeOperations
	query operations.SEOPageQuery
}

func (repository *fakeSEOInventory) SEOInventorySummary(_ context.Context, now time.Time) (operations.SEOInventorySummary, error) {
	return operations.SEOInventorySummary{ObservedAt: now, Total: 3, Indexable: 1, Excluded: 2, Unknown: 1,
		SitemapCandidates: 1, Published: 2, Hidden: 1, Reasons: map[string]int64{"quality_unknown": 1}}, repository.operationErr
}

func (repository *fakeSEOInventory) ListSEOPages(_ context.Context, query operations.SEOPageQuery) (operations.SEOPageResult, error) {
	repository.query = query
	reason := "quality_unknown"
	return operations.SEOPageResult{Total: 1, Items: []operations.SEOPage{{EntityType: "work",
		EntityID: "10000000-0000-4000-8000-000000000001", URLPath: "/works/test-001", Title: "TEST-001 示例",
		Description: "TEST-001 的公开发行与人物资料。", Canonical: "/works/test-001", Robots: "noindex,nofollow",
		PublicationStatus: "published", ContentClassification: "non_explicit", QualityStatus: "unknown",
		MediaPolicy: "missing", ExclusionReason: &reason, UpdatedAt: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC),
	}}}, repository.operationErr
}

func TestSEOInventoryRequiresManagementRoleAndReturnsExplicitEligibility(t *testing.T) {
	repository := &fakeSEOInventory{fakeOperations: &fakeOperations{}}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/seo/summary", ""))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"indexable":1`) ||
		!strings.Contains(response.Body.String(), `"quality_unknown":1`) {
		t.Fatalf("unexpected SEO summary %d: %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	adminHandler("editor", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet, "/admin/v1/seo/summary", ""))
	if response.Code != http.StatusForbidden {
		t.Fatalf("editor summary returned %d: %s", response.Code, response.Body.String())
	}
}

func TestSEOPageFiltersAreBoundedAndExposeRevisionRoute(t *testing.T) {
	repository := &fakeSEOInventory{fakeOperations: &fakeOperations{}}
	response := httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/seo/pages?entity_type=work&status=published&q=TEST&limit=25&offset=0", ""))
	if response.Code != http.StatusOK || repository.query.EntityType != "work" || repository.query.Query != "TEST" ||
		!strings.Contains(response.Body.String(), `"revision_href":"/catalog/work/10000000-0000-4000-8000-000000000001"`) {
		t.Fatalf("unexpected SEO pages %d: %s query=%+v", response.Code, response.Body.String(), repository.query)
	}

	response = httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodGet,
		"/admin/v1/seo/pages?limit=101", ""))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unbounded SEO list returned %d: %s", response.Code, response.Body.String())
	}
}
