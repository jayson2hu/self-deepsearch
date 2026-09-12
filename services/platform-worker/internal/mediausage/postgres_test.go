package mediausage

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const runID = "90000000-0000-4000-8000-000000000001"

type usageRow func(...any) error

func (f usageRow) Scan(dest ...any) error { return f(dest...) }

type usageTx struct {
	pgx.Tx
	t                *testing.T
	row              storedState
	steps            []string
	fail, zero       string
	options          pgx.TxOptions
	saved, finalized []any
}

func (tx *usageTx) step(name string) error {
	tx.steps = append(tx.steps, name)
	if tx.fail == name {
		return errors.New("synthetic database error")
	}
	return nil
}
func (tx *usageTx) begin(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx.options = options
	if err := tx.step("begin"); err != nil {
		return nil, err
	}
	return tx, nil
}
func (tx *usageTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	name := ""
	switch {
	case strings.Contains(sql, "pg_advisory_xact_lock"):
		name = "lock"
	case strings.Contains(sql, "INSERT INTO platform.media_usage_state"):
		name = "initialize"
		if len(args) != 6 || args[0] != testSchedule().Fingerprint() || args[1] != testSchedule().AccountRef() || strings.Contains(args[5].(string), testAccount) {
			tx.t.Error("missing/sensitive policy evidence")
		}
	case strings.Contains(sql, "error_code='usage_interrupted'"):
		name = "interrupt"
	case strings.Contains(sql, "SET next_poll_at="):
		name = "claim"
	case strings.Contains(sql, "high_class_a=$1"):
		name = "save"
		tx.saved = args
	case strings.Contains(sql, "SET run_status=$2"):
		name = "finalize"
		tx.finalized = args
	case strings.Contains(sql, "active_run_id=NULL"):
		if len(args) == 0 {
			name = "clear-interrupted"
		} else {
			name = "release"
			if !strings.Contains(sql, "lease_until>clock_timestamp()") || !strings.Contains(sql, "active_run_id=$1") {
				tx.t.Error("missing lease fencing")
			}
		}
	default:
		tx.t.Errorf("unexpected SQL: %s", sql)
	}
	if err := tx.step(name); err != nil {
		return pgconn.CommandTag{}, err
	}
	if tx.zero == name {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *usageTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return usageRow(func(dest ...any) error {
		if strings.Contains(sql, "INSERT INTO audit.media_usage_runs") {
			if err := tx.step("create-run"); err != nil {
				return err
			}
			*dest[0].(*string) = runID
			return nil
		}
		name := "read"
		if !strings.Contains(sql, "FOR UPDATE") {
			name = "read-only"
		}
		if err := tx.step(name); err != nil {
			return err
		}
		if tx.fail == "no-row" {
			return pgx.ErrNoRows
		}
		r := tx.row
		var reviewReason *string
		if r.ReviewRequired {
			reason := r.ReviewReason
			reviewReason = &reason
		}
		values := []any{r.digest, strconv.FormatUint(r.HighWater.ClassA, 10), strconv.FormatUint(r.HighWater.ClassB, 10), strconv.FormatUint(r.HighWater.Free, 10),
			timePointer(r.LastSuccessAt), timePointer(r.LastUntil), r.ReviewRequired, timePointer(r.ReviewSince), reviewReason, r.StopRecommended,
			r.LastAssessment.Status, r.LastAssessment.Reason, r.LastAssessment.Recommendation, r.UpdatedAt, r.nextPoll, r.activeRun, r.leaseUntil, r.leaseLive}
		if len(values) != len(dest) {
			tx.t.Fatal("scan shape changed")
		}
		for i, value := range values {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
		}
		return nil
	})
}
func (tx *usageTx) Commit(context.Context) error   { return tx.step("commit") }
func (tx *usageTx) Rollback(context.Context) error { return tx.step("rollback") }
func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
func initialStored() storedState {
	return storedState{State: State{LastAssessment: Assessment{"unknown", "usage_no_data", "hold_for_review"}, UpdatedAt: testNow}, digest: testSchedule().Fingerprint(), nextPoll: testNow}
}
func leasedStored() storedState {
	r := initialStored()
	id := runID
	r.activeRun = &id
	r.leaseUntil = timePointer(testNow.Add(2 * time.Minute))
	r.leaseLive = true
	return r
}

func TestPrepareSerializesClaimsAndPersistsHolds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storedState)
		now    time.Time
		want   error
		steps  string
	}{
		{"due", func(*storedState) {}, testNow, nil, "begin,lock,initialize,read,create-run,claim,save,commit,rollback"},
		{"not-due", func(r *storedState) { r.nextPoll = testNow.Add(time.Minute) }, testNow, ErrNotDue, "begin,lock,initialize,read,save,commit,rollback"},
		{"live-lease", func(r *storedState) { *r = leasedStored() }, testNow, ErrNotDue, "begin,lock,initialize,read,commit,rollback"},
		{"crashed", func(r *storedState) { *r = leasedStored(); r.leaseUntil = timePointer(testNow); r.leaseLive = false }, testNow, nil, "begin,lock,initialize,read,interrupt,clear-interrupted,create-run,claim,save,commit,rollback"},
		{"different-account", func(r *storedState) { r.digest = strings.Repeat("f", 64) }, testNow, ErrScheduleChanged, "begin,lock,initialize,read,save,commit,rollback"},
		{"closed-period", func(*storedState) {}, testSchedule().PeriodEnd, ErrPeriodInactive, "begin,lock,initialize,read,save,commit,rollback"},
		{"stale", func(r *storedState) { r.LastUntil = testNow.Add(-31 * time.Minute); r.LastSuccessAt = r.LastUntil }, testNow, nil, "begin,lock,initialize,read,create-run,claim,save,commit,rollback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := initialStored()
			tc.change(&row)
			tx := &usageTx{t: t, row: row}
			repo := PostgresRepository{tx.begin, testSchedule()}
			run, err := repo.Prepare(context.Background(), tc.now)
			if !errors.Is(err, tc.want) || tx.options.IsoLevel != pgx.ReadCommitted || strings.Join(tx.steps, ",") != tc.steps {
				t.Fatalf("run=%+v err=%v steps=%v", run, err, tx.steps)
			}
			if tc.want != nil && run.ID != "" {
				t.Fatal("failed/not-due claim returned run")
			}
			if tc.name == "crashed" || tc.name == "different-account" || tc.name == "closed-period" || tc.name == "stale" {
				if tx.saved[5] != true || tx.saved[6] == nil || tx.saved[7] == nil {
					t.Fatal("missing durable hold")
				}
			}
		})
	}
}

