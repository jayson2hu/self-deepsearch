package mediareconcile

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func completeReport(run Run) Report {
	return Report{RunID: run.ID, ExpectedCount: len(run.Objects), IssueSamples: map[string][]string{
		"missing_s3": {}, "corrupt_s3": {}, "missing_backup": {}, "corrupt_backup": {}, "orphan_s3": {}, "orphan_backup": {},
	}}
}

func reportRun() Run {
	return Run{ID: "10000000-0000-4000-8000-000000000001", Objects: []Object{{StorageScope: "private", StorageKey: "media-master/a", BackupPath: "media-master/a", SHA256: strings.Repeat("a", 64), ByteSize: 1}}}
}

func TestIncompleteReconciliationNeverMarksRunClean(t *testing.T) {
	run := reportRun()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"run_id":"`+run.ID+`","expected_count":1,"issue_samples":{}}`)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, strings.Repeat("r", 32), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{run: run}
	runner := Runner{Repository: repository, Client: client, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	runner.runOnce(context.Background())
	if repository.completed != nil || repository.failed != "invalid_report" {
		t.Fatalf("incomplete report was accepted as clean: completed=%v failed=%q", repository.completed, repository.failed)
	}
}

func TestHTTPReportRequiresCompleteBoundedJSON(t *testing.T) {
	run := reportRun()
	encoded, _ := json.Marshal(completeReport(run))
	healthy := string(encoded)
	for _, test := range []struct {
		name              string
		status            int
		contentType, body string
		valid             bool
	}{
		{"healthy", 200, "application/json; charset=utf-8", healthy, true},
		{"accepted_not_complete", 202, "application/json", healthy, false},
		{"html_type", 200, "text/html", healthy, false},
		{"missing_type", 200, "", healthy, false},
		{"trailing_json", 200, "application/json", healthy + `{}`, false},
		{"trailing_junk", 200, "application/json", healthy + `x`, false},
		{"oversized_whitespace", 200, "application/json", healthy + strings.Repeat(" ", 64*1024), false},
		{"invalid_utf8", 200, "application/json", strings.Replace(healthy, `"run_id":"`, "\"run_id\":\"\xff", 1), false},
		{"missing_counter", 200, "application/json", strings.Replace(healthy, `"missing_s3_count":0,`, "", 1), false},
		{"null_counter", 200, "application/json", strings.Replace(healthy, `"missing_s3_count":0`, `"missing_s3_count":null`, 1), false},
		{"duplicate_counter", 200, "application/json", strings.Replace(healthy, `"missing_s3_count":0`, `"missing_s3_count":1,"missing_s3_count":0`, 1), false},
		{"null_samples", 200, "application/json", strings.Replace(healthy, `"missing_s3":[]`, `"missing_s3":null`, 1), false},
		{"unexpected_field", 200, "application/json", strings.TrimSuffix(healthy, "}") + `,"unexpected":true}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.contentType != "" {
					w.Header().Set("Content-Type", test.contentType)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := NewHTTPClient(server.URL, strings.Repeat("r", 32), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Reconcile(context.Background(), run)
			if (err == nil) != test.valid {
				t.Fatalf("response validity: %v", err)
			}
		})
	}
}

func TestReportRejectsImpossibleCountsAndUnrelatedSamples(t *testing.T) {
	run := reportRun()
	for _, test := range []struct {
		name   string
		mutate func(*Report)
	}{
		{"wrong_run", func(r *Report) { r.RunID = "different" }},
		{"wrong_count", func(r *Report) { r.ExpectedCount = 0 }},
		{"negative_count", func(r *Report) { r.MissingS3Count = -1 }},
		{"counter_overflow", func(r *Report) { r.OrphanS3Count = 1 << 31 }},
		{"impossible_total", func(r *Report) { r.MissingS3Count = 1; r.CorruptS3Count = 1 }},
		{"unknown_category", func(r *Report) { delete(r.IssueSamples, "missing_s3"); r.IssueSamples["unknown"] = []string{} }},
		{"missing_category", func(r *Report) { delete(r.IssueSamples, "missing_s3") }},
		{"unexpected_missing_key", func(r *Report) { r.MissingS3Count = 1; r.IssueSamples["missing_s3"] = []string{"media-master/other"} }},
		{"known_orphan_key", func(r *Report) {
			r.OrphanS3Count = 1
			r.IssueSamples["orphan_s3"] = []string{run.Objects[0].StorageKey}
		}},
		{"unsafe_orphan_key", func(r *Report) {
			r.OrphanS3Count = 1
			r.IssueSamples["orphan_s3"] = []string{"media-master/../outside"}
		}},
		{"too_many_samples", func(r *Report) {
			r.OrphanS3Count = 1
			r.IssueSamples["orphan_s3"] = []string{"media-master/a1", "media-master/a2"}
		}},
		{"duplicate_samples", func(r *Report) {
			r.OrphanS3Count = 2
			r.IssueSamples["orphan_s3"] = []string{"media-master/a1", "media-master/a1"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := completeReport(run)
			test.mutate(&report)
			if report.validFor(run) {
				t.Fatal("invalid report passed semantic validation")
			}
		})
	}
	for _, run := range []Run{reportRun(), {ID: reportRun().ID, Objects: []Object{}}} {
		report := completeReport(run)
		if !report.validFor(run) {
			t.Fatal("complete clean report rejected")
		}
		// Samples may be byte-truncated, but issue totals must remain intact.
		report.OrphanS3Count = 2000000000
		report.OrphanBackupCount = 2000000000
		if !report.validFor(run) || report.IssueCount() != 4000000000 {
			t.Fatal("issue count overflow or sample truncation rejected")
		}
	}
}
