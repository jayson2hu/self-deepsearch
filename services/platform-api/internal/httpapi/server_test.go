package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/account"
	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/identity"
)

type fakeDatabase struct {
	pingErr       error
	schemaVersion int
	schemaErr     error
}

func (database fakeDatabase) Ping(context.Context) error { return database.pingErr }
func (database fakeDatabase) SchemaVersion(context.Context) (int, error) {
	return database.schemaVersion, database.schemaErr
}

type pointerDatabase struct{}

func (*pointerDatabase) Ping(context.Context) error { return nil }
func (*pointerDatabase) SchemaVersion(context.Context) (int, error) {
	return requiredDatabaseSchemaVersion, nil
}

type fakeCatalog struct {
	latest            []catalog.WorkSummary
	detail            catalog.WorkDetail
	searchRaw         string
	searchCode        string
	searchCalls       int
	performerSearch   []catalog.PerformerSearchResult
	studioSearch      []catalog.StudioSearchResult
	entitySearchQuery string
	entitySearchAfter *catalog.SearchCursor
	relatedWorks      []catalog.RelatedWorkResult
	relatedSlug       string
	relatedAfter      *catalog.RelatedWorkCursor
	relatedType       string
	performer         catalog.PerformerDetail
	studio            catalog.StudioDetail
	pageViews         int
	pageViewTypes     []string
	searchMetricCalls int
	searchMetricZero  bool
	sitemapEntries    []catalog.SitemapEntry
	sitemapCount      int
	sitemapOffset     int
	sitemapLimit      int
	repositoryErr     error
}

type recordingAccount struct {
	account.Repository
	historyCalls int
	userID       string
	contentType  string
	contentID    string
	viewedAt     time.Time
	historyErr   error
}

func (repository *recordingAccount) RecordHistory(_ context.Context, userID, contentType, contentID string, viewedAt time.Time) error {
	repository.historyCalls++
	repository.userID = userID
	repository.contentType = contentType
	repository.contentID = contentID
	repository.viewedAt = viewedAt
	return repository.historyErr
}

func (repository *fakeCatalog) LatestWorks(context.Context, string, int) ([]catalog.WorkSummary, error) {
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) RecentlyAddedWorks(context.Context, string, int) ([]catalog.WorkSummary, error) {
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) EditorialWorks(context.Context, int) ([]catalog.WorkSummary, error) {
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) LatestPerformers(context.Context, int) ([]catalog.PerformerSummary, error) {
	return []catalog.PerformerSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) DiscoveryMixRule(context.Context) (catalog.DiscoveryMixRule, error) {
	return catalog.DiscoveryMixRule{WorkSlots: 4, PerformerSlots: 1, RepeatWindow: 10}, repository.repositoryErr
}
func (repository *fakeCatalog) TrendingWorks(context.Context, time.Duration, string, int) ([]catalog.WorkSummary, error) {
	return []catalog.WorkSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) MostViewedWorks(context.Context, *time.Duration, string, int) ([]catalog.WorkSummary, error) {
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) SearchWorks(_ context.Context, raw, compact, _ string, _ int) ([]catalog.WorkSummary, error) {
	repository.searchCalls++
	repository.searchRaw, repository.searchCode = raw, compact
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) SearchPerformers(_ context.Context, query string, after *catalog.SearchCursor, _ int) ([]catalog.PerformerSearchResult, error) {
	repository.entitySearchQuery, repository.entitySearchAfter = query, after
	return repository.performerSearch, repository.repositoryErr
}
func (repository *fakeCatalog) SearchStudios(_ context.Context, query string, after *catalog.SearchCursor, _ int) ([]catalog.StudioSearchResult, error) {
	repository.entitySearchQuery, repository.entitySearchAfter = query, after
	return repository.studioSearch, repository.repositoryErr
}
func (repository *fakeCatalog) ListPerformers(context.Context, string, int) ([]catalog.PerformerSummary, error) {
	return []catalog.PerformerSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) PerformerBySlug(context.Context, string) (catalog.PerformerDetail, error) {
	return repository.performer, repository.repositoryErr
}
func (repository *fakeCatalog) PerformerWorksBySlug(_ context.Context, slug string, after *catalog.RelatedWorkCursor, _ int) ([]catalog.RelatedWorkResult, error) {
	repository.relatedSlug, repository.relatedAfter, repository.relatedType = slug, after, "performer"
	return repository.relatedWorks, repository.repositoryErr
}
func (repository *fakeCatalog) ListStudios(context.Context, string, int) ([]catalog.StudioSummary, error) {
	return []catalog.StudioSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) StudioBySlug(context.Context, string) (catalog.StudioDetail, error) {
	return repository.studio, repository.repositoryErr
}
func (repository *fakeCatalog) StudioWorksBySlug(_ context.Context, slug string, after *catalog.RelatedWorkCursor, _ int) ([]catalog.RelatedWorkResult, error) {
	repository.relatedSlug, repository.relatedAfter, repository.relatedType = slug, after, "studio"
	return repository.relatedWorks, repository.repositoryErr
}
func (repository *fakeCatalog) WorkBySlug(context.Context, string) (catalog.WorkDetail, error) {
	return repository.detail, repository.repositoryErr
}
func (repository *fakeCatalog) RecordPageView(_ context.Context, contentType, _ string) error {
	repository.pageViews++
	repository.pageViewTypes = append(repository.pageViewTypes, contentType)
	return repository.repositoryErr
}

