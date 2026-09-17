package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
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

type fakeSEOIssues struct {
	*fakeSEOInventory
	update operations.SEOIssueUpdate
}

type fakeSEOReports struct {
	*fakeOperations
	input operations.SEOReportImportInput
}

func (repository *fakeSEOReports) ImportSEOReport(_ context.Context, input operations.SEOReportImportInput, _, _ string, now time.Time) (operations.SEOReportImport, error) {
	repository.input = input
	return operations.SEOReportImport{ID: "40000000-0000-4000-8000-000000000001", ReportType: input.ReportType,
		DatasetKind: input.DatasetKind, PropertyURI: input.PropertyURI, PeriodStart: input.PeriodStart, PeriodEnd: input.PeriodEnd,
		ExportedAt: input.ExportedAt, FileSHA256: input.FileSHA256, RowCount: len(input.Rows), Status: "imported",
		ImportedBy: "owner@example.test", CreatedAt: now}, repository.operationErr
}

func (repository *fakeSEOReports) ListSEOReports(context.Context, int) ([]operations.SEOReportImport, error) {
	return []operations.SEOReportImport{}, repository.operationErr
}

func (repository *fakeSEOIssues) RunSEOScan(_ context.Context, _, _ string, now time.Time) (operations.SEOScan, error) {
	return operations.SEOScan{ID: "20000000-0000-4000-8000-000000000001", Source: "manual", Status: "completed",
		ScannedCount: 10, IssueCount: 2, StartedAt: now, CompletedAt: &now}, repository.operationErr
}

func (repository *fakeSEOIssues) ListSEOScans(_ context.Context, _ int) ([]operations.SEOScan, error) {
	item, err := repository.RunSEOScan(context.Background(), "", "", time.Now())
	return []operations.SEOScan{item}, err
}

func (repository *fakeSEOIssues) ListSEOIssues(_ context.Context, _ string, _ int) ([]operations.SEOIssue, error) {
	now := time.Now()
	return []operations.SEOIssue{{ID: "30000000-0000-4000-8000-000000000001", RuleCode: "SEO_QUALITY_UNKNOWN",
		EntityType: "work", EntityID: "10000000-0000-4000-8000-000000000001", URLPath: "/works/test-001",
		Priority: "P1", Status: "open", Evidence: map[string]any{"exclusion_reason": "quality_unknown"}, FirstSeenAt: now, LastSeenAt: now, UpdatedAt: now}}, repository.operationErr
}

func (repository *fakeSEOIssues) UpdateSEOIssue(_ context.Context, id string, input operations.SEOIssueUpdate, _, _ string, now time.Time) (operations.SEOIssue, error) {
	repository.update = input
	return operations.SEOIssue{ID: id, RuleCode: "SEO_QUALITY_UNKNOWN", EntityType: "work",
		EntityID: "10000000-0000-4000-8000-000000000001", URLPath: "/works/test-001", Priority: "P1",
		Status: input.Status, Evidence: map[string]any{}, AssigneeID: input.AssigneeID, FirstSeenAt: now, LastSeenAt: now, UpdatedAt: now}, repository.operationErr
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

func TestSEOIssueRoutesRunBoundedScanAndValidateTransitionInput(t *testing.T) {
	repository := &fakeSEOIssues{fakeSEOInventory: &fakeSEOInventory{fakeOperations: &fakeOperations{}}}
	response := httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPost, "/admin/v1/seo/scans", `{}`))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"scanned_count":10`) {
		t.Fatalf("unexpected scan response %d: %s", response.Code, response.Body.String())
	}

	body := `{"status":"in_progress","assignee_id":"90000000-0000-4000-8000-000000000001","due_at":null,"note":"开始处理"}`
	response = httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPatch,
		"/admin/v1/seo/issues/30000000-0000-4000-8000-000000000001", body))
	if response.Code != http.StatusOK || repository.update.Status != "in_progress" || repository.update.Note != "开始处理" {
		t.Fatalf("unexpected issue update %d: %s update=%+v", response.Code, response.Body.String(), repository.update)
	}

	response = httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, authenticatedAdminRequest(http.MethodPatch,
		"/admin/v1/seo/issues/30000000-0000-4000-8000-000000000001", `{"status":"closed","assignee_id":null,"due_at":null,"note":"x"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("short issue evidence returned %d: %s", response.Code, response.Body.String())
	}
}

func TestSEOReportImportValidatesOfficialShapeAndOwnerRole(t *testing.T) {
	repository := &fakeSEOReports{fakeOperations: &fakeOperations{}}
	request := seoReportRequest(t, "performance.csv", "query,page,country,device,date,clicks,impressions,ctr,position\n幕鉴,https://example.test/works/a,CN,DESKTOP,2026-09-16,1,10,10%,2.5\n")
	response := httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || len(repository.input.Rows) != 1 || repository.input.DatasetKind != "demo" ||
		repository.input.Rows[0].Metrics["ctr"] != 0.1 {
		t.Fatalf("unexpected report import %d: %s input=%+v", response.Code, response.Body.String(), repository.input)
	}

	request = seoReportRequest(t, "performance.csv", "query,page,country,device,date,clicks,impressions,ctr,position\n=formula,https://example.test/works/a,CN,DESKTOP,2026-09-16,1,10,0.1,2.5\n")
	response = httptest.NewRecorder()
	adminHandler("owner", repository).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("formula CSV returned %d: %s", response.Code, response.Body.String())
	}

	request = seoReportRequest(t, "performance.csv", "query,page,country,device,date,clicks,impressions,ctr,position\n幕鉴,https://example.test/works/a,CN,DESKTOP,2026-09-16,1,10,0.1,2.5\n")
	response = httptest.NewRecorder()
	adminHandler("admin", repository).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("admin import returned %d: %s", response.Code, response.Body.String())
	}
}

func seoReportRequest(t *testing.T, filename, csvContent string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"report_type": "performance", "dataset_kind": "demo", "property_uri": "https://example.test/", "period_start": "2026-09-01", "period_end": "2026-09-16", "exported_at": "2026-09-17T00:00:00Z"} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(csvContent)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := authenticatedAdminRequest(http.MethodPost, "/admin/v1/seo/reports/import", "")
	request.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	request.ContentLength = int64(body.Len())
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
