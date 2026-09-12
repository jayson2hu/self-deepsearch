package httpapi

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"self-deepsearch/services/platform-api/internal/importcsv"
	"self-deepsearch/services/platform-api/internal/operations"
)

var importKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

type workCSVPreflightResponse struct {
	Rows         []importcsv.WorkRow `json:"rows"`
	FileIssues   []importcsv.Issue   `json:"file_issues"`
	TotalCount   int                 `json:"total_count"`
	ValidCount   int                 `json:"valid_count"`
	InvalidCount int                 `json:"invalid_count"`
}

type workImportBatchResponse struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	InputCount       int      `json:"input_count"`
	AcceptedCount    int      `json:"accepted_count"`
	RejectedCount    int      `json:"rejected_count"`
	EntityIDs        []string `json:"entity_ids"`
	IdempotentReplay bool     `json:"idempotent_replay"`
	ReceivedAt       string   `json:"received_at"`
	CompletedAt      string   `json:"completed_at"`
}

type workImportBatchListResponse struct {
	Items []workImportBatchResponse `json:"items"`
}

func (s *Server) adminPreflightWorkCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok {
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeError(w, r, http.StatusUnsupportedMediaType, "MULTIPART_REQUIRED", "请使用表单上传 CSV 文件")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, importcsv.MaxBytes+64*1024)
	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "CSV_TOO_LARGE", "CSV 文件不能超过 1 MiB")
			return
		}
		writeError(w, r, http.StatusBadRequest, "INVALID_MULTIPART", "上传表单格式不正确")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "CSV_REQUIRED", "请选择 CSV 文件")
		return
	}
	defer file.Close()
	report := importcsv.ParseWorks(file)
	valid := report.ValidCount()
	writeJSON(w, http.StatusOK, workCSVPreflightResponse{Rows: report.Rows, FileIssues: report.FileIssues,
		TotalCount: len(report.Rows), ValidCount: valid, InvalidCount: len(report.Rows) - valid})
}

func (s *Server) adminImportWorkCSV(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdminRole(w, r, "editor", "admin", "owner")
	if !ok || !s.requireOperations(w, r) {
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeError(w, r, http.StatusUnsupportedMediaType, "MULTIPART_REQUIRED", "请使用表单上传 CSV 文件")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !importKeyPattern.MatchString(key) {
		writeError(w, r, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键必须为 8 至 128 个安全字符")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, importcsv.MaxBytes+64*1024)
	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "CSV_TOO_LARGE", "CSV 文件不能超过 1 MiB")
			return
		}
		writeError(w, r, http.StatusBadRequest, "INVALID_MULTIPART", "上传表单格式不正确")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if len(reason) < 2 || len(reason) > 1000 {
		writeError(w, r, http.StatusBadRequest, "INVALID_REASON", "请填写 2 至 1000 个字符的导入理由")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "CSV_REQUIRED", "请选择 CSV 文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_CSV", "无法读取 CSV 文件")
		return
	}
	report := importcsv.ParseWorks(strings.NewReader(string(data)))
	valid := report.ValidCount()
	if len(report.FileIssues) > 0 || valid != len(report.Rows) || valid == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, workCSVPreflightResponse{Rows: report.Rows, FileIssues: report.FileIssues,
			TotalCount: len(report.Rows), ValidCount: valid, InvalidCount: len(report.Rows) - valid})
		return
	}
	rows := make([]operations.WorkImportRow, 0, len(report.Rows))
	for _, row := range report.Rows {
		input := operations.WorkInput{Code: row.Code, Title: row.Title, PerformerIDs: row.PerformerIDs, Reason: reason}
		input.TitleOriginal, input.StudioID, input.Summary = optionalText(row.TitleOriginal), optionalText(row.StudioID), optionalText(row.Summary)
		if row.ReleaseDate != "" {
			parsed, _ := time.Parse(time.DateOnly, row.ReleaseDate)
			input.ReleaseDate = &parsed
		}
		rows = append(rows, operations.WorkImportRow{RowNumber: row.RowNumber, Input: input})
	}
	digest := sha256.Sum256(append(append([]byte{}, data...), []byte("\n"+reason)...))
	item, err := s.operations.ImportWorks(r.Context(), operations.WorkImportInput{IdempotencyKey: key,
		RequestHash: "sha256:" + fmt.Sprintf("%x", digest), Rows: rows, Reason: reason}, user.ID,
		requestIDFromContext(r.Context()), s.now().UTC())
	if s.writeOperationsError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusCreated, workImportBatchResponse{ID: item.ID, Status: item.Status, InputCount: item.InputCount,
		AcceptedCount: item.AcceptedCount, RejectedCount: item.RejectedCount, EntityIDs: item.EntityIDs,
		IdempotentReplay: item.IdempotentReplay, ReceivedAt: item.ReceivedAt.UTC().Format(time.RFC3339), CompletedAt: item.CompletedAt.UTC().Format(time.RFC3339)})
}

func (s *Server) adminListWorkImports(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdminRole(w, r, "editor", "admin", "owner"); !ok || !s.requireOperations(w, r) {
		return
	}
	items, err := s.operations.ListWorkImportBatches(r.Context(), 100)
	if s.writeOperationsError(w, r, err) {
		return
	}
	response := workImportBatchListResponse{Items: make([]workImportBatchResponse, 0, len(items))}
	for _, item := range items {
		response.Items = append(response.Items, workImportBatchResponse{ID: item.ID, Status: item.Status, InputCount: item.InputCount,
			AcceptedCount: item.AcceptedCount, RejectedCount: item.RejectedCount, EntityIDs: item.EntityIDs,
			ReceivedAt: item.ReceivedAt.UTC().Format(time.RFC3339), CompletedAt: item.CompletedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, response)
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
