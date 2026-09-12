package mediareconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maximumReportBytes = 64 * 1024

// A transport 2xx or a missing integer's Go zero value is not evidence of a
// complete inventory inspection. Require every field exactly once.
func decodeReport(response *http.Response, run Run) (Report, error) {
	empty := Report{}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return empty, ErrInvalidReport
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumReportBytes+1))
	if err != nil || len(body) > maximumReportBytes || !utf8.Valid(body) {
		return empty, ErrInvalidReport
	}
	fields, err := uniqueObject(body)
	if err != nil || len(fields) != 9 {
		return empty, ErrInvalidReport
	}
	var report Report
	targets := map[string]any{
		"run_id": &report.RunID, "expected_count": &report.ExpectedCount,
		"missing_s3_count": &report.MissingS3Count, "corrupt_s3_count": &report.CorruptS3Count,
		"missing_backup_count": &report.MissingBackupCount, "corrupt_backup_count": &report.CorruptBackupCount,
		"orphan_s3_count": &report.OrphanS3Count, "orphan_backup_count": &report.OrphanBackupCount,
	}
	for key, target := range targets {
		raw, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, target) != nil {
			return empty, ErrInvalidReport
		}
	}
	samples, err := uniqueObject(fields["issue_samples"])
	if err != nil {
		return empty, ErrInvalidReport
	}
	report.IssueSamples = make(map[string][]string, len(samples))
	for key, raw := range samples {
		var values []string
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return empty, ErrInvalidReport
		}
		report.IssueSamples[key] = values
	}
	if !report.validFor(run) {
		return empty, ErrInvalidReport
	}
	return report, nil
}

func uniqueObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrInvalidReport
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, ErrInvalidReport
		}
		if _, exists := fields[key]; exists {
			return nil, ErrInvalidReport
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return nil, ErrInvalidReport
		}
		fields[key] = raw
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, ErrInvalidReport
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidReport
	}
	return fields, nil
}

func (report Report) validFor(run Run) bool {
	if run.ID == "" || report.RunID != run.ID || len(run.Objects) > maximumObjects || report.ExpectedCount != len(run.Objects) || len(report.IssueSamples) != 6 {
		return false
	}
	counts := map[string]int{
		"missing_s3": report.MissingS3Count, "corrupt_s3": report.CorruptS3Count,
		"missing_backup": report.MissingBackupCount, "corrupt_backup": report.CorruptBackupCount,
		"orphan_s3": report.OrphanS3Count, "orphan_backup": report.OrphanBackupCount,
	}
	expected := make(map[string]bool, len(run.Objects))
	for _, object := range run.Objects {
		expected[object.StorageKey] = true
	}
	for category, count := range counts {
		orphan := strings.HasPrefix(category, "orphan_")
		if count < 0 || count > 1<<31-1 || (!orphan && count > report.ExpectedCount) {
			return false
		}
		samples, ok := report.IssueSamples[category]
		if !ok || samples == nil || len(samples) > 50 || len(samples) > count {
			return false
		}
		seen := make(map[string]bool, len(samples))
		for _, key := range samples {
			if !validSampleKey(key) || seen[key] || expected[key] == orphan {
				return false
			}
			seen[key] = true
		}
	}
	// An object cannot be both absent and corrupt in one store observation.
	if report.MissingS3Count+report.CorruptS3Count > report.ExpectedCount || report.MissingBackupCount+report.CorruptBackupCount > report.ExpectedCount {
		return false
	}
	for _, store := range []string{"s3", "backup"} {
		missing := make(map[string]bool)
		for _, key := range report.IssueSamples["missing_"+store] {
			missing[key] = true
		}
		for _, key := range report.IssueSamples["corrupt_"+store] {
			if missing[key] {
				return false
			}
		}
	}
	return true
}

func validSampleKey(key string) bool {
	if key == "" || utf8.RuneCountInString(key) > 2048 || strings.TrimSpace(key) != key || strings.Contains(key, "\\") ||
		(!strings.HasPrefix(key, "media-master/") && !strings.HasPrefix(key, "media-public/")) {
		return false
	}
	for _, char := range key {
		if unicode.IsControl(char) {
			return false
		}
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