func TestPrepareAnyWriteFailureDoesNotReturnClaim(t *testing.T) {
	for _, step := range []string{"begin", "lock", "initialize", "read", "create-run", "claim", "save", "commit"} {
		t.Run(step, func(t *testing.T) {
			tx := &usageTx{t: t, row: initialStored(), fail: step}
			repo := PostgresRepository{tx.begin, testSchedule()}
			if run, err := repo.Prepare(context.Background(), testNow); err == nil || run.ID != "" {
				t.Fatal("failed transaction returned claim")
			}
			if step != "begin" && tx.steps[len(tx.steps)-1] != "rollback" {
				t.Fatal("transaction leaked")
			}
		})
	}
}

func TestInterruptedRunMustHaveMatchingAuditEvidence(t *testing.T) {
	row := leasedStored()
	row.leaseLive = false
	row.leaseUntil = timePointer(testNow)
	tx := &usageTx{t: t, row: row, zero: "interrupt"}
	repo := PostgresRepository{tx.begin, testSchedule()}
	run, err := repo.Prepare(context.Background(), testNow)
	if !errors.Is(err, ErrLeaseLost) || run.ID != "" || strings.Contains(strings.Join(tx.steps, ","), "clear-interrupted") {
		t.Fatal("missing interruption evidence was silently cleared")
	}
}