func (repository *fakeCatalog) RecordSearchOutcome(_ context.Context, zeroResult bool) error {
	repository.searchMetricCalls++
	repository.searchMetricZero = zeroResult
	return nil
}
func (repository *fakeCatalog) SitemapEntries(_ context.Context, _ string, offset, limit int) ([]catalog.SitemapEntry, error) {
	repository.sitemapOffset, repository.sitemapLimit = offset, limit
	if repository.sitemapEntries != nil {
		return repository.sitemapEntries, repository.repositoryErr
	}
	return []catalog.SitemapEntry{{EntityType: "work", Slug: "test-001", UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}}, repository.repositoryErr
}
func (repository *fakeCatalog) SitemapCount(context.Context, string) (int, error) {
	if repository.sitemapCount != 0 || repository.sitemapEntries != nil {
		return repository.sitemapCount, repository.repositoryErr
	}
	return 1, repository.repositoryErr
}
func (repository *fakeCatalog) RedirectTarget(context.Context, string) (catalog.RedirectTarget, error) {
	return catalog.RedirectTarget{}, catalog.ErrNotFound
}

func TestRelatedWorksPaginationBindsFullTupleAndScope(t *testing.T) {
	releaseDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	publishedAt := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	items := make([]catalog.RelatedWorkResult, 0, publicPageSize+1)
	for index := 1; index <= publicPageSize+1; index++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		itemDate := &releaseDate
		if index == publicPageSize {
			itemDate = nil
		}
		items = append(items, catalog.RelatedWorkResult{
			WorkSummary: catalog.WorkSummary{ID: id, Code: fmt.Sprintf("REL-%03d", index), Title: "关联作品", Slug: fmt.Sprintf("rel-%03d", index)},
			Cursor:      catalog.RelatedWorkCursor{ReleaseDate: itemDate, PublishedAt: publishedAt.Add(-time.Duration(index) * time.Minute), ID: id},
		})
	}
	repository := &fakeCatalog{relatedWorks: items}
	handler := New(Options{Catalog: repository})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/performers/test-performer/works", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	page, ok := body["items"].([]any)
	if !ok || len(page) != publicPageSize || body["total"] != nil {
		t.Fatalf("unexpected related works page: %#v", body)
	}
	first := page[0].(map[string]any)
	if first["href"] != "/works/rel-001" {
		t.Fatalf("expected real work href, got %#v", first)
	}
	cursor, ok := body["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("expected next cursor: %#v", body)
	}
	if len(cursor) > 256 {
		t.Fatalf("related works cursor exceeds OpenAPI limit: %d", len(cursor))
	}

	repository.relatedAfter = nil
	repository.relatedWorks = nil
	request = httptest.NewRequest(http.MethodGet, "/api/v1/performers/test-performer/works?cursor="+url.QueryEscape(cursor), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || repository.relatedAfter == nil {
		t.Fatalf("expected cursor to reach repository, status=%d after=%#v", response.Code, repository.relatedAfter)
	}
	if repository.relatedAfter.ReleaseDate != nil || !repository.relatedAfter.PublishedAt.Equal(items[publicPageSize-1].Cursor.PublishedAt) || repository.relatedAfter.ID != items[publicPageSize-1].ID {
		t.Fatalf("cursor lost ordering tuple: %#v", repository.relatedAfter)
	}
	var tail itemListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &tail); err != nil || len(tail.Items) != 0 || tail.NextCursor != nil {
		t.Fatalf("empty tail should terminate without a total or next cursor: body=%s err=%v", response.Body.String(), err)
	}

	repository.relatedAfter = nil
	request = httptest.NewRequest(http.MethodGet, "/api/v1/performers/other-performer/works?cursor="+url.QueryEscape(cursor), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || repository.relatedAfter != nil {
		t.Fatalf("cross-parent cursor should be rejected before repository: %d %#v", response.Code, repository.relatedAfter)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/studios/test-performer/works?cursor="+url.QueryEscape(cursor), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cross-type cursor should be rejected, got %d", response.Code)
	}
}

func TestRelatedWorksRejectsBadCursorAndHiddenParent(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/studios/test-studio/works?cursor=not-a-cursor", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid cursor 400, got %d", response.Code)
	}

	repository.repositoryErr = catalog.ErrNotFound
	request = httptest.NewRequest(http.MethodGet, "/api/v1/studios/test-studio/works", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected hidden parent 404, got %d", response.Code)
	}
}

func TestHealth(t *testing.T) {
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	handler := New(Options{Service: "platform-api", Version: "test", Now: func() time.Time { return now }})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "test-request")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Header().Get("X-Request-ID") != "test-request" {
		t.Fatalf("expected request id to be returned")
	}
	var body healthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Status != "ok" || body.Version != "test" || body.Time != "2026-08-05T08:00:00Z" {
		t.Fatalf("unexpected health response: %#v", body)
	}
}

func TestRequestIDRejectsControlCharacters(t *testing.T) {
	handler := New(Options{Service: "platform-api", Version: "test"})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "bad\nrequest")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "bad\nrequest" || !validRequestID(requestID) {
		t.Fatalf("unsafe request id was accepted: %q", requestID)
	}
}

