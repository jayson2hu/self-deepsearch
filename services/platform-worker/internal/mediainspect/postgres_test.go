package mediainspect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type scheduleRow func(...any) error

func (r scheduleRow) Scan(dest ...any) error { return r(dest...) }

type scheduleTx struct {
	pgx.Tx
	steps            []string
	isolation        pgx.TxIsoLevel
	notDue, failRead bool
}

func (tx *scheduleTx) begin(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx.isolation = options.IsoLevel
	return tx, nil
}
func (tx *scheduleTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		tx.steps = append(tx.steps, "lock")
	} else if strings.Contains(sql, "interrupted") {
		tx.steps = append(tx.steps, "interrupt-stale")
	} else {
		return pgconn.CommandTag{}, errors.New("unexpected write")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *scheduleTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return scheduleRow(func(dest ...any) error {
		if strings.Contains(sql, "SELECT EXISTS") {
			tx.steps = append(tx.steps, "schedule")
			if tx.failRead {
				return errors.New("query failed")
			}
			*dest[0].(*bool) = tx.notDue
		} else {
			tx.steps = append(tx.steps, "run")
			if len(args) != 2 || args[1] != "https://display.test" {
				return errors.New("missing origin evidence")
			}
			*dest[0].(*string) = "run-a"
		}
		return nil
	})
}
func (tx *scheduleTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	return nil
}
func (tx *scheduleTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
}

func TestScheduleUsesPostLockSnapshotAndPersistsInterruptedRuns(t *testing.T) {
	for _, test := range []struct {
		name, steps      string
		notDue, failRead bool
	}{
		{"due", "lock,interrupt-stale,schedule,run,commit,rollback", false, false},
		{"not-due", "lock,interrupt-stale,schedule,commit,rollback", true, false},
		{"failure", "lock,interrupt-stale,schedule,rollback", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &scheduleTx{notDue: test.notDue, failRead: test.failRead}
			id, err := prepareInspection(context.Background(), tx.begin, time.Now(), "https://display.test")
			if tx.isolation != pgx.ReadCommitted || strings.Join(tx.steps, ",") != test.steps {
				t.Fatalf("bad schedule isolation/order: %v %v", tx.isolation, tx.steps)
			}
			if test.notDue && (!errors.Is(err, ErrNotDue) || id != "") {
				t.Fatal("not-due run created")
			}
			if test.failRead && err == nil {
				t.Fatal("failed schedule created run")
			}
			if !test.notDue && !test.failRead && (err != nil || id != "run-a") {
				t.Fatal("due run not created")
			}
		})
	}
}
