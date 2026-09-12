package mediausage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)

const testAccount = "0123456789abcdef0123456789abcdef"
const testToken = "synthetic-analytics-token-only-12345"

func group(action, count string) string {
	return fmt.Sprintf(`{"dimensions":{"actionType":%q},"sum":{"requests":%s}}`, action, count)
}

func envelope(groups string) string {
	return `{"data":{"viewer":{"accounts":[{"r2OperationsAdaptiveGroups":[` + groups + `]}]}}}`
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewClient(testAccount, testToken)
	if err != nil {
		t.Fatal(err)
	}
	c.endpoint, c.now = server.URL, func() time.Time { return testNow }
	return c
}

func shortWindow() Window { return Window{Start: testNow.Add(-time.Hour), EndInclusive: testNow} }

func TestFetchAccountScopeAndConservativeInclusiveWindows(t *testing.T) {
	var windows []Window
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing expected request method/headers")
		}
		var payload struct {
			Query     string
			Variables map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Variables["accountTag"] != testAccount || len(payload.Variables) != 3 ||
			strings.Contains(payload.Query, "bucketName") || strings.Contains(payload.Query, "actionStatus") ||
			strings.Contains(payload.Query, testToken) || !strings.Contains(payload.Query, "r2OperationsAdaptiveGroups") {
			t.Error("query must cover all buckets/statuses with separate variables")
		}
		start, _ := time.Parse(time.RFC3339, payload.Variables["startDate"])
		end, _ := time.Parse(time.RFC3339, payload.Variables["endDate"])
		windows = append(windows, Window{start, end})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, envelope(group("PutObject", "2")+","+group("GetObject", "5")+","+group("DeleteObject", "1")))
	})
	w := Window{testNow.Add(-48 * time.Hour), testNow}
	o, err := c.Fetch(context.Background(), w)
	if err != nil {
		t.Fatal(err)
	}
	if o.Counts != (Counts{4, 10, 2}) || o.Segments != 2 || !o.ReceivedAt.Equal(testNow) || o.Window != w {
		t.Fatalf("unexpected report: %+v", o)
	}
	if len(windows) != 2 || windows[0].Start != w.Start || windows[1].EndInclusive != w.EndInclusive {
		t.Fatalf("missing range: %+v", windows)
	}
	for i, segment := range windows {
		if segment.EndInclusive.Sub(segment.Start) > 24*time.Hour || segment.Start.After(segment.EndInclusive) {
			t.Fatal("invalid daily segment")
		}
		if i > 0 && !segment.Start.Equal(windows[i-1].EndInclusive) {
			t.Fatal("inclusive windows must share exactly one endpoint")
		}
	}
}

func TestPricingClassification(t *testing.T) {
	actions := []struct {
		names []string
		want  Counts
	}{
		{[]string{"ListBuckets", "PutBucket", "ListObjects", "PutObject", "CopyObject", "CompleteMultipartUpload", "CreateMultipartUpload", "LifecycleStorageTierTransition", "ListMultipartUploads", "UploadPart", "UploadPartCopy", "ListParts", "PutBucketEncryption", "PutBucketCors", "PutBucketLifecycleConfiguration"}, Counts{ClassA: 1}},
		{[]string{"HeadBucket", "HeadObject", "GetObject", "UsageSummary", "GetBucketEncryption", "GetBucketLocation", "GetBucketCors", "GetBucketLifecycleConfiguration"}, Counts{ClassB: 1}},
		{[]string{"DeleteObject", "DeleteBucket", "AbortMultipartUpload"}, Counts{Free: 1}},
	}
	for _, class := range actions {
		for _, action := range class.names {
			t.Run(action, func(t *testing.T) {
				got, rows, err := parseOperations([]byte(envelope(group(action, "1"))))
				if err != nil || rows != 1 || got != class.want {
					t.Fatalf("counts=%+v rows=%d err=%v", got, rows, err)
				}
			})
		}
	}
}