func TestValidRequestIDKeepsStableCorrelationValue(t *testing.T) {
	handler := New(Options{Service: "platform-api", Version: "test"})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "release-a:smoke-01")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "release-a:smoke-01" {
		t.Fatalf("valid request id changed: %q", got)
	}
}

func TestMetricsRequireTokenAndUseNormalizedRoutes(t *testing.T) {
	handler := New(Options{
		Service: "platform-api", Version: "test",
		Database: fakeDatabase{schemaVersion: requiredDatabaseSchemaVersion}, Catalog: &fakeCatalog{},
		MetricsToken: "test-metrics-token",
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/works/private-work-slug", nil))
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected metrics 401, got %d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer test-metrics-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `route="/healthz"`) || !strings.Contains(body, "self_deepsearch_database_ready 1") {
		t.Fatalf("unexpected metrics response %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		"# TYPE self_deepsearch_http_request_duration_seconds histogram",
		`route="/api/v1/works/{slug}"`,
		`self_deepsearch_http_request_duration_seconds_bucket{method="GET",route="/healthz",status_class="2xx",le="+Inf"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected metrics output to contain %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "private-work-slug") {
		t.Fatalf("metrics leaked a concrete route parameter: %s", body)
	}
}

func TestMetricsAreDisabledWithoutToken(t *testing.T) {
	response := httptest.NewRecorder()
	New(Options{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected disabled metrics to return 404, got %d", response.Code)
	}
}

func TestReadinessNotConfigured(t *testing.T) {
	handler := New(Options{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

func TestReadinessTreatsTypedNilAsNotConfigured(t *testing.T) {
	var database *pointerDatabase
	handler := New(Options{Database: database})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
}

func TestReadinessDatabaseUnavailable(t *testing.T) {
	handler := New(Options{Database: fakeDatabase{pingErr: errors.New("connection refused")}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
	if response.Body.String() == "" {
		t.Fatal("expected readiness body")
	}
}

func TestReadinessDatabaseAvailableAtRequiredSchemaVersion(t *testing.T) {
	handler := New(Options{Database: fakeDatabase{schemaVersion: requiredDatabaseSchemaVersion}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	if body.Status != "ready" || len(body.Dependencies) != 1 || body.Dependencies[0].Status != "ok" {
		t.Fatalf("unexpected readiness body: %#v", body)
	}
}

func TestReadinessRejectsSchemaVersionMismatch(t *testing.T) {
	handler := New(Options{Database: fakeDatabase{schemaVersion: requiredDatabaseSchemaVersion - 1}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	if body.Status != "not_ready" || len(body.Dependencies) != 1 || body.Dependencies[0].Status != "schema_mismatch" {
		t.Fatalf("unexpected readiness body: %#v", body)
	}
	wantDetail := fmt.Sprintf("expected schema version %d, got %d", requiredDatabaseSchemaVersion, requiredDatabaseSchemaVersion-1)
	if body.Dependencies[0].Detail != wantDetail {
		t.Fatalf("expected mismatch detail %q, got %q", wantDetail, body.Dependencies[0].Detail)
	}
}

func TestReadinessRejectsSchemaVersionQueryFailure(t *testing.T) {
	handler := New(Options{Database: fakeDatabase{
		schemaVersion: requiredDatabaseSchemaVersion,
		schemaErr:     errors.New("metadata table unavailable"),
	}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
	var body readinessResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness body: %v", err)
	}
	if body.Status != "not_ready" || len(body.Dependencies) != 1 || body.Dependencies[0].Status != "unavailable" {
		t.Fatalf("unexpected readiness body: %#v", body)
	}
	if body.Dependencies[0].Detail != "database schema version query failed" {
		t.Fatalf("unexpected query failure detail: %q", body.Dependencies[0].Detail)
	}
}

func TestHomeUsesPublishedRepository(t *testing.T) {
	repository := &fakeCatalog{latest: []catalog.WorkSummary{{
		ID: "10000000-0000-4000-8000-000000000001", Code: "TEST-001", Title: "测试作品", Slug: "test-001",
	}}}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/home", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var body homeResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode home response: %v", err)
	}
	sections := make(map[string]homeSection, len(body.Sections))
	for _, section := range body.Sections {
		sections[section.Key] = section
	}
	if len(body.Sections) != 6 || len(sections["discovery"].Items) != 1 || len(sections["latest"].Items) != 1 || len(sections["editorial"].Items) != 1 || len(sections["most_viewed"].Items) != 1 {
		t.Fatalf("unexpected home response: %#v", body)
	}
}

func TestHomeImageIncludesStructuredRenditionsAndLegacyURL(t *testing.T) {
	image := responsiveCatalogImage()
	legacyURL := image.URL
	repository := &fakeCatalog{latest: []catalog.WorkSummary{{
		ID: "10000000-0000-4000-8000-000000000001", Code: "TEST-001", Title: "测试作品", Slug: "test-001",
		ImageURL: &legacyURL, Image: &image,
	}}}
	response := httptest.NewRecorder()
	New(Options{Catalog: repository}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/home", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body homeResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode home response: %v", err)
	}
	item := body.Sections[1].Items[0]
	if item.ImageURL == nil || *item.ImageURL != image.URL || item.Image == nil || len(item.Image.Renditions) != 3 {
		t.Fatalf("expected compatibility URL and structured renditions, got %#v", item)
	}
}

func TestSearchNormalizesWorkCode(t *testing.T) {
	repository := &fakeCatalog{latest: []catalog.WorkSummary{{
		ID: "10000000-0000-4000-8000-000000000001", Code: "AB-123", Title: "测试作品", Slug: "ab-123",
	}}}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=ab-123", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if repository.searchRaw != "ab-123" || repository.searchCode != "AB123" {
		t.Fatalf("unexpected normalized search: %q, %q", repository.searchRaw, repository.searchCode)
	}
	if repository.searchMetricCalls != 1 || repository.searchMetricZero {
		t.Fatalf("expected one non-zero first-page search metric, got calls=%d zero=%t", repository.searchMetricCalls, repository.searchMetricZero)
	}
}

func TestSearchRecordsZeroResultOnlyForFirstPage(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=missing", nil))
	if first.Code != http.StatusOK || repository.searchMetricCalls != 1 || !repository.searchMetricZero {
		t.Fatalf("expected zero-result metric for first page, got status=%d calls=%d zero=%t", first.Code, repository.searchMetricCalls, repository.searchMetricZero)
	}
	values := make([]catalog.WorkSummary, 25)
	for index := range values {
		values[index].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", index+1)
	}
	repository.latest = values
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=missing", nil))
	if second.Code != http.StatusOK || repository.searchMetricCalls != 2 || repository.searchMetricZero {
		t.Fatalf("expected non-zero metric for populated first page, got status=%d calls=%d zero=%t", second.Code, repository.searchMetricCalls, repository.searchMetricZero)
	}
	_, cursor := pageValues(values, searchCursorScope("missing"))
	third := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=missing&cursor="+*cursor, nil)
	handler.ServeHTTP(third, request)
	if third.Code != http.StatusOK || repository.searchMetricCalls != 2 {
		t.Fatalf("expected cursor page not to add metric, got status=%d calls=%d", third.Code, repository.searchMetricCalls)
	}
}

func TestSearchNormalizesFullWidthCodeAndWhitespace(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=%EF%BC%A1%EF%BC%A2%E3%80%80%EF%BC%91%EF%BC%92%EF%BC%93", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if repository.searchRaw != "AB 123" || repository.searchCode != "AB123" {
		t.Fatalf("unexpected full-width normalization: %q, %q", repository.searchRaw, repository.searchCode)
	}
}

func TestSearchAcceptsMultilingualText(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=%E5%A4%8F%E3%81%AE%E8%A8%98%E9%8C%B2", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected multilingual query to return 200, got %d: %s", response.Code, response.Body.String())
	}
	if repository.searchRaw != "夏の記録" || repository.searchCode != "夏の記録" || repository.searchCalls != 1 {
		t.Fatalf("unexpected multilingual search: raw=%q compact=%q calls=%d", repository.searchRaw, repository.searchCode, repository.searchCalls)
	}
}

func TestSearchFailureDoesNotRecordZeroResult(t *testing.T) {
	repository := &fakeCatalog{repositoryErr: errors.New("synthetic search failure")}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=failure", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"DEPENDENCY_UNAVAILABLE"`) {
		t.Fatalf("expected explicit dependency failure, got %d: %s", response.Code, response.Body.String())
	}
	if repository.searchCalls != 1 || repository.searchMetricCalls != 0 {
		t.Fatalf("failed search must not record a zero-result metric: calls=%d metrics=%d", repository.searchCalls, repository.searchMetricCalls)
	}
}

func TestSearchCursorIsBoundToNormalizedQuery(t *testing.T) {
	values := make([]catalog.WorkSummary, publicPageSize+1)
	for index := range values {
		values[index].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", index+1)
	}
	_, cursor := pageValues(values, searchCursorScope("first query"))
	if cursor == nil {
		t.Fatal("expected query cursor")
	}
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	target := "/api/v1/search/works?q=second+query&cursor=" + url.QueryEscape(*cursor)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_CURSOR"`) {
		t.Fatalf("expected query-bound cursor rejection, got %d: %s", response.Code, response.Body.String())
	}
	if repository.searchCalls != 0 || repository.searchMetricCalls != 0 {
		t.Fatalf("invalid cursor reached search or metrics: calls=%d metrics=%d", repository.searchCalls, repository.searchMetricCalls)
	}
}

func TestEntitySearchReturnsAccurateTypesAndNormalizesQuery(t *testing.T) {
	performerID := "20000000-0000-4000-8000-000000000001"
	studioID := "30000000-0000-4000-8000-000000000001"
	repository := &fakeCatalog{
		performerSearch: []catalog.PerformerSearchResult{{
			PerformerSummary: catalog.PerformerSummary{ID: performerID, Name: "春野澪", Slug: "haruno-mio"},
			Cursor:           catalog.SearchCursor{Rank: 0, Name: "春野澪", ID: performerID},
		}},
		studioSearch: []catalog.StudioSearchResult{{
			StudioSummary: catalog.StudioSummary{ID: studioID, Name: "青空制作", Slug: "blue-sky"},
			Cursor:        catalog.SearchCursor{Rank: 0, Name: "青空制作", ID: studioID},
		}},
	}
	handler := New(Options{Catalog: repository})

	performer := httptest.NewRecorder()
	handler.ServeHTTP(performer, httptest.NewRequest(http.MethodGet, "/api/v1/search/performers?q=%E3%80%80%E6%98%A5%E9%87%8E%E6%BE%AA%E3%80%80", nil))
	if performer.Code != http.StatusOK || !strings.Contains(performer.Body.String(), `"name":"春野澪"`) || !strings.Contains(performer.Body.String(), `"href":"/performers/haruno-mio"`) {
		t.Fatalf("unexpected performer search response %d: %s", performer.Code, performer.Body.String())
	}
	if repository.entitySearchQuery != "春野澪" || repository.searchMetricCalls != 0 {
		t.Fatalf("performer search normalization/metrics mismatch: query=%q metrics=%d", repository.entitySearchQuery, repository.searchMetricCalls)
	}

	studio := httptest.NewRecorder()
	handler.ServeHTTP(studio, httptest.NewRequest(http.MethodGet, "/api/v1/search/studios?q=%E9%9D%92%E7%A9%BA%E5%88%B6%E4%BD%9C", nil))
	if studio.Code != http.StatusOK || !strings.Contains(studio.Body.String(), `"name":"青空制作"`) || !strings.Contains(studio.Body.String(), `"href":"/studios/blue-sky"`) {
		t.Fatalf("unexpected studio search response %d: %s", studio.Code, studio.Body.String())
	}
	if repository.searchMetricCalls != 0 {
		t.Fatal("new entity searches must not mix into work zero-result metrics")
	}
}

func TestEntitySearchRejectsInvalidNameWithoutRepositoryCall(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	for _, target := range []string{"/api/v1/search/performers?q=%E6%98%A5", "/api/v1/search/studios?q=--"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_QUERY"`) {
			t.Fatalf("expected invalid entity query for %s, got %d: %s", target, response.Code, response.Body.String())
		}
	}
	if repository.entitySearchQuery != "" || repository.searchMetricCalls != 0 {
		t.Fatalf("invalid entity query reached repository or metrics: query=%q metrics=%d", repository.entitySearchQuery, repository.searchMetricCalls)
	}
}

