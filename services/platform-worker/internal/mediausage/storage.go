package mediausage

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"sort"
	"time"
)

const (
	decimalGigabyteBytes       = uint64(1_000_000_000)
	storageBillingCycleDays    = uint64(30)
	storageGBMonthMicrounits   = uint64(1_000_000)
	standardFreeTierMicrounits = uint64(10_000_000)
	storageEstimateBasis       = "sum_utc_daily_peak_payload_plus_metadata_bytes_div_30_decimal_gb"
	storageEstimateRounding    = "ceiling_to_micro_gb_month"
)

// The query deliberately includes both bucket and time dimensions. A max for
// one bucket is not an account total: rows at the same instant must be summed
// before an account-wide daily peak can be selected.
// Dataset: https://developers.cloudflare.com/r2/platform/metrics-analytics/
// Pricing basis: https://developers.cloudflare.com/r2/pricing/
const storageQuery = `query MediaStorage($accountTag: string!, $startDate: Time, $endDate: Time) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      r2StorageAdaptiveGroups(limit: 10000, filter: {
        datetime_geq: $startDate, datetime_leq: $endDate
      }, orderBy: [datetime_ASC]) {
        max { payloadSize metadataSize objectCount uploadCount }
        dimensions { bucketName datetime }
      }
    }
  }
}`

var bucketNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

type StoragePolicy struct {
	LimitGBMonthMicrounits        uint64        `json:"limit_gb_month_microunits"`
	ReserveGBMonthMicrounits      uint64        `json:"reserve_gb_month_microunits"`
	StandardOnlyOperatorConfirmed bool          `json:"standard_only_operator_confirmed"`
	MaxAge                        time.Duration `json:"-"`
}

func DefaultStoragePolicy(standardOnlyConfirmed bool) StoragePolicy {
	return StoragePolicy{
		LimitGBMonthMicrounits:        standardFreeTierMicrounits,
		ReserveGBMonthMicrounits:      500_000,
		StandardOnlyOperatorConfirmed: standardOnlyConfirmed,
		MaxAge:                        30 * time.Minute,
	}
}

func (p StoragePolicy) valid() bool {
	return p.LimitGBMonthMicrounits > 0 && p.ReserveGBMonthMicrounits > 0 &&
		p.ReserveGBMonthMicrounits < p.LimitGBMonthMicrounits &&
		p.MaxAge > 0 && p.MaxAge <= time.Hour
}

type StorageDailyPeak struct {
	Date          string    `json:"date"`
	SampledAt     time.Time `json:"sampled_at"`
	PayloadBytes  uint64    `json:"payload_bytes"`
	MetadataBytes uint64    `json:"metadata_bytes"`
	TotalBytes    uint64    `json:"total_bytes"`
	ObjectCount   uint64    `json:"object_count"`
	UploadCount   uint64    `json:"unfinished_multipart_upload_count"`
}

type StorageObservation struct {
	Window                     Window             `json:"requested_window"`
	ReceivedAt                 time.Time          `json:"received_at"`
	Rows                       int                `json:"rows"`
	SampleTimes                int                `json:"sample_times"`
	ObservedBuckets            int                `json:"observed_buckets"`
	DailyPeaks                 []StorageDailyPeak `json:"utc_daily_peaks"`
	ObservedByteDays           string             `json:"observed_byte_days"`
	EstimatedGBMonthMicrounits uint64             `json:"estimated_gb_month_microunits"`
	EstimateBasis              string             `json:"estimate_basis"`
	EstimateRounding           string             `json:"estimate_rounding"`
}

type StorageAssessment struct {
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Recommendation string `json:"recommendation"`
}

type storageRow struct {
	Max *struct {
		PayloadSize  *uint64 `json:"payloadSize"`
		MetadataSize *uint64 `json:"metadataSize"`
		ObjectCount  *uint64 `json:"objectCount"`
		UploadCount  *uint64 `json:"uploadCount"`
	} `json:"max"`
	Dimensions *struct {
		BucketName *string `json:"bucketName"`
		Datetime   *string `json:"datetime"`
	} `json:"dimensions"`
}

type storagePoint struct {
	PayloadBytes, MetadataBytes, ObjectCount, UploadCount uint64
}