func TestFinishFencesLeaseAndRollsBackUnconfirmedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*usageTx)
		fetchError error
		expectErr  bool
	}{
		{"healthy", func(*usageTx) {}, nil, false},
		{"provider-failed", func(*usageTx) {}, errors.New(testToken), false},
		{"wrong-owner", func(tx *usageTx) { id := "another"; tx.row.activeRun = &id }, nil, true},
		{"local-expired", func(tx *usageTx) { tx.row.leaseUntil = timePointer(testNow) }, nil, true},
		{"db-expired", func(tx *usageTx) { tx.row.leaseLive = false }, nil, true},
		{"config-changed", func(tx *usageTx) { tx.row.digest = "other" }, nil, true},
		{"missing", func(tx *usageTx) { tx.fail = "no-row" }, nil, true},
		{"audit-conflict", func(tx *usageTx) { tx.zero = "finalize" }, nil, true},
		{"save-failure", func(tx *usageTx) { tx.fail = "save" }, nil, true},
		{"release-conflict", func(tx *usageTx) { tx.zero = "release" }, nil, true},
		{"commit-failure", func(tx *usageTx) { tx.fail = "commit" }, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &usageTx{t: t, row: leasedStored()}
			tc.change(tx)
			repo := PostgresRepository{tx.begin, testSchedule()}
			state, err := repo.Finish(context.Background(), Run{runID, shortWindow()}, observation(), tc.fetchError, testNow)
			if (err != nil) != tc.expectErr {
				t.Fatalf("unexpected finish: %+v %v", state, err)
			}
			if tx.steps[len(tx.steps)-1] != "rollback" {
				t.Fatal("unclosed transaction")
			}
			if tc.expectErr && !state.UpdatedAt.IsZero() {
				t.Fatal("uncommitted state reported as final")
			}
			if tc.name == "healthy" && strings.Join(tx.steps, ",") != "begin,read,finalize,save,release,commit,rollback" {
				t.Fatal(tx.steps)
			}
			if tc.name == "provider-failed" && (tx.finalized[1] != "failed" || tx.finalized[3] != nil || tx.finalized[5] != "usage_unavailable" || !state.ReviewRequired) {
				t.Fatal("partial/error response became complete evidence")
			}
		})
	}
}

func TestMetricsReadsDurableStateWithoutLockOrPrivateLabels(t *testing.T) {
	row := leasedStored()
	row.State, _ = Advance(State{}, observation(), nil, testSchedule(), testNow)
	tx := &usageTx{t: t, row: row}
	repo := PostgresRepository{tx.begin, testSchedule()}
	var out bytes.Buffer
	repo.WriteMetrics(context.Background(), &out, testNow)
	if tx.options.AccessMode != pgx.ReadOnly || strings.Join(tx.steps, ",") != "begin,read-only,commit,rollback" {
		t.Fatal("metrics wrote or locked state")
	}
	if !strings.Contains(out.String(), "media_usage_state_available 1") || !strings.Contains(out.String(), "media_usage_class_a_highwater 0") || strings.Contains(out.String(), testAccount) {
		t.Fatal(out.String())
	}
	out.Reset()
	writeUsageMetrics(&out, State{}, errors.New(testToken), testNow, testSchedule())
	if !strings.Contains(out.String(), "media_usage_state_available 0") || strings.Contains(out.String(), "class_a_highwater") || strings.Contains(out.String(), testToken) {
		t.Fatal("unavailable state reported as zero/private data")
	}
	out.Reset()
	writeUsageMetrics(&out, State{}, nil, testNow, testSchedule())
	if !strings.Contains(out.String(), "media_usage_hold_recommended 1") || strings.Contains(out.String(), "class_a_highwater") {
		t.Fatal("unobserved state reported healthy")
	}
	out.Reset()
	stopped := row.State
	stopped.StopRecommended = true
	stopped.ReviewRequired = true
	writeUsageMetrics(&out, stopped, nil, testNow, testSchedule())
	if !strings.Contains(out.String(), "media_usage_stop_recommended 1") {
		t.Fatal(out.String())
	}
}
