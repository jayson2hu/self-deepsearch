package mediausage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func reviewFixture() (storedState, reviewMetadata, reviewRequest, Schedule) {
	s := testSchedule()
	row := initialStored()
	row.State, _ = Advance(row.State, observation(), nil, s, testNow)
	row.State = requireReview(row.State, testNow, "usage_unavailable")
	row.LastAssessment = Assessment{"low_estimate", "operations_below_warning", "observe_only"}
	return row, reviewMetadata{s.AccountRef(), s.PolicyDocument(), s.PeriodStart, s.PeriodEnd},
		reviewRequest{ID: runID, ActorID: runID, Action: "acknowledge", Digest: s.Fingerprint(), ExpectedAt: row.UpdatedAt, CreatedAt: testNow.Add(-time.Second), ExpiresAt: testNow.Add(5 * time.Minute)}, s
}

func TestReviewAcknowledgementRequiresFreshAuthorizedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storedState, *reviewRequest, *Schedule)
	}{
		{"stop", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.StopRecommended = true }},
		{"false-low-label", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.HighWater.ClassA = 950000 }},
		{"not-held", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.ReviewRequired = false }},
		{"unknown", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.LastAssessment.Status = "unknown" }},
		{"high", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.LastAssessment.Status = "high" }},
		{"old-sample", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.LastUntil = testNow.Add(-31 * time.Minute) }},
		{"missing-sample", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.LastSuccessAt = time.Time{} }},
		{"future-sample", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.LastUntil = testNow.Add(time.Second) }},
		{"busy", func(r *storedState, _ *reviewRequest, _ *Schedule) { r.activeRun = timeString(runID) }},
		{"stale-snapshot", func(_ *storedState, r *reviewRequest, _ *Schedule) { r.ExpectedAt = r.ExpectedAt.Add(time.Second) }},
		{"expired", func(_ *storedState, r *reviewRequest, _ *Schedule) { r.ExpiresAt = testNow }},
		{"future-request", func(_ *storedState, r *reviewRequest, _ *Schedule) { r.CreatedAt = testNow.Add(time.Second) }},
		{"config-changed", func(_ *storedState, _ *reviewRequest, s *Schedule) { s.AccountID = strings.Repeat("f", 32) }},
		{"wrong-digest", func(_ *storedState, r *reviewRequest, _ *Schedule) { r.Digest = strings.Repeat("f", 64) }},
		{"invalid-action", func(_ *storedState, r *reviewRequest, _ *Schedule) { r.Action = "reset_everything" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, m, req, s := reviewFixture()
			tc.change(&row, &req, &s)
			patch, code := reviewPatch(row, m, req, s, testNow)
			if patch != nil || code == "" {
				t.Fatal("unsafe review was accepted")
			}
		})
	}
	row, m, req, s := reviewFixture()
	for _, status := range []Assessment{{"low_estimate", "operations_below_warning", "observe_only"}, {"warning", "operations_at_70_percent", "review_usage"}} {
		row.LastAssessment = status
		patch, code := reviewPatch(row, m, req, s, testNow)
		if code != "" || patch["review_required"] != false || len(patch) != 5 {
			t.Fatalf("unexpected acknowledgement: %v %s", patch, code)
		}
		if _, ok := patch["high_class_a"]; ok {
			t.Fatal("acknowledgement lowered counters")
		}
	}
}
func timeString(s string) *string { return &s }

