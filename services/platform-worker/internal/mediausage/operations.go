// Package mediausage reads account-wide R2 analytics. It is not a billing
// meter and cannot authorize uploads, resume delivery, or guarantee zero cost.
package mediausage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	graphqlEndpoint = "https://api.cloudflare.com/client/v4/graphql"
	rowLimit        = 10000
	bodyLimit       = 2 << 20
	retention       = 31 * 24 * time.Hour
)

// The dataset, fields and account-level query are documented at:
// https://developers.cloudflare.com/r2/platform/metrics-analytics/
// No bucket filter: other buckets/applications in this account count too.
// Only documented geq/leq filters are used. Inclusive windows share endpoints
// conservatively so fractional timestamps cannot fall into a one-second gap.
const operationsQuery = `query MediaOperations($accountTag: string!, $startDate: Time, $endDate: Time) {
  viewer {
    accounts(filter: {accountTag: $accountTag}) {
      r2OperationsAdaptiveGroups(limit: 10000, filter: {
        datetime_geq: $startDate, datetime_leq: $endDate
      }) {
        sum { requests }
        dimensions { actionType }
      }
    }
  }
}`

var (
	ErrConfig          = errors.New("usage_config_invalid")
	ErrWindow          = errors.New("usage_window_invalid")
	ErrUnavailable     = errors.New("usage_unavailable")
	ErrResponse        = errors.New("usage_response_invalid")
	ErrPartial         = errors.New("usage_response_partial")
	ErrTruncated       = errors.New("usage_response_truncated")
	ErrUnknownAction   = errors.New("usage_unknown_action")
	ErrNoData          = errors.New("usage_no_data")
	ErrStorageCoverage = errors.New("usage_storage_coverage_incomplete")
	accountPattern     = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
	tokenPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{20,256}$`)
)

type Window struct {
	Start        time.Time `json:"start"`
	EndInclusive time.Time `json:"end_inclusive"`
}

func (w Window) valid(now time.Time) bool {
	return !w.Start.IsZero() && !w.EndInclusive.IsZero() && !now.IsZero() &&
		w.Start.Nanosecond() == 0 && w.EndInclusive.Nanosecond() == 0 &&
		!w.Start.After(w.EndInclusive) && !w.EndInclusive.After(now) &&
		!w.Start.Before(now.Add(-retention)) && w.EndInclusive.Sub(w.Start) < retention
}

type Counts struct {
	ClassA uint64 `json:"class_a"`
	ClassB uint64 `json:"class_b"`
	Free   uint64 `json:"free"`
}

type Observation struct {
	Window         Window    `json:"requested_window"`
	ReceivedAt     time.Time `json:"received_at"`
	Segments       int       `json:"query_segments"`
	BoundaryPolicy string    `json:"boundary_policy"`
	Counts         Counts    `json:"operations"`
}

type Client struct {
	account  string
	token    string
	endpoint string
	http     *http.Client
	now      func() time.Time
}

// NewClient fixes the destination and refuses redirects. The analytics token
// must be supplied out of band, not as a URL or command-line argument.
func NewClient(account, token string) (*Client, error) {
	if !accountPattern.MatchString(account) || !tokenPattern.MatchString(token) {
		return nil, ErrConfig
	}
	return &Client{account: account, token: token, endpoint: graphqlEndpoint,
		http: &http.Client{Timeout: 8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		now: time.Now}, nil
}

// Fetch queries at most 31 daily segments, serially, with one overall timeout
// and no automatic retries. A failed segment invalidates the entire result.
// Repeated polling replaces the full period estimate; callers must not add
// repeated month-to-date reports together.
func (c *Client) Fetch(parent context.Context, window Window) (*Observation, error) {
	if !window.valid(c.now().UTC()) {
		return nil, ErrWindow
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	result := &Observation{Window: window, BoundaryPolicy: "inclusive_shared_endpoints"}
	rows := 0
	for start := window.Start; !start.After(window.EndInclusive); {
		end := start.Add(24 * time.Hour)
		if end.After(window.EndInclusive) {
			end = window.EndInclusive
		}
		counts, count, err := c.fetchSegment(ctx, Window{Start: start, EndInclusive: end})
		if err != nil {
			return nil, err
		}
		if !addCounts(&result.Counts, counts) {
			return nil, ErrResponse
		}
		rows += count
		result.Segments++
		if end.Equal(window.EndInclusive) {
			break
		}
		start = end
	}
	// Empty quiet days are legitimate within a nonempty period. An entirely
	// empty report cannot distinguish a new/quiet account from missing data.
	if rows == 0 {
		return nil, ErrNoData
	}
	result.ReceivedAt = c.now().UTC()
	return result, nil
}

func (c *Client) fetchSegment(ctx context.Context, w Window) (Counts, int, error) {
	body, err := c.doGraphQL(ctx, operationsQuery, map[string]string{
		"accountTag": c.account, "startDate": w.Start.UTC().Format(time.RFC3339), "endDate": w.EndInclusive.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return Counts{}, 0, err
	}
	return parseOperations(body)
}

func (c *Client) doGraphQL(ctx context.Context, query string, variables map[string]string) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, ErrConfig
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, ErrConfig
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return nil, ErrResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, bodyLimit+1))
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(body) > bodyLimit {
		return nil, ErrTruncated
	}
	return body, nil
}

type operationRow struct {
	Sum *struct {
		Requests *uint64 `json:"requests"`
	} `json:"sum"`
	Dimensions *struct {
		ActionType *string `json:"actionType"`
	} `json:"dimensions"`
}

func parseOperations(body []byte) (Counts, int, error) {
	var response struct {
		Errors []json.RawMessage `json:"errors"`
		Data   *struct {
			Viewer *struct {
				Accounts []struct {
					Groups *[]operationRow `json:"r2OperationsAdaptiveGroups"`
				} `json:"accounts"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if !unambiguousJSON(body) || json.Unmarshal(body, &response) != nil {
		return Counts{}, 0, ErrResponse
	}
	if len(response.Errors) != 0 {
		return Counts{}, 0, ErrPartial
	}
	if response.Data == nil || response.Data.Viewer == nil || len(response.Data.Viewer.Accounts) != 1 || response.Data.Viewer.Accounts[0].Groups == nil {
		return Counts{}, 0, ErrResponse
	}
	groups := *response.Data.Viewer.Accounts[0].Groups
	if len(groups) >= rowLimit {
		return Counts{}, 0, ErrTruncated
	}
	result := Counts{}
	seen := make(map[string]bool)
	for _, group := range groups {
		if group.Sum == nil || group.Sum.Requests == nil || group.Dimensions == nil || group.Dimensions.ActionType == nil {
			return Counts{}, 0, ErrResponse
		}
		action := *group.Dimensions.ActionType
		if seen[action] {
			return Counts{}, 0, ErrResponse
		}
		seen[action] = true
		counts := Counts{}
		// Pricing classification verified 2026-09-11. Count all observed
		// statuses (including errors); do not assume success-only is billable.
		// https://developers.cloudflare.com/r2/pricing/
		switch action {
		case "ListBuckets", "PutBucket", "ListObjects", "PutObject", "CopyObject", "CompleteMultipartUpload", "CreateMultipartUpload", "LifecycleStorageTierTransition", "ListMultipartUploads", "UploadPart", "UploadPartCopy", "ListParts", "PutBucketEncryption", "PutBucketCors", "PutBucketLifecycleConfiguration":
			counts.ClassA = *group.Sum.Requests
		case "HeadBucket", "HeadObject", "GetObject", "UsageSummary", "GetBucketEncryption", "GetBucketLocation", "GetBucketCors", "GetBucketLifecycleConfiguration":
			counts.ClassB = *group.Sum.Requests
		case "DeleteObject", "DeleteBucket", "AbortMultipartUpload":
			counts.Free = *group.Sum.Requests
		default:
			return Counts{}, 0, ErrUnknownAction
		}
		if !addCounts(&result, counts) {
			return Counts{}, 0, ErrResponse
		}
	}
	return result, len(groups), nil
}

// encoding/json accepts repeated keys (and matches Go fields case-insensitively),
// which could let a trailing low count or empty errors array replace evidence.
// Reject that ambiguity, deeply nested input, and trailing documents first.
func unambiguousJSON(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, container := token.(json.Delim)
		if !container {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return false
				}
				key = strings.ToLower(key)
				if seen[key] {
					return false
				}
				seen[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func addCounts(total *Counts, next Counts) bool {
	if next.ClassA > math.MaxUint64-total.ClassA || next.ClassB > math.MaxUint64-total.ClassB || next.Free > math.MaxUint64-total.Free {
		return false
	}
	total.ClassA += next.ClassA
	total.ClassB += next.ClassB
	total.Free += next.Free
	return true
}

// ErrorCode intentionally never includes a URL, token, account or provider body.
func ErrorCode(err error) string {
	for _, known := range []error{ErrConfig, ErrWindow, ErrUnavailable, ErrResponse, ErrPartial, ErrTruncated, ErrUnknownAction, ErrNoData, ErrStorageCoverage} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return ErrUnavailable.Error()
}
