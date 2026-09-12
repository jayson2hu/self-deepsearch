package mediausage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func storageGroup(bucket string, at time.Time, payload, metadata, objects, uploads string) string {
	return fmt.Sprintf(`{"dimensions":{"bucketName":%q,"datetime":%q},"max":{"payloadSize":%s,"metadataSize":%s,"objectCount":%s,"uploadCount":%s}}`,
		bucket, at.Format(time.RFC3339Nano), payload, metadata, objects, uploads)
}

func storageEnvelope(groups string) string {
	return `{"data":{"viewer":{"accounts":[{"r2StorageAdaptiveGroups":[` + groups + `]}]}}}`
}

func makeStorageObservation(totalBytes uint64) *StorageObservation {
	peak := StorageDailyPeak{
		Date:         testNow.Format(time.DateOnly),
		SampledAt:    testNow,
		PayloadBytes: totalBytes,
		TotalBytes:   totalBytes,
		ObjectCount:  1,
	}
	byteDays, estimate, ok := storageEstimate([]StorageDailyPeak{peak})
	if !ok {
		panic("test storage estimate overflow")
	}
	return &StorageObservation{
		Window:                     shortWindow(),
		ReceivedAt:                 testNow,
		Rows:                       1,
		SampleTimes:                1,
		ObservedBuckets:            1,
		DailyPeaks:                 []StorageDailyPeak{peak},
		ObservedByteDays:           byteDays,
		EstimatedGBMonthMicrounits: estimate,
		EstimateBasis:              storageEstimateBasis,
		EstimateRounding:           storageEstimateRounding,
	}
}

func storageObservation() *StorageObservation { return makeStorageObservation(decimalGigabyteBytes) }

func TestFetchStorageSumsBucketsBeforeSelectingUTCDailyPeak(t *testing.T) {
	window := Window{Start: time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC), EndInclusive: testNow}
	t1 := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	groups := []string{
		storageGroup("bucket-b", t2, "200", "20", "2", "1"),
		storageGroup("bucket-a", t1, "100", "10", "1", "0"),
		storageGroup("bucket-a", t3, "400", "40", "4", "2"),
		storageGroup("bucket-b", t1, "200", "20", "2", "1"),
		storageGroup("bucket-a", t2, "300", "30", "3", "2"),
		storageGroup("bucket-b", t3, "100", "10", "1", "0"),
	}
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing expected request method/headers")
		}
		var payload struct {
			Query     string
			Variables map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Variables["accountTag"] != testAccount || len(payload.Variables) != 3 ||
			!strings.Contains(payload.Query, "r2StorageAdaptiveGroups") || !strings.Contains(payload.Query, "bucketName datetime") ||
			!strings.Contains(payload.Query, "payloadSize metadataSize objectCount uploadCount") ||
			strings.Contains(payload.Query, "bucketName:") || strings.Contains(payload.Query, testToken) {
			t.Errorf("unsafe or incomplete storage query: %s", payload.Query)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, storageEnvelope(strings.Join(groups, ",")))
	})
	o, err := c.FetchStorage(context.Background(), window)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || o.Rows != 6 || o.SampleTimes != 3 || o.ObservedBuckets != 2 || !o.ReceivedAt.Equal(testNow) || o.Window != window {
		t.Fatalf("unexpected storage observation: %+v calls=%d", o, calls)
	}
	if len(o.DailyPeaks) != 2 || o.DailyPeaks[0].TotalBytes != 550 || o.DailyPeaks[0].SampledAt != t2 ||
		o.DailyPeaks[0].PayloadBytes != 500 || o.DailyPeaks[0].MetadataBytes != 50 || o.DailyPeaks[0].ObjectCount != 5 || o.DailyPeaks[0].UploadCount != 3 ||
		o.DailyPeaks[1].TotalBytes != 550 || o.DailyPeaks[1].SampledAt != t3 || o.ObservedByteDays != "1100" || o.EstimatedGBMonthMicrounits != 1 {
		t.Fatalf("bucket sum or daily peak is wrong: %+v", o)
	}
}