func TestEntitySearchCursorSurvivesWithoutAnchorLookupAndRejectsCrossScope(t *testing.T) {
	values := make([]catalog.PerformerSearchResult, publicPageSize+1)
	for index := range values {
		id := fmt.Sprintf("20000000-0000-4000-8000-%012d", index+1)
		values[index] = catalog.PerformerSearchResult{
			PerformerSummary: catalog.PerformerSummary{ID: id, Name: fmt.Sprintf("人物 %02d", index+1)},
			Cursor:           catalog.SearchCursor{Rank: 2, Name: fmt.Sprintf("人物 %02d", index+1), ID: id},
		}
	}
	_, cursor := pageRankedSearchValues(values, entitySearchCursorScope("performers", "人物"))
	if cursor == nil {
		t.Fatal("expected ranked search cursor")
	}

	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/api/v1/search/performers?q=%E4%BA%BA%E7%89%A9&cursor="+url.QueryEscape(*cursor), nil))
	if valid.Code != http.StatusOK || repository.entitySearchAfter == nil || repository.entitySearchAfter.ID != values[publicPageSize-1].ID {
		t.Fatalf("ranked cursor did not carry the deleted-anchor-independent tuple: status=%d after=%+v", valid.Code, repository.entitySearchAfter)
	}

	for _, target := range []string{
		"/api/v1/search/performers?q=%E5%88%AB%E4%BA%BA&cursor=" + url.QueryEscape(*cursor),
		"/api/v1/search/studios?q=%E4%BA%BA%E7%89%A9&cursor=" + url.QueryEscape(*cursor),
		"/api/v1/search/performers?q=%E4%BA%BA%E7%89%A9&cursor=not-a-cursor",
	} {
		repository.entitySearchQuery, repository.entitySearchAfter = "", nil
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_CURSOR"`) || repository.entitySearchQuery != "" {
			t.Fatalf("cross-scope cursor was not rejected before repository call: %s => %d %s", target, response.Code, response.Body.String())
		}
	}
}