func TestPeriodRotationRequiresConfiguredSameAccountAndRetainsHold(t *testing.T) {
	row, m, req, s := reviewFixture()
	m.End = testNow.Add(-time.Second)
	req.Action = "rotate_period"
	s.PeriodStart = testNow
	s.PeriodEnd = testNow.Add(24 * time.Hour)
	req.NextStart = &s.PeriodStart
	req.NextEnd = &s.PeriodEnd
	patch, code := reviewPatch(row, m, req, s, testNow)
	if code != "" || patch["review_required"] != true || patch["last_recommendation"] != "hold_for_review" || patch["stop_recommended"] != false || patch["last_until"] != nil {
		t.Fatalf("bad rotation: %v %s", patch, code)
	}
	for _, tc := range []struct {
		name   string
		change func(*reviewMetadata, *reviewRequest, *Schedule)
	}{
		{"overlap", func(m *reviewMetadata, _ *reviewRequest, _ *Schedule) { m.End = testNow.Add(time.Hour) }},
		{"account", func(m *reviewMetadata, _ *reviewRequest, _ *Schedule) { m.AccountRef = strings.Repeat("f", 64) }},
		{"policy", func(m *reviewMetadata, _ *reviewRequest, _ *Schedule) {
			m.Policy = strings.Replace(m.Policy, "1000000", "2000000", 1)
		}},
		{"invalid-policy", func(m *reviewMetadata, _ *reviewRequest, _ *Schedule) { m.Policy = "{" }},
		{"missing-period", func(_ *reviewMetadata, r *reviewRequest, _ *Schedule) { r.NextEnd = nil }},
		{"config-period", func(_ *reviewMetadata, _ *reviewRequest, s *Schedule) { s.PeriodEnd = s.PeriodEnd.Add(time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm, cr, cs := m, req, s
			tc.change(&cm, &cr, &cs)
			if p, c := reviewPatch(row, cm, cr, cs, testNow); p != nil || c == "" {
				t.Fatal("mismatched rotation accepted")
			}
		})
	}
	// Above JS's exact integer limit: policy equality must not round two distinct budgets together.
	s.Policy.ClassALimit = math.MaxUint64
	m.Policy = s.PolicyDocument()
	if p, c := reviewPatch(row, m, req, s, testNow); p == nil || c != "" {
		t.Fatal("uint64 policy comparison lost precision")
	}
	m.Policy = strings.Replace(m.Policy, "18446744073709551615", "18446744073709551614", 1)
	if p, c := reviewPatch(row, m, req, s, testNow); p != nil || c == "" {
		t.Fatal("different uint64 budgets compared equal")
	}
}

type reviewTx struct {
	usageTx
	request reviewRequest
	meta    reviewMetadata
	allowed bool
	code    string
}

func (tx *reviewTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM audit.media_usage_reviews WHERE status='pending'") {
		return usageRow(func(dest ...any) error {
			if err := tx.step("review"); err != nil {
				return err
			}
			if tx.fail == "none" {
				return pgx.ErrNoRows
			}
			r := tx.request
			values := []any{r.ID, r.ActorID, r.Action, r.Digest, r.ExpectedAt, r.CreatedAt, r.ExpiresAt, r.NextStart, r.NextEnd}
			for i, v := range values {
				reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
			}
			return nil
		})
	}
	if strings.Contains(sql, "SELECT account_ref,policy_snapshot") {
		return usageRow(func(dest ...any) error {
			if err := tx.step("metadata"); err != nil {
				return err
			}
			*dest[0].(*string) = tx.meta.AccountRef
			*dest[1].(*string) = tx.meta.Policy
			*dest[2].(*time.Time) = tx.meta.Start
			*dest[3].(*time.Time) = tx.meta.End
			return nil
		})
	}
	if strings.Contains(sql, "lock_media_usage_reviewer") {
		return usageRow(func(dest ...any) error {
			if err := tx.step("authorize"); err != nil {
				return err
			}
			*dest[0].(*bool) = tx.allowed
			return nil
		})
	}
	return tx.usageTx.QueryRow(ctx, sql, args...)
}
func (tx *reviewTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	name := ""
	switch {
	case strings.Contains(sql, "status='rejected'"):
		name = "reject"
		tx.code = args[2].(string)
	case strings.Contains(sql, "before_state=to_jsonb(state)"):
		name = "evidence"
	case strings.Contains(sql, "config_digest=next.config_digest"):
		name = "transition"
	default:
		return tx.usageTx.Exec(ctx, sql, args...)
	}
	if err := tx.step(name); err != nil {
		return pgconn.CommandTag{}, err
	}
	if tx.zero == name {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *reviewTx) begin(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	_, err := tx.usageTx.begin(ctx, o)
	return tx, err
}

func TestReviewPersistenceIsAtomicAndRechecksActor(t *testing.T) {
	for _, failure := range []string{"", "begin", "lock", "read", "review", "metadata", "authorize", "evidence", "transition", "commit"} {
		t.Run(failure, func(t *testing.T) {
			row, m, req, s := reviewFixture()
			tx := &reviewTx{usageTx: usageTx{t: t, row: row, fail: failure}, request: req, meta: m, allowed: true}
			repo := PostgresRepository{begin: tx.begin, schedule: s}
			err := repo.ProcessReviews(context.Background(), testNow)
			if (err != nil) != (failure != "") {
				t.Fatalf("failure boundary %s %v", failure, err)
			}
			if failure == "" && strings.Join(tx.steps, ",") != "begin,lock,read,review,metadata,authorize,evidence,transition,commit,rollback" {
				t.Fatalf("wrong transaction order: %v", tx.steps)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		allowed bool
		zero    string
	}{{"inactive", false, ""}, {"evidence", true, "evidence"}, {"transition", true, "transition"}, {"reject", false, "reject"}} {
		t.Run(tc.name, func(t *testing.T) {
			row, m, req, s := reviewFixture()
			tx := &reviewTx{usageTx: usageTx{t: t, row: row, zero: tc.zero}, request: req, meta: m, allowed: tc.allowed}
			repo := PostgresRepository{begin: tx.begin, schedule: s}
			err := repo.ProcessReviews(context.Background(), testNow)
			if tc.zero != "" && !errors.Is(err, ErrLeaseLost) {
				t.Fatal("lost update returned success")
			}
			if !tc.allowed && tx.code != "usage_reviewer_inactive" {
				t.Fatal("inactive actor was not rejected")
			}
		})
	}
}

type failingReviews struct {
	t     *testing.T
	err   error
	calls int
}

func (f *failingReviews) ProcessReviews(ctx context.Context, _ time.Time) error {
	f.calls++
	checkDeadline(f.t, ctx, 3*time.Second)
	return f.err
}
func TestReviewStageRunsBeforeObservationAndFailureDoesNotFetch(t *testing.T) {
	for _, fail := range []error{nil, errors.New(testToken)} {
		reviews := &failingReviews{t: t, err: fail}
		repo := &runnerRepo{t: t, run: Run{runID, shortWindow()}}
		fetch := &fakeFetcher{}
		r := Runner{Repository: repo, Reviews: reviews, Client: fetch, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return testNow }}
		r.runOnce(context.Background())
		if reviews.calls != 1 || (fail != nil && repo.prepared != 0) || (fail == nil && repo.prepared != 1) {
			t.Fatal("wrong review/observe ordering")
		}
	}
}