func TestRejectIncompleteOrAmbiguousReports(t *testing.T) {
	cases := []struct {
		name, body string
		want       error
	}{
		{"malformed", `{"data":`, ErrResponse},
		{"utf8", envelope(group("GetObject", "1")) + string([]byte{0xff}), ErrResponse},
		{"trailing_json", envelope(group("GetObject", "1")) + `{}`, ErrResponse},
		{"duplicate_errors", `{"errors":[{"message":"hidden error"}],"errors":[],"data":{"viewer":{"accounts":[{"r2OperationsAdaptiveGroups":[]}]}}}`, ErrResponse},
		{"duplicate_requests", envelope(`{"dimensions":{"actionType":"GetObject"},"sum":{"requests":10000001,"requests":1}}`), ErrResponse},
		{"case_duplicate", envelope(`{"dimensions":{"actionType":"GetObject"},"sum":{"requests":10000001,"Requests":1}}`), ErrResponse},
		{"partial", `{"errors":[{"message":"secret provider detail"}],"data":{"viewer":{"accounts":[]}}}`, ErrPartial},
		{"data_null", `{"data":null}`, ErrResponse},
		{"viewer_null", `{"data":{"viewer":null}}`, ErrResponse},
		{"account_missing", `{"data":{"viewer":{"accounts":[]}}}`, ErrResponse},
		{"accounts_multiple", `{"data":{"viewer":{"accounts":[{},{}]}}}`, ErrResponse},
		{"groups_missing", `{"data":{"viewer":{"accounts":[{}]}}}`, ErrResponse},
		{"groups_null", `{"data":{"viewer":{"accounts":[{"r2OperationsAdaptiveGroups":null}]}}}`, ErrResponse},
		{"row_null", envelope(`null`), ErrResponse},
		{"sum_missing", envelope(`{"dimensions":{"actionType":"GetObject"}}`), ErrResponse},
		{"requests_missing", envelope(`{"dimensions":{"actionType":"GetObject"},"sum":{}}`), ErrResponse},
		{"requests_null", envelope(group("GetObject", "null")), ErrResponse},
		{"negative", envelope(group("GetObject", "-1")), ErrResponse},
		{"fractional", envelope(group("GetObject", "1.5")), ErrResponse},
		{"exponent", envelope(group("GetObject", "1e3")), ErrResponse},
		{"string_count", envelope(group("GetObject", `"1"`)), ErrResponse},
		{"uint_overflow", envelope(group("GetObject", "18446744073709551616")), ErrResponse},
		{"class_overflow", envelope(group("GetObject", "18446744073709551615") + "," + group("HeadObject", "1")), ErrResponse},
		{"dimensions_missing", envelope(`{"sum":{"requests":1}}`), ErrResponse},
		{"action_null", envelope(`{"dimensions":{"actionType":null},"sum":{"requests":1}}`), ErrResponse},
		{"unknown", envelope(group("FutureOperation", "1")), ErrUnknownAction},
		{"unknown_zero", envelope(group("FutureOperation", "0")), ErrUnknownAction},
		{"duplicate_groups", envelope(group("GetObject", "1") + "," + group("GetObject", "2")), ErrResponse},
		{"limit_reached", envelope(strings.TrimSuffix(strings.Repeat(group("GetObject", "1")+",", rowLimit), ",")), ErrTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rows, err := parseOperations([]byte(tc.body))
			if !errors.Is(err, tc.want) || got != (Counts{}) || rows != 0 {
				t.Fatalf("expected no partial counts and %v; got %+v, %d, %v", tc.want, got, rows, err)
			}
			if strings.Contains(ErrorCode(err), "secret") {
				t.Fatal("error disclosed remote content")
			}
		})
	}
}

func TestFractionalBoundaryEventsAreNotLost(t *testing.T) {
	w := Window{testNow.Add(-48 * time.Hour), testNow}
	boundary := w.Start.Add(24 * time.Hour)
	events := []time.Time{boundary.Add(-500 * time.Millisecond), boundary, boundary.Add(500 * time.Millisecond)}
	c := newTestClient(t, func(response http.ResponseWriter, r *http.Request) {
		var payload struct{ Variables map[string]string }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		start, _ := time.Parse(time.RFC3339, payload.Variables["startDate"])
		end, _ := time.Parse(time.RFC3339, payload.Variables["endDate"])
		count := 0
		for _, event := range events {
			if !event.Before(start) && !event.After(end) {
				count++
			}
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, envelope(group("GetObject", fmt.Sprint(count))))
	})
	o, err := c.Fetch(context.Background(), w)
	// The exact boundary is counted twice, intentionally. No fractional event
	// is lost, and the report must not be advertised as an exact billing meter.
	if err != nil || o == nil || o.Counts.ClassB != 4 {
		t.Fatalf("boundary event lost: %+v %v", o, err)
	}
}

func TestFetchFailureDiscardsSuccessfulSegments(t *testing.T) {
	for _, failure := range []string{"http", "empty_all", "overflow", "unknown", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				switch {
				case failure == "empty_all":
					fmt.Fprint(w, envelope(""))
				case failure == "overflow":
					fmt.Fprint(w, envelope(group("PutObject", "18446744073709551615")))
				case calls == 1:
					fmt.Fprint(w, envelope(group("PutObject", "3")))
					if failure == "cancel" {
						cancel()
					}
				case failure == "http":
					w.WriteHeader(429)
					fmt.Fprint(w, testToken)
				default:
					fmt.Fprint(w, envelope(group("UnknownAction", "3")))
				}
			})
			o, err := c.Fetch(ctx, Window{testNow.Add(-25 * time.Hour), testNow})
			if err == nil || o != nil || calls > 2 {
				t.Fatalf("partial data or retry: %+v %v calls=%d", o, err, calls)
			}
		})
	}
}