func TestEntitySearchPaginationReturnsStableCursorAndTerminates(t *testing.T) {
	values := make([]catalog.PerformerSearchResult, publicPageSize+1)
	for index := range values {
		id := fmt.Sprintf("20000000-0000-4000-8000-%012d", index+1)
		values[index] = catalog.PerformerSearchResult{
			PerformerSummary: catalog.PerformerSummary{ID: id, Name: fmt.Sprintf("分页人物 %02d", index+1), Slug: fmt.Sprintf("page-person-%02d", index+1)},
			Cursor:           catalog.SearchCursor{Rank: 2, Name: fmt.Sprintf("分页人物 %02d", index+1), ID: id},
		}
	}
	repository := &fakeCatalog{performerSearch: values}
	handler := New(Options{Catalog: repository})
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/search/performers?q=%E5%88%86%E9%A1%B5", nil))
	var firstBody performerSearchResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first entity search page: %v", err)
	}
	if first.Code != http.StatusOK || len(firstBody.Items) != publicPageSize || firstBody.NextCursor == nil {
		t.Fatalf("unexpected first entity search page: status=%d items=%d cursor=%v", first.Code, len(firstBody.Items), firstBody.NextCursor)
	}

	repository.performerSearch = []catalog.PerformerSearchResult{}
	tail := httptest.NewRecorder()
	handler.ServeHTTP(tail, httptest.NewRequest(http.MethodGet, "/api/v1/search/performers?q=%E5%88%86%E9%A1%B5&cursor="+url.QueryEscape(*firstBody.NextCursor), nil))
	var tailBody performerSearchResponse
	if err := json.Unmarshal(tail.Body.Bytes(), &tailBody); err != nil {
		t.Fatalf("decode terminal entity search page: %v", err)
	}
	if tail.Code != http.StatusOK || len(tailBody.Items) != 0 || tailBody.NextCursor != nil || repository.entitySearchAfter == nil {
		t.Fatalf("unexpected terminal entity search page: status=%d items=%d cursor=%v after=%+v", tail.Code, len(tailBody.Items), tailBody.NextCursor, repository.entitySearchAfter)
	}
}