func TestStorageParserRejectsPartialAmbiguousAndIncompleteEvidence(t *testing.T) {
	w := shortWindow()
	at := testNow.Add(-30 * time.Minute)
	valid := storageGroup("bucket-a", at, "1", "2", "3", "4")
	otherDayWindow := Window{Start: testNow.Add(-25 * time.Hour), EndInclusive: testNow}
	cases := []struct {
		name string
		body string
		w    Window
		want error
	}{
		{"malformed", `{"data":`, w, ErrResponse},
		{"trailing", storageEnvelope(valid) + `{}`, w, ErrResponse},
		{"duplicate_errors", `{"errors":[{}],"errors":[],"data":{}}`, w, ErrResponse},
		{"partial", `{"errors":[{"message":"provider detail"}],"data":{"viewer":{"accounts":[]}}}`, w, ErrPartial},
		{"data_null", `{"data":null}`, w, ErrResponse},
		{"viewer_null", `{"data":{"viewer":null}}`, w, ErrResponse},
		{"account_missing", `{"data":{"viewer":{"accounts":[]}}}`, w, ErrResponse},
		{"accounts_multiple", `{"data":{"viewer":{"accounts":[{},{}]}}}`, w, ErrResponse},
		{"groups_missing", `{"data":{"viewer":{"accounts":[{}]}}}`, w, ErrResponse},
		{"groups_null", `{"data":{"viewer":{"accounts":[{"r2StorageAdaptiveGroups":null}]}}}`, w, ErrResponse},
		{"empty", storageEnvelope(""), w, ErrNoData},
		{"row_null", storageEnvelope("null"), w, ErrResponse},
		{"max_missing", storageEnvelope(`{"dimensions":{"bucketName":"bucket-a","datetime":"2026-09-11T09:30:00Z"}}`), w, ErrResponse},
		{"dimensions_missing", storageEnvelope(`{"max":{"payloadSize":1,"metadataSize":2,"objectCount":3,"uploadCount":4}}`), w, ErrResponse},
		{"field_missing", storageEnvelope(storageGroup("bucket-a", at, "1", "2", "3", "null")), w, ErrResponse},
		{"bucket_invalid", storageEnvelope(storageGroup("bad bucket", at, "1", "2", "3", "4")), w, ErrResponse},
		{"datetime_invalid", storageEnvelope(`{"dimensions":{"bucketName":"bucket-a","datetime":"not-a-date"},"max":{"payloadSize":1,"metadataSize":2,"objectCount":3,"uploadCount":4}}`), w, ErrResponse},
		{"datetime_outside", storageEnvelope(storageGroup("bucket-a", testNow.Add(time.Second), "1", "2", "3", "4")), w, ErrResponse},
		{"negative", storageEnvelope(storageGroup("bucket-a", at, "-1", "2", "3", "4")), w, ErrResponse},
		{"fractional", storageEnvelope(storageGroup("bucket-a", at, "1.5", "2", "3", "4")), w, ErrResponse},
		{"exponent", storageEnvelope(storageGroup("bucket-a", at, "1e3", "2", "3", "4")), w, ErrResponse},
		{"string_number", storageEnvelope(storageGroup("bucket-a", at, `"1"`, "2", "3", "4")), w, ErrResponse},
		{"uint_overflow", storageEnvelope(storageGroup("bucket-a", at, "18446744073709551616", "2", "3", "4")), w, ErrResponse},
		{"duplicate_bucket_time", storageEnvelope(valid + "," + valid), w, ErrResponse},
		{"bucket_missing_at_sample", storageEnvelope(valid + "," + storageGroup("bucket-a", at.Add(10*time.Minute), "1", "2", "3", "4") + "," + storageGroup("bucket-b", at.Add(10*time.Minute), "1", "2", "3", "4")), w, ErrStorageCoverage},
		{"point_payload_overflow", storageEnvelope(storageGroup("bucket-a", at, fmt.Sprint(uint64(math.MaxUint64)), "0", "0", "0") + "," + storageGroup("bucket-b", at, "1", "0", "0", "0")), w, ErrResponse},
		{"point_object_overflow", storageEnvelope(storageGroup("bucket-a", at, "0", "0", fmt.Sprint(uint64(math.MaxUint64)), "0") + "," + storageGroup("bucket-b", at, "0", "0", "1", "0")), w, ErrResponse},
		{"total_bytes_overflow", storageEnvelope(storageGroup("bucket-a", at, fmt.Sprint(uint64(math.MaxUint64)), "1", "0", "0")), w, ErrResponse},
		{"missing_utc_date", storageEnvelope(storageGroup("bucket-a", testNow.Add(-24*time.Hour), "1", "0", "0", "0")), otherDayWindow, ErrStorageCoverage},
		{"limit_reached", storageEnvelope(strings.TrimSuffix(strings.Repeat(valid+",", rowLimit), ",")), w, ErrTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseStorage([]byte(tc.body), tc.w)
			if o != nil || !errors.Is(err, tc.want) {
				t.Fatalf("expected no partial observation and %v; got %+v %v", tc.want, o, err)
			}
			if strings.Contains(ErrorCode(err), "provider detail") {
				t.Fatal("provider response leaked into error code")
			}
		})
	}
}

