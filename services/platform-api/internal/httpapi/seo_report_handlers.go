package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

const maxSEOReportBytes = 5 * 1024 * 1024
const maxSEOReportRows = 10000

type seoReportResponse struct {
	ID           string  `json:"id"`
	ReportType   string  `json:"report_type"`
	DatasetKind  string  `json:"dataset_kind"`
	PropertyURI  string  `json:"property_uri"`
	PeriodStart  string  `json:"period_start"`
	PeriodEnd    string  `json:"period_end"`
	ExportedAt   string  `json:"exported_at"`
	FileSHA256   string  `json:"file_sha256"`
	RowCount     int     `json:"row_count"`
	Status       string  `json:"status"`
	ErrorSummary *string `json:"error_summary"`
	ImportedBy   string  `json:"imported_by"`
	CreatedAt    string  `json:"created_at"`
}

func (s *Server) adminListSEOReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.SEOReportRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "SEO_REPORTS_UNAVAILABLE", "SEO 报表服务暂时不可用")
		return
	}
	items, err := repository.ListSEOReports(r.Context(), 50)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := make([]seoReportResponse, 0, len(items))
	for _, item := range items {
		response = append(response, seoReportItem(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": response})
}

func (s *Server) adminImportSEOReport(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	repository, ok := s.operations.(operations.SEOReportRepository)
	if !ok {
		writeError(w, r, http.StatusServiceUnavailable, "SEO_REPORTS_UNAVAILABLE", "SEO 报表服务暂时不可用")
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeError(w, r, http.StatusUnsupportedMediaType, "INVALID_SEO_REPORT", "请上传 multipart CSV 文件")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSEOReportBytes+128*1024)
	if err := r.ParseMultipartForm(128 * 1024); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT", "报表文件无法解析或超过 5 MiB")
		return
	}
	reportType, datasetKind := strings.TrimSpace(r.FormValue("report_type")), strings.TrimSpace(r.FormValue("dataset_kind"))
	propertyURI := strings.TrimSpace(r.FormValue("property_uri"))
	periodStart, startErr := time.Parse(time.DateOnly, r.FormValue("period_start"))
	periodEnd, endErr := time.Parse(time.DateOnly, r.FormValue("period_end"))
	exportedAt, exportedErr := time.Parse(time.RFC3339, r.FormValue("exported_at"))
	if !oneOf(reportType, "performance", "indexing") || !oneOf(datasetKind, "real", "demo") || !validSearchProperty(propertyURI) ||
		startErr != nil || endErr != nil || exportedErr != nil || periodEnd.Before(periodStart) || exportedAt.After(s.now().UTC().Add(5*time.Minute)) {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT_METADATA", "报表类型、站点、数据标识或时间区间不符合要求")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT", "缺少 CSV 文件")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") || strings.ContainsAny(header.Filename, `/\`) {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT", "文件名或扩展名不符合要求")
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSEOReportBytes+1))
	if err != nil || len(content) == 0 || len(content) > maxSEOReportBytes {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT", "CSV 必须非空且不超过 5 MiB")
		return
	}
	rows, err := parseSEOReportCSV(reportType, content)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_SEO_REPORT_ROWS", "CSV 表头、行数或字段内容不符合正式导入格式")
		return
	}
	digest := sha256.Sum256(content)
	item, err := repository.ImportSEOReport(r.Context(), operations.SEOReportImportInput{ReportType: reportType,
		DatasetKind: datasetKind, PropertyURI: propertyURI, PeriodStart: periodStart, PeriodEnd: periodEnd,
		ExportedAt: exportedAt.UTC(), FileSHA256: hex.EncodeToString(digest[:]), Rows: rows},
		user.ID, requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, seoReportItem(item))
}

func parseSEOReportCSV(reportType string, content []byte) ([]operations.SEOReportRow, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 || len(records)-1 > maxSEOReportRows {
		return nil, errors.New("invalid csv")
	}
	expected := []string{"query", "page", "country", "device", "date", "clicks", "impressions", "ctr", "position"}
	if reportType == "indexing" {
		expected = []string{"page", "status", "reason", "last_crawled"}
	}
	if !equalCSVHeader(records[0], expected) {
		return nil, errors.New("invalid header")
	}
	rows := make([]operations.SEOReportRow, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != len(expected) {
			return nil, errors.New("invalid row")
		}
		for _, value := range record {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				return nil, errors.New("formula cell")
			}
		}
		if reportType == "performance" {
			clicks, err1 := strconv.ParseFloat(record[5], 64)
			impressions, err2 := strconv.ParseFloat(record[6], 64)
			ctr, err3 := strconv.ParseFloat(strings.TrimSuffix(record[7], "%"), 64)
			position, err4 := strconv.ParseFloat(record[8], 64)
			if strings.HasSuffix(record[7], "%") {
				ctr /= 100
			}
			if err1 != nil || err2 != nil || err3 != nil || err4 != nil || clicks < 0 || impressions < 0 || ctr < 0 || ctr > 1 || position < 0 {
				return nil, errors.New("invalid metrics")
			}
			rows = append(rows, operations.SEOReportRow{Dimensions: map[string]string{"query": record[0], "page": record[1], "country": record[2], "device": record[3], "date": record[4]},
				Metrics: map[string]float64{"clicks": clicks, "impressions": impressions, "ctr": ctr, "position": position}})
		} else {
			rows = append(rows, operations.SEOReportRow{Dimensions: map[string]string{"page": record[0], "status": record[1], "reason": record[2], "last_crawled": record[3]}, Metrics: map[string]float64{}})
		}
	}
	return rows, nil
}

func equalCSVHeader(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if strings.ToLower(strings.TrimSpace(actual[index])) != expected[index] {
			return false
		}
	}
	return true
}

func validSearchProperty(value string) bool {
	if strings.HasPrefix(value, "sc-domain:") {
		host := strings.TrimPrefix(value, "sc-domain:")
		return host != "" && !strings.ContainsAny(host, `/\\:@?#`) && strings.Contains(host, ".")
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/")
}

func seoReportItem(item operations.SEOReportImport) seoReportResponse {
	return seoReportResponse{ID: item.ID, ReportType: item.ReportType, DatasetKind: item.DatasetKind, PropertyURI: item.PropertyURI,
		PeriodStart: item.PeriodStart.Format(time.DateOnly), PeriodEnd: item.PeriodEnd.Format(time.DateOnly), ExportedAt: item.ExportedAt.UTC().Format(time.RFC3339),
		FileSHA256: item.FileSHA256, RowCount: item.RowCount, Status: item.Status, ErrorSummary: item.ErrorSummary,
		ImportedBy: item.ImportedBy, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339)}
}