func TestEntitySearchFailureDoesNotRecordWorkSearchMetric(t *testing.T) {
	repository := &fakeCatalog{repositoryErr: errors.New("synthetic entity search failure")}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/performers?q=%E6%98%A5%E9%87%8E", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"DEPENDENCY_UNAVAILABLE"`) || repository.searchMetricCalls != 0 {
		t.Fatalf("entity search failure contract mismatch: %d metrics=%d body=%s", response.Code, repository.searchMetricCalls, response.Body.String())
	}
}

func TestSitemapUsesPublishedRepository(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/sitemap", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"slug":"test-001"`) {
		t.Fatalf("expected published sitemap entry, got %d: %s", response.Code, response.Body.String())
	}
}

func TestSitemapPaginationReportsCompletenessAndStableOffsets(t *testing.T) {
	repository := &fakeCatalog{
		sitemapCount:   50001,
		sitemapEntries: []catalog.SitemapEntry{{EntityType: "work", Slug: "test-50001", UpdatedAt: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)}},
	}
	response := httptest.NewRecorder()
	New(Options{Catalog: repository}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/sitemap?entity_type=work&page=2&page_size=50000", nil))
	if response.Code != http.StatusOK || repository.sitemapOffset != 50000 || repository.sitemapLimit != 50000 {
		t.Fatalf("unexpected sitemap page query: status=%d offset=%d limit=%d body=%s", response.Code, repository.sitemapOffset, repository.sitemapLimit, response.Body.String())
	}
	var body sitemapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sitemap response: %v", err)
	}
	if body.Page != 2 || body.PageSize != 50000 || body.TotalItems != 50001 || body.TotalPages != 2 || len(body.Items) != 1 {
		t.Fatalf("unexpected sitemap completeness metadata: %+v", body)
	}
}

func TestSitemapRejectsInvalidAndMissingPages(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		count  int
		status int
		code   string
	}{
		{name: "invalid zero", path: "/api/v1/site/sitemap?page=0", count: 1, status: http.StatusBadRequest, code: "INVALID_SITEMAP_PAGE"},
		{name: "invalid size", path: "/api/v1/site/sitemap?page_size=50001", count: 1, status: http.StatusBadRequest, code: "INVALID_SITEMAP_PAGE"},
		{name: "past final page", path: "/api/v1/site/sitemap?page=3&page_size=1", count: 2, status: http.StatusNotFound, code: "SITEMAP_PAGE_NOT_FOUND"},
		{name: "empty second page", path: "/api/v1/site/sitemap?page=2", count: 0, status: http.StatusNotFound, code: "SITEMAP_PAGE_NOT_FOUND"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeCatalog{sitemapCount: test.count, sitemapEntries: []catalog.SitemapEntry{}}
			response := httptest.NewRecorder()
			New(Options{Catalog: repository}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("unexpected sitemap error: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPerformerDetailIncludesPublishedAliases(t *testing.T) {
	repository := &fakeCatalog{performer: catalog.PerformerDetail{
		PerformerSummary: catalog.PerformerSummary{ID: "10000000-0000-4000-8000-000000000001", Name: "Test Person", Slug: "test-person"},
		Aliases:          []string{"Alias One", "Alias Two"}, ActivityStatus: "unknown",
	}}
	response := httptest.NewRecorder()
	New(Options{Catalog: repository}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/performers/test-person", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"aliases":["Alias One","Alias Two"]`) {
		t.Fatalf("expected aliases in performer detail, got %d: %s", response.Code, response.Body.String())
	}
}

func TestPublicDetailsExposePublicationTimeAndLinkedStudio(t *testing.T) {
	publishedAt := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC)
	studio := catalog.StudioSummary{ID: "30000000-0000-4000-8000-000000000001", Name: "测试厂牌", Slug: "test-studio"}
	repository := &fakeCatalog{
		detail: catalog.WorkDetail{
			WorkSummary:     catalog.WorkSummary{ID: "10000000-0000-4000-8000-000000000001", Code: "TEST-001", Title: "测试作品", Slug: "test-work", StudioName: studio.Name},
			PublicUpdatedAt: &publishedAt, Studio: &studio,
		},
		performer: catalog.PerformerDetail{
			PerformerSummary: catalog.PerformerSummary{ID: "20000000-0000-4000-8000-000000000001", Name: "测试人物", Slug: "test-person"},
			PublicUpdatedAt:  &publishedAt, ActivityStatus: "unknown",
		},
		studio: catalog.StudioDetail{StudioSummary: studio, PublicUpdatedAt: &publishedAt},
	}
	handler := New(Options{Catalog: repository})
	for _, testCase := range []struct {
		path       string
		wantStudio bool
	}{
		{path: "/api/v1/works/test-work", wantStudio: true},
		{path: "/api/v1/performers/test-person"},
		{path: "/api/v1/studios/test-studio"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"public_updated_at":"2026-09-16T07:00:00Z"`) {
			t.Fatalf("%s did not expose publication time: %d %s", testCase.path, response.Code, response.Body.String())
		}
		if testCase.wantStudio && (!strings.Contains(response.Body.String(), `"studio_name":"测试厂牌"`) || !strings.Contains(response.Body.String(), `"href":"/studios/test-studio"`)) {
			t.Fatalf("work detail lost compatible studio fields: %s", response.Body.String())
		}
	}
}

func TestSearchRejectsEmptyCode(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=-", nil))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"INVALID_QUERY"`) || repository.searchCalls != 0 || repository.searchMetricCalls != 0 {
		t.Fatalf("invalid query must be distinguishable and side-effect free: %s", response.Body.String())
	}
}

func TestPublicPaginationRejectsMalformedCursor(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/works/recent?cursor=not-a-cursor", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed cursor, got %d", response.Code)
	}
}

func TestPublicPaginationCursorIsBoundToScope(t *testing.T) {
	values := make([]catalog.WorkSummary, publicPageSize+1)
	for index := range values {
		values[index].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", index+1)
	}
	_, cursor := pageValues(values, "works.recent")
	if cursor == nil {
		t.Fatal("expected next cursor")
	}
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	target := "/api/v1/performers?cursor=" + url.QueryEscape(*cursor)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-scope cursor, got %d", response.Code)
	}
}