func TestStorageEstimateUsesIntegerCeilingAndAssessmentRequiresExplicitClassAttestation(t *testing.T) {
	peaks := []StorageDailyPeak{{TotalBytes: 1}, {TotalBytes: 29_999}}
	byteDays, estimate, ok := storageEstimate(peaks)
	if !ok || byteDays != "30000" || estimate != 1 {
		t.Fatalf("unexpected fixed-point estimate: %s %d %v", byteDays, estimate, ok)
	}

	o := storageObservation()
	if got := AssessStorage(o, nil, o.Window.Start, testNow, DefaultStoragePolicy(false)); got != (StorageAssessment{"estimate_only", "storage_class_unconfirmed", "verify_standard_only_and_billing"}) {
		t.Fatalf("unconfirmed class used as free-tier evidence: %+v", got)
	}
	if got := AssessStorage(o, nil, o.Window.Start, testNow, DefaultStoragePolicy(true)); got.Status != "low_estimate" {
		t.Fatalf("confirmed low estimate rejected: %+v", got)
	}
	warning := makeStorageObservation(210_000_000_000)
	if got := AssessStorage(warning, nil, warning.Window.Start, testNow, DefaultStoragePolicy(true)); got.Status != "warning" || got.Recommendation != "review_usage" {
		t.Fatalf("storage warning threshold is wrong: %+v", got)
	}
	high := makeStorageObservation(255_000_000_000)
	if got := AssessStorage(high, nil, high.Window.Start, testNow, DefaultStoragePolicy(true)); got.Status != "high" || got.Recommendation != "pause_nonessential" {
		t.Fatalf("storage high threshold is wrong: %+v", got)
	}
	stop := makeStorageObservation(285_000_000_000)
	if stop.EstimatedGBMonthMicrounits != 9_500_000 {
		t.Fatal(stop.EstimatedGBMonthMicrounits)
	}
	if got := AssessStorage(stop, nil, stop.Window.Start, testNow, DefaultStoragePolicy(true)); got.Status != "stop_recommended" {
		t.Fatalf("storage reserve did not stop: %+v", got)
	}
	stale := storageObservation()
	if got := AssessStorage(stale, nil, stale.Window.Start, testNow.Add(time.Hour), DefaultStoragePolicy(true)); got.Status != "unknown" || got.Reason != "storage_observation_stale" {
		t.Fatalf("stale storage evidence accepted: %+v", got)
	}
	if ErrorCode(ErrStorageCoverage) != "usage_storage_coverage_incomplete" {
		t.Fatal("storage coverage error is not sanitized")
	}
	invalidPolicy := DefaultStoragePolicy(true)
	invalidPolicy.ReserveGBMonthMicrounits = invalidPolicy.LimitGBMonthMicrounits
	if got := AssessStorage(o, nil, o.Window.Start, testNow, invalidPolicy); got.Status != "unknown" || got.Reason != "storage_policy_invalid" {
		t.Fatalf("invalid storage policy accepted: %+v", got)
	}
}

func TestStorageAssessmentRejectsTamperedObservation(t *testing.T) {
	mutations := []func(*StorageObservation){
		func(o *StorageObservation) { o.ObservedByteDays = "0" },
		func(o *StorageObservation) { o.EstimatedGBMonthMicrounits = 0 },
		func(o *StorageObservation) { o.DailyPeaks[0].TotalBytes++ },
		func(o *StorageObservation) { o.DailyPeaks[0].Date = "2026-09-10" },
		func(o *StorageObservation) { o.Rows = rowLimit },
		func(o *StorageObservation) { o.SampleTimes = 0 },
		func(o *StorageObservation) { o.ObservedBuckets = 0 },
	}
	for i, mutate := range mutations {
		o := storageObservation()
		mutate(o)
		got := AssessStorage(o, nil, o.Window.Start, testNow, DefaultStoragePolicy(true))
		if got.Status != "unknown" || got.Reason != "storage_observation_invalid" {
			t.Fatalf("mutation %d accepted: %+v", i, got)
		}
	}
}
