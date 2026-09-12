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
	performer         catalog.PerformerDetail
	pageViews         int
	pageViewTypes     []string
	searchMetricCalls int
	searchMetricZero  bool
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
	repository.searchRaw, repository.searchCode = raw, compact
	return repository.latest, repository.repositoryErr
}
func (repository *fakeCatalog) ListPerformers(context.Context, string, int) ([]catalog.PerformerSummary, error) {
	return []catalog.PerformerSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) PerformerBySlug(context.Context, string) (catalog.PerformerDetail, error) {
	return repository.performer, repository.repositoryErr
}
func (repository *fakeCatalog) ListStudios(context.Context, string, int) ([]catalog.StudioSummary, error) {
	return []catalog.StudioSummary{}, repository.repositoryErr
}
func (repository *fakeCatalog) StudioBySlug(context.Context, string) (catalog.StudioDetail, error) {
	return catalog.StudioDetail{}, repository.repositoryErr
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
func (repository *fakeCatalog) SitemapEntries(context.Context, string, int) ([]catalog.SitemapEntry, error) {
	return []catalog.SitemapEntry{{EntityType: "work", Slug: "test-001", UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}}, repository.repositoryErr
}
func (repository *fakeCatalog) RedirectTarget(context.Context, string) (catalog.RedirectTarget, error) {
	return catalog.RedirectTarget{}, catalog.ErrNotFound
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

func TestSitemapUsesPublishedRepository(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/site/sitemap", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"slug":"test-001"`) {
		t.Fatalf("expected published sitemap entry, got %d: %s", response.Code, response.Body.String())
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

func TestSearchRejectsEmptyCode(t *testing.T) {
	handler := New(Options{Catalog: &fakeCatalog{}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/search/works?q=-", nil))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
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
