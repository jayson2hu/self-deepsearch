package importcsv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	MaxBytes = 1 << 20
	MaxRows  = 500
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type WorkRow struct {
	RowNumber     int      `json:"row_number"`
	Code          string   `json:"code"`
	Title         string   `json:"title"`
	TitleOriginal string   `json:"title_original,omitempty"`
	ReleaseDate   string   `json:"release_date,omitempty"`
	StudioID      string   `json:"studio_id,omitempty"`
	PerformerIDs  []string `json:"performer_ids,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Issues        []Issue  `json:"issues"`
}

type Report struct {
	Rows       []WorkRow `json:"rows"`
	FileIssues []Issue   `json:"file_issues"`
}

func (report Report) ValidCount() int {
	count := 0
	for _, row := range report.Rows {
		if len(row.Issues) == 0 {
			count++
		}
	}
	return count
}

func ParseWorks(reader io.Reader) Report {
	limited := &io.LimitedReader{R: reader, N: MaxBytes + 1}
	parser := csv.NewReader(limited)
	parser.FieldsPerRecord = -1
	parser.TrimLeadingSpace = true
	header, err := parser.Read()
	if err != nil {
		return Report{Rows: []WorkRow{}, FileIssues: []Issue{csvIssue(err)}}
	}
	if limited.N == 0 {
		return fileIssue("FILE_TOO_LARGE", "CSV 文件不能超过 1 MiB")
	}
	columns, issues := parseHeader(header)
	if len(issues) > 0 {
		return Report{Rows: []WorkRow{}, FileIssues: issues}
	}
	report := Report{Rows: make([]WorkRow, 0), FileIssues: make([]Issue, 0)}
	seenCodes := make(map[string]int)
	for rowNumber := 2; ; rowNumber++ {
		record, err := parser.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			report.FileIssues = append(report.FileIssues, csvIssue(err))
			break
		}
		if limited.N == 0 {
			return fileIssue("FILE_TOO_LARGE", "CSV 文件不能超过 1 MiB")
		}
		if blankRecord(record) {
			continue
		}
		if len(report.Rows) >= MaxRows {
			report.FileIssues = append(report.FileIssues, Issue{Code: "TOO_MANY_ROWS", Message: "CSV 最多包含 500 条非空数据"})
			break
		}
		row := workRow(rowNumber, record, columns)
		compact := compactCode(row.Code)
		if previous, exists := seenCodes[compact]; compact != "" && exists {
			row.Issues = append(row.Issues, Issue{Field: "code", Code: "DUPLICATE_IN_FILE", Message: fmt.Sprintf("与第 %d 行番号重复", previous)})
		} else if compact != "" {
			seenCodes[compact] = rowNumber
		}
		report.Rows = append(report.Rows, row)
	}
	return report
}

func parseHeader(header []string) (map[string]int, []Issue) {
	columns := make(map[string]int, len(header))
	allowed := map[string]bool{"code": true, "title": true, "title_original": true, "release_date": true, "studio_id": true, "performer_ids": true, "summary": true}
	issues := make([]Issue, 0)
	for index, raw := range header {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff")))
		if !allowed[name] {
			issues = append(issues, Issue{Field: name, Code: "UNKNOWN_HEADER", Message: "存在不支持的 CSV 列"})
			continue
		}
		if _, exists := columns[name]; exists {
			issues = append(issues, Issue{Field: name, Code: "DUPLICATE_HEADER", Message: "CSV 列名不能重复"})
		}
		columns[name] = index
	}
	for _, required := range []string{"code", "title"} {
		if _, exists := columns[required]; !exists {
			issues = append(issues, Issue{Field: required, Code: "MISSING_HEADER", Message: "缺少必填 CSV 列"})
		}
	}
	return columns, issues
}

func workRow(rowNumber int, record []string, columns map[string]int) WorkRow {
	value := func(name string) string {
		index, exists := columns[name]
		if !exists || index >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[index])
	}
	row := WorkRow{RowNumber: rowNumber, Code: value("code"), Title: value("title"), TitleOriginal: value("title_original"),
		ReleaseDate: value("release_date"), StudioID: value("studio_id"), Summary: value("summary"), Issues: make([]Issue, 0)}
	if performers := value("performer_ids"); performers != "" {
		for _, id := range strings.Split(performers, "|") {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				row.PerformerIDs = append(row.PerformerIDs, trimmed)
			}
		}
	}
	validateRow(&row)
	return row
}

func validateRow(row *WorkRow) {
	if len([]rune(row.Code)) < 1 || len([]rune(row.Code)) > 100 {
		row.Issues = append(row.Issues, Issue{Field: "code", Code: "INVALID_CODE", Message: "番号长度必须为 1 至 100 个字符"})
	}
	if len([]rune(row.Title)) < 1 || len([]rune(row.Title)) > 500 {
		row.Issues = append(row.Issues, Issue{Field: "title", Code: "INVALID_TITLE", Message: "标题长度必须为 1 至 500 个字符"})
	}
	if len([]rune(row.TitleOriginal)) > 500 || len([]rune(row.Summary)) > 5000 {
		row.Issues = append(row.Issues, Issue{Field: "text", Code: "TEXT_TOO_LONG", Message: "原文标题或简介超过长度限制"})
	}
	if row.ReleaseDate != "" {
		if _, err := time.Parse(time.DateOnly, row.ReleaseDate); err != nil {
			row.Issues = append(row.Issues, Issue{Field: "release_date", Code: "INVALID_DATE", Message: "发行日期必须使用 YYYY-MM-DD"})
		}
	}
	if row.StudioID != "" && !uuidPattern.MatchString(row.StudioID) {
		row.Issues = append(row.Issues, Issue{Field: "studio_id", Code: "INVALID_UUID", Message: "厂牌 ID 必须是 UUID"})
	}
	if len(row.PerformerIDs) > 20 {
		row.Issues = append(row.Issues, Issue{Field: "performer_ids", Code: "TOO_MANY_PERFORMERS", Message: "每部作品最多关联 20 个人物"})
	}
	seen := make(map[string]struct{}, len(row.PerformerIDs))
	for _, id := range row.PerformerIDs {
		normalized := strings.ToLower(id)
		if !uuidPattern.MatchString(id) {
			row.Issues = append(row.Issues, Issue{Field: "performer_ids", Code: "INVALID_UUID", Message: "人物 ID 必须是 UUID，并使用 | 分隔"})
			break
		}
		if _, exists := seen[normalized]; exists {
			row.Issues = append(row.Issues, Issue{Field: "performer_ids", Code: "DUPLICATE_PERFORMER", Message: "同一行人物 ID 不能重复"})
			break
		}
		seen[normalized] = struct{}{}
	}
}

func compactCode(value string) string {
	var result strings.Builder
	for _, character := range strings.ToUpper(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func blankRecord(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func csvIssue(err error) Issue {
	if errors.Is(err, io.EOF) {
		return Issue{Code: "EMPTY_FILE", Message: "CSV 文件为空"}
	}
	return Issue{Code: "INVALID_CSV", Message: "CSV 格式不正确"}
}

func fileIssue(code, message string) Report {
	return Report{Rows: []WorkRow{}, FileIssues: []Issue{{Code: code, Message: message}}}
}