func TestPopularWorksValidatesRange(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/works/popular?range=invalid", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestDiscoveryMixUsesConfiguredRatio(t *testing.T) {
	works := make([]catalog.WorkSummary, 8)
	for index := range works {
		works[index] = catalog.WorkSummary{ID: fmt.Sprintf("work-%d", index)}
	}
	performers := []catalog.PerformerSummary{{ID: "performer-1"}, {ID: "performer-2"}}
	items := mixDiscoveryItems(works, performers, catalog.DiscoveryMixRule{WorkSlots: 4, PerformerSlots: 1, RepeatWindow: 10})
	if len(items) != 10 || items[4].Type != "performer" || items[9].Type != "performer" {
		t.Fatalf("expected a 4:1 discovery mix, got %#v", items)
	}
}

func TestDiscoveryMixNormalizesInvalidRuntimeRule(t *testing.T) {
	works := []catalog.WorkSummary{{ID: "work-1"}, {ID: "work-2"}}
	performers := []catalog.PerformerSummary{{ID: "performer-1"}}

	// A malformed persisted row must still make progress. In particular, a
	// zero slot on either side used to make the outer loop spin forever while
	// source items remained.
	items := mixDiscoveryItems(works, performers, catalog.DiscoveryMixRule{})
	if len(items) != 3 {
		t.Fatalf("expected normalized rule to return available items, got %d", len(items))
	}
	workLimit, performerLimit := discoverySourceLimits(catalog.DiscoveryMixRule{})
	if workLimit != 8 || performerLimit != 4 {
		t.Fatalf("unexpected normalized source limits: works=%d performers=%d", workLimit, performerLimit)
	}
}

func TestWorkDetailIsReadOnly(t *testing.T) {
	repository := &fakeCatalog{detail: catalog.WorkDetail{WorkSummary: catalog.WorkSummary{
		ID: "10000000-0000-4000-8000-000000000001", Code: "TEST-001", Title: "测试作品", Slug: "test-001",
	}, RelatedWorks: []catalog.WorkSummary{{
		ID: "10000000-0000-4000-8000-000000000002", Code: "TEST-002", Title: "相关作品", Slug: "test-002",
	}}}}
	handler := New(Options{Catalog: repository})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/works/test-001", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if repository.pageViews != 0 {
		t.Fatalf("detail GET must not record page views, got %d", repository.pageViews)
	}
	var body workDetailResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode work detail: %v", err)
	}
	if len(body.RelatedWorks) != 1 || body.RelatedWorks[0].Code != "TEST-002" {
		t.Fatalf("expected related works in detail response, got %#v", body.RelatedWorks)
	}
}

func TestWorkDetailGroupsRenditionsUnderOneLogicalImage(t *testing.T) {
	image := responsiveCatalogImage()
	repository := &fakeCatalog{detail: catalog.WorkDetail{
		WorkSummary: catalog.WorkSummary{
			ID: "10000000-0000-4000-8000-000000000001", Code: "TEST-001", Title: "测试作品", Slug: "test-001",
		},
		Images: []catalog.Image{image},
	}}
	response := httptest.NewRecorder()
	New(Options{Catalog: repository}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/works/test-001", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body workDetailResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode work detail: %v", err)
	}
	if len(body.Images) != 1 || len(body.Images[0].Renditions) != 3 || body.Images[0].Renditions[2].Width != 960 {
		t.Fatalf("expected one logical image with three derivatives, got %#v", body.Images)
	}
}

