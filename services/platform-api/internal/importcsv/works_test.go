package importcsv

import (
	"strings"
	"testing"
)

func TestParseWorksAcceptsDocumentedColumns(t *testing.T) {
	report := ParseWorks(strings.NewReader("code,title,release_date,performer_ids\nAB-123,Test,2026-08-01,10000000-0000-4000-8000-000000000001\n"))
	if len(report.FileIssues) != 0 || report.ValidCount() != 1 || report.Rows[0].Code != "AB-123" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestParseWorksReportsRowAndFileErrors(t *testing.T) {
	report := ParseWorks(strings.NewReader("code,title,unexpected\nAB-123,Test,value\n"))
	if len(report.FileIssues) != 1 || report.FileIssues[0].Code != "UNKNOWN_HEADER" {
		t.Fatalf("expected unsupported header error: %#v", report)
	}

	report = ParseWorks(strings.NewReader("code,title,release_date\nAB-123,Test,not-a-date\nAB123,Again,2026-08-01\n"))
	if len(report.Rows) != 2 || len(report.Rows[0].Issues) == 0 || len(report.Rows[1].Issues) == 0 {
		t.Fatalf("expected date and duplicate errors: %#v", report)
	}
}