// FetchStorage is a one-shot read of the documented Storage Analytics dataset.
// It is intentionally not used by the durable operations observer yet.
func (c *Client) FetchStorage(parent context.Context, window Window) (*StorageObservation, error) {
	if !window.valid(c.now().UTC()) {
		return nil, ErrWindow
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	body, err := c.doGraphQL(ctx, storageQuery, map[string]string{
		"accountTag": c.account,
		"startDate":  window.Start.UTC().Format(time.RFC3339),
		"endDate":    window.EndInclusive.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	result, err := parseStorage(body, window)
	if err != nil {
		return nil, err
	}
	result.ReceivedAt = c.now().UTC()
	return result, nil
}

func parseStorage(body []byte, window Window) (*StorageObservation, error) {
	var response struct {
		Errors []json.RawMessage `json:"errors"`
		Data   *struct {
			Viewer *struct {
				Accounts []struct {
					Groups *[]storageRow `json:"r2StorageAdaptiveGroups"`
				} `json:"accounts"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if !unambiguousJSON(body) || json.Unmarshal(body, &response) != nil {
		return nil, ErrResponse
	}
	if len(response.Errors) != 0 {
		return nil, ErrPartial
	}
	if response.Data == nil || response.Data.Viewer == nil || len(response.Data.Viewer.Accounts) != 1 ||
		response.Data.Viewer.Accounts[0].Groups == nil {
		return nil, ErrResponse
	}
	groups := *response.Data.Viewer.Accounts[0].Groups
	if len(groups) == 0 {
		return nil, ErrNoData
	}
	if len(groups) >= rowLimit {
		return nil, ErrTruncated
	}

	type rowKey struct {
		Bucket string
		At     time.Time
	}
	seenRows := make(map[rowKey]struct{}, len(groups))
	buckets := make(map[string]struct{})
	points := make(map[time.Time]storagePoint)
	bucketsAtTime := make(map[time.Time]int)
	for _, group := range groups {
		if group.Max == nil || group.Max.PayloadSize == nil || group.Max.MetadataSize == nil ||
			group.Max.ObjectCount == nil || group.Max.UploadCount == nil || group.Dimensions == nil ||
			group.Dimensions.BucketName == nil || group.Dimensions.Datetime == nil {
			return nil, ErrResponse
		}
		bucket := *group.Dimensions.BucketName
		if !bucketNamePattern.MatchString(bucket) {
			return nil, ErrResponse
		}
		at, err := time.Parse(time.RFC3339Nano, *group.Dimensions.Datetime)
		if err != nil {
			return nil, ErrResponse
		}
		at = at.UTC()
		if at.Before(window.Start) || at.After(window.EndInclusive) {
			return nil, ErrResponse
		}
		key := rowKey{Bucket: bucket, At: at}
		if _, duplicate := seenRows[key]; duplicate {
			return nil, ErrResponse
		}
		seenRows[key] = struct{}{}
		buckets[bucket] = struct{}{}
		bucketsAtTime[at]++
		point := points[at]
		next := storagePoint{*group.Max.PayloadSize, *group.Max.MetadataSize, *group.Max.ObjectCount, *group.Max.UploadCount}
		if !addStoragePoint(&point, next) {
			return nil, ErrResponse
		}
		points[at] = point
	}
	for at := range points {
		if bucketsAtTime[at] != len(buckets) {
			return nil, ErrStorageCoverage
		}
	}

	times := make([]time.Time, 0, len(points))
	for at := range points {
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	peaksByDate := make(map[string]StorageDailyPeak)
	for _, at := range times {
		point := points[at]
		if point.MetadataBytes > math.MaxUint64-point.PayloadBytes {
			return nil, ErrResponse
		}
		peak := StorageDailyPeak{
			Date:          at.Format(time.DateOnly),
			SampledAt:     at,
			PayloadBytes:  point.PayloadBytes,
			MetadataBytes: point.MetadataBytes,
			TotalBytes:    point.PayloadBytes + point.MetadataBytes,
			ObjectCount:   point.ObjectCount,
			UploadCount:   point.UploadCount,
		}
		current, exists := peaksByDate[peak.Date]
		if !exists || peak.TotalBytes > current.TotalBytes ||
			(peak.TotalBytes == current.TotalBytes && peak.SampledAt.Before(current.SampledAt)) {
			peaksByDate[peak.Date] = peak
		}
	}

	expectedDates := utcDates(window)
	if len(peaksByDate) != len(expectedDates) {
		return nil, ErrStorageCoverage
	}
	peaks := make([]StorageDailyPeak, 0, len(expectedDates))
	for _, date := range expectedDates {
		peak, exists := peaksByDate[date]
		if !exists {
			return nil, ErrStorageCoverage
		}
		peaks = append(peaks, peak)
	}
	byteDays, estimate, ok := storageEstimate(peaks)
	if !ok {
		return nil, ErrResponse
	}
	return &StorageObservation{
		Window:                     window,
		Rows:                       len(groups),
		SampleTimes:                len(points),
		ObservedBuckets:            len(buckets),
		DailyPeaks:                 peaks,
		ObservedByteDays:           byteDays,
		EstimatedGBMonthMicrounits: estimate,
		EstimateBasis:              storageEstimateBasis,
		EstimateRounding:           storageEstimateRounding,
	}, nil
}

func addStoragePoint(total *storagePoint, next storagePoint) bool {
	if next.PayloadBytes > math.MaxUint64-total.PayloadBytes ||
		next.MetadataBytes > math.MaxUint64-total.MetadataBytes ||
		next.ObjectCount > math.MaxUint64-total.ObjectCount ||
		next.UploadCount > math.MaxUint64-total.UploadCount {
		return false
	}
	total.PayloadBytes += next.PayloadBytes
	total.MetadataBytes += next.MetadataBytes
	total.ObjectCount += next.ObjectCount
	total.UploadCount += next.UploadCount
	return true
}

func utcDates(window Window) []string {
	start := time.Date(window.Start.UTC().Year(), window.Start.UTC().Month(), window.Start.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(window.EndInclusive.UTC().Year(), window.EndInclusive.UTC().Month(), window.EndInclusive.UTC().Day(), 0, 0, 0, 0, time.UTC)
	result := make([]string, 0, 32)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		result = append(result, day.Format(time.DateOnly))
	}
	return result
}

func storageEstimate(peaks []StorageDailyPeak) (string, uint64, bool) {
	byteDays := new(big.Int)
	for _, peak := range peaks {
		byteDays.Add(byteDays, new(big.Int).SetUint64(peak.TotalBytes))
	}
	numerator := new(big.Int).Mul(new(big.Int).Set(byteDays), new(big.Int).SetUint64(storageGBMonthMicrounits))
	denominator := new(big.Int).SetUint64(storageBillingCycleDays * decimalGigabyteBytes)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return "", 0, false
	}
	return byteDays.String(), quotient.Uint64(), true
}

func AssessStorage(o *StorageObservation, fetchError error, expectedStart, now time.Time, p StoragePolicy) StorageAssessment {
	hold := func(reason string) StorageAssessment { return StorageAssessment{"unknown", reason, "hold_for_review"} }
	if !p.valid() {
		return hold("storage_policy_invalid")
	}
	if fetchError != nil {
		return hold(ErrorCode(fetchError))
	}
	if o == nil {
		return hold("usage_no_data")
	}
	if !validStorageObservation(o, expectedStart, now) {
		return hold("storage_observation_invalid")
	}
	if now.Sub(o.ReceivedAt) > p.MaxAge || now.Sub(o.Window.EndInclusive) > p.MaxAge {
		return hold("storage_observation_stale")
	}
	if !p.StandardOnlyOperatorConfirmed {
		return StorageAssessment{"estimate_only", "storage_class_unconfirmed", "verify_standard_only_and_billing"}
	}
	used, limit, reserve := o.EstimatedGBMonthMicrounits, p.LimitGBMonthMicrounits, p.ReserveGBMonthMicrounits
	if used >= limit || reserve >= limit-used {
		return StorageAssessment{"stop_recommended", "storage_reserve_reached", "stop_media_review_required"}
	}
	if used >= percentageCeiling(limit, 85) {
		return StorageAssessment{"high", "storage_at_85_percent", "pause_nonessential"}
	}
	if used >= percentageCeiling(limit, 70) {
		return StorageAssessment{"warning", "storage_at_70_percent", "review_usage"}
	}
	return StorageAssessment{"low_estimate", "storage_below_warning", "observe_only"}
}

func validStorageObservation(o *StorageObservation, expectedStart, now time.Time) bool {
	if !o.Window.valid(now) || !o.Window.Start.Equal(expectedStart) || o.ReceivedAt.IsZero() ||
		o.ReceivedAt.After(now) || o.ReceivedAt.Before(o.Window.EndInclusive) || o.Rows < 1 || o.Rows >= rowLimit ||
		o.SampleTimes < 1 || o.SampleTimes > o.Rows || o.ObservedBuckets < 1 || o.ObservedBuckets > o.Rows ||
		o.EstimateBasis != storageEstimateBasis || o.EstimateRounding != storageEstimateRounding ||
		len(o.DailyPeaks) != len(utcDates(o.Window)) {
		return false
	}
	expectedDates := utcDates(o.Window)
	for i, peak := range o.DailyPeaks {
		if peak.Date != expectedDates[i] || peak.SampledAt.UTC().Format(time.DateOnly) != peak.Date ||
			peak.SampledAt.Before(o.Window.Start) || peak.SampledAt.After(o.Window.EndInclusive) ||
			peak.MetadataBytes > math.MaxUint64-peak.PayloadBytes || peak.TotalBytes != peak.PayloadBytes+peak.MetadataBytes {
			return false
		}
	}
	byteDays, estimate, ok := storageEstimate(o.DailyPeaks)
	return ok && byteDays == o.ObservedByteDays && estimate == o.EstimatedGBMonthMicrounits
}