func TestQuietDayAndRetentionBound(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, envelope(""))
			return
		}
		fmt.Fprint(w, envelope(group("HeadObject", "1")))
	})
	o, err := c.Fetch(context.Background(), Window{testNow.Add(-retention + time.Second), testNow})
	if err != nil || o == nil || o.Segments != 31 || o.Counts.ClassB != 30 || calls != 31 {
		t.Fatalf("bad retention partition: %+v %v calls=%d", o, err, calls)
	}
}

func TestInvalidWindowsDoNotRequest(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid window made an HTTP request") })
	cases := []Window{
		{}, {testNow, time.Time{}}, {testNow, testNow.Add(time.Second)},
		{testNow, testNow.Add(-time.Second)}, {testNow.Add(-retention), testNow},
		{testNow.Add(-retention - time.Second), testNow.Add(-time.Second)},
		{testNow.Add(-time.Hour + time.Nanosecond), testNow}, {testNow.Add(-time.Hour), testNow.Add(-time.Nanosecond)},
	}
	for _, w := range cases {
		if o, err := c.Fetch(context.Background(), w); o != nil || !errors.Is(err, ErrWindow) {
			t.Fatalf("accepted %+v: %+v %v", w, o, err)
		}
	}
}

func TestHTTPBoundaries(t *testing.T) {
	for _, name := range []string{"wrong_mime", "no_mime", "large_body", "unauthorized", "server_error", "partial_json"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if name != "no_mime" {
					w.Header().Set("Content-Type", "application/json")
				}
				switch name {
				case "wrong_mime":
					w.Header().Set("Content-Type", "text/html")
				case "large_body":
					fmt.Fprint(w, strings.Repeat(" ", bodyLimit+1))
					return
				case "unauthorized":
					w.WriteHeader(401)
				case "server_error":
					w.WriteHeader(503)
				case "partial_json":
					fmt.Fprint(w, `{"data":`)
					return
				}
				fmt.Fprint(w, envelope(group("GetObject", "1")))
			})
			o, err := c.Fetch(context.Background(), shortWindow())
			if err == nil || o != nil || calls != 1 {
				t.Fatalf("boundary accepted: %+v %v calls=%d", o, err, calls)
			}
		})
	}
}

func TestRedirectCannotForwardToken(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	if o, err := c.Fetch(context.Background(), shortWindow()); o != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected result: %+v %v", o, err)
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("redirect leaked request to another endpoint")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCancellationDeadlineAndBodyReadFailure(t *testing.T) {
	c, _ := NewClient(testAccount, testToken)
	c.now = func() time.Time { return testNow }
	calls := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			t.Error("missing HTTP deadline")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if o, err := c.Fetch(ctx, shortWindow()); o != nil || !errors.Is(err, ErrUnavailable) || calls != 1 {
		t.Fatalf("bad cancellation: %+v %v", o, err)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: badBody{}}, nil
	})
	if o, err := c.Fetch(context.Background(), shortWindow()); o != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("bad body failure: %+v %v", o, err)
	}
}

type badBody struct{}

func (badBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (badBody) Close() error             { return nil }

func TestClientConfigAndSanitizedErrors(t *testing.T) {
	for _, pair := range [][2]string{{"", testToken}, {testAccount, ""}, {"host/../../", testToken}, {testAccount, "Bearer " + testToken}, {testAccount, testToken + "\r\n"}, {testAccount, strings.Repeat("a", 257)}} {
		if c, err := NewClient(pair[0], pair[1]); c != nil || !errors.Is(err, ErrConfig) {
			t.Fatal("invalid credentials accepted")
		}
	}
	c, err := NewClient(testAccount, testToken)
	if err != nil || c.endpoint != graphqlEndpoint || c.http.Timeout != 8*time.Second {
		t.Fatal("unsafe production client")
	}
	if ErrorCode(fmt.Errorf("request %s failed: %w", testToken, ErrPartial)) != "usage_response_partial" ||
		ErrorCode(errors.New(testToken)) != "usage_unavailable" {
		t.Fatal("unsanitized error code")
	}
	total := Counts{math.MaxUint64, 2, 3}
	if addCounts(&total, Counts{1, 3, 4}) || total != (Counts{math.MaxUint64, 2, 3}) {
		t.Fatal("overflow mutated partial result")
	}
}