func responsiveCatalogImage() catalog.Image {
	renditions := []catalog.ImageRendition{
		{URL: "https://media.example.test/cover-w320.webp", Rendition: "w320", Width: 320, Height: 200, MimeType: "image/webp"},
		{URL: "https://media.example.test/cover-w640.webp", Rendition: "w640", Width: 640, Height: 400, MimeType: "image/webp"},
		{URL: "https://media.example.test/cover-w960.webp", Rendition: "w960", Width: 960, Height: 600, MimeType: "image/webp"},
	}
	return catalog.Image{
		URL: renditions[1].URL, Rendition: renditions[1].Rendition, Width: renditions[1].Width,
		Height: renditions[1].Height, MimeType: renditions[1].MimeType, Renditions: renditions,
	}
}

func TestPageViewRecordsOneAggregate(t *testing.T) {
	repository := &fakeCatalog{}
	accounts := &recordingAccount{}
	handler := New(Options{Catalog: repository, Account: accounts})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/page-view", strings.NewReader(`{"content_type":"work","content_id":"10000000-0000-4000-8000-000000000001"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || repository.pageViews != 1 {
		t.Fatalf("expected one aggregate page view, got status=%d views=%d", response.Code, repository.pageViews)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("mutation responses must not be cached, got %q", response.Header().Get("Cache-Control"))
	}
	if accounts.historyCalls != 0 {
		t.Fatalf("anonymous page view must not write user history, got %d calls", accounts.historyCalls)
	}
}

func TestAuthenticatedPageViewAlsoRecordsPreciseHistory(t *testing.T) {
	const contentID = "10000000-0000-4000-8000-000000000001"
	now := time.Date(2026, 9, 8, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60))
	repository := &fakeCatalog{}
	accounts := &recordingAccount{}
	identityService := &fakeIdentityService{}
	identityService.user.ID = "20000000-0000-4000-8000-000000000001"
	handler := New(Options{Catalog: repository, Account: accounts, Identity: identityService, Now: func() time.Time { return now }})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/page-view", strings.NewReader(`{"content_type":"performer","content_id":"`+contentID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: "valid-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || repository.pageViews != 1 {
		t.Fatalf("expected aggregate and history page view, got status=%d views=%d", response.Code, repository.pageViews)
	}
	if accounts.historyCalls != 1 || accounts.userID != identityService.user.ID || accounts.contentType != "performer" || accounts.contentID != contentID {
		t.Fatalf("unexpected history write: %#v", accounts)
	}
	if !accounts.viewedAt.Equal(now.UTC()) {
		t.Fatalf("expected UTC history time %s, got %s", now.UTC(), accounts.viewedAt)
	}
}

func TestPageViewAcceptsAllPublicEntityTypes(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	for _, contentType := range []string{"work", "performer", "studio"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/page-view", strings.NewReader(`{"content_type":"`+contentType+`","content_id":"10000000-0000-4000-8000-000000000001"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("expected %s page view to return 204, got %d: %s", contentType, response.Code, response.Body.String())
		}
	}
	if len(repository.pageViewTypes) != 3 || repository.pageViewTypes[1] != "performer" || repository.pageViewTypes[2] != "studio" {
		t.Fatalf("unexpected page view types: %#v", repository.pageViewTypes)
	}
}

func TestPageViewRejectsUnknownEntityType(t *testing.T) {
	repository := &fakeCatalog{}
	handler := New(Options{Catalog: repository})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/page-view", strings.NewReader(`{"content_type":"media","content_id":"10000000-0000-4000-8000-000000000001"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || repository.pageViews != 0 {
		t.Fatalf("expected unknown page view type to be rejected, got status=%d views=%d", response.Code, repository.pageViews)
	}
}

func TestPublicReadDoesNotForcePrivateCachePolicy(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/home", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Header().Get("Cache-Control") == "no-store" {
		t.Fatal("public catalog reads must remain cacheable by the frontend")
	}
}

func TestUnknownRouteReturnsContractError(t *testing.T) {
	handler := New(Options{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
	var body errorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "NOT_FOUND" || body.Error.RequestID == "" {
		t.Fatalf("unexpected error response: %#v", body)
	}
}

func TestAccessLogUsesRouteTemplateInsteadOfIdentifiers(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := New(Options{Logger: logger})
	userID := "10000000-0000-4000-8000-000000000099"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+userID+"/role", nil))
	logLine := output.String()
	if strings.Contains(logLine, userID) || !strings.Contains(logLine, `"route":"/admin/v1/users/{userID}/role"`) {
		t.Fatalf("access log must use the route template: %s", logLine)
	}
}

func TestPanicLogDoesNotExposeRecoveredValue(t *testing.T) {
	var output bytes.Buffer
	server := &Server{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	handler := server.recoverMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("user@example.test verification code 123456")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", response.Code)
	}
	logLine := output.String()
	if strings.Contains(logLine, "user@example.test") || strings.Contains(logLine, "123456") || !strings.Contains(logLine, `"error_class":"internal"`) {
		t.Fatalf("panic log exposed recovered value or omitted classification: %s", logLine)
	}
}
