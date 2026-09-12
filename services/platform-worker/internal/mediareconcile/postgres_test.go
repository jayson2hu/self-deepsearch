package mediareconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type inventoryRow func(...any) error

func (row inventoryRow) Scan(dest ...any) error { return row(dest...) }

type inventoryRows struct {
	pgx.Rows
	objects []Object
	index   int
	closed  bool
}

func (rows *inventoryRows) Next() bool { rows.index++; return rows.index <= len(rows.objects) }
func (rows *inventoryRows) Err() error { return nil }
func (rows *inventoryRows) Close()     { rows.closed = true }
func (rows *inventoryRows) Scan(dest ...any) error {
	object := rows.objects[rows.index-1]
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*string), *dest[4].(*int64) = object.StorageScope, object.StorageKey, object.BackupPath, object.SHA256, object.ByteSize
	return nil
}

type inventoryTx struct {
	pgx.Tx
	options           pgx.TxOptions
	steps             []string
	notDue, committed bool
	rows              inventoryRows
	inventorySQL      string
	scheduleSQL       string
	failQuery         bool
}

func (tx *inventoryTx) begin(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx.options = options
	return tx, nil
}
func (tx *inventoryTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		tx.steps = append(tx.steps, "lock")
		return pgconn.NewCommandTag("SELECT 1"), nil
	}
	if !tx.rows.closed {
		return pgconn.CommandTag{}, errors.New("inventory cursor is still open")
	}
	if strings.Contains(sql, "inventory_too_large") {
		tx.steps = append(tx.steps, "too-large")
	} else {
		tx.steps = append(tx.steps, "count")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *inventoryTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return inventoryRow(func(dest ...any) error {
		if strings.Contains(sql, "SELECT EXISTS") {
			tx.scheduleSQL = sql
			tx.steps = append(tx.steps, "schedule")
			*dest[0].(*bool) = tx.notDue
			return nil
		}
		if strings.Contains(sql, "INSERT INTO audit.media_reconciliation_runs") {
			tx.steps = append(tx.steps, "run")
			*dest[0].(*string) = "10000000-0000-4000-8000-000000000001"
			return nil
		}
		return errors.New("unexpected inventory query")
	})
}
func (tx *inventoryTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.steps = append(tx.steps, "inventory")
	tx.inventorySQL = sql
	if tx.failQuery || len(args) != 1 || args[0] != maximumObjects+1 {
		return nil, errors.New("inventory failed")
	}
	return &tx.rows, nil
}
func (tx *inventoryTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	tx.committed = true
	return nil
}
func (tx *inventoryTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
}

func TestInventoryPreparationUsesPostLockSnapshotAndRetainsHiddenObjects(t *testing.T) {
	tx := &inventoryTx{rows: inventoryRows{objects: reportRun().Objects}}
	run, err := prepareReconciliation(context.Background(), tx.begin, time.Now(), 24*time.Hour)
	if err != nil || !tx.committed || len(run.Objects) != 1 {
		t.Fatalf("inventory: %+v %v", run, err)
	}
	if tx.options.IsoLevel != pgx.ReadCommitted || strings.Join(tx.steps, ",") != "lock,schedule,run,inventory,count,commit,rollback" {
		t.Fatalf("stale snapshot or bad transaction ordering: %+v %v", tx.options, tx.steps)
	}
	if !strings.Contains(tx.inventorySQL, "object_status <> 'deleted'") || strings.Contains(tx.inventorySQL, "object_status = 'published'") {
		t.Fatal("retained/hidden inventory was excluded before physical deletion")
	}
	if !strings.Contains(tx.scheduleSQL, "AND NOT (run_status = 'failed' AND error_code = 'usage_guard_denied')") {
		t.Fatal("a pre-dispatch denial must not consume the daily scan cadence")
	}
}

func TestInventoryPreparationNotDueFailureAndLimit(t *testing.T) {
	for _, test := range []struct {
		name              string
		notDue, failQuery bool
		count             int
		committed         bool
	}{
		{"not-due", true, false, 0, false}, {"query-failure", false, true, 0, false}, {"over-limit", false, false, maximumObjects + 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &inventoryTx{notDue: test.notDue, failQuery: test.failQuery, rows: inventoryRows{objects: make([]Object, test.count)}}
			run, err := prepareReconciliation(context.Background(), tx.begin, time.Now(), 24*time.Hour)
			if err == nil || run.ID != "" || tx.committed != test.committed {
				t.Fatalf("invalid result: %+v %v %v", run, tx.steps, err)
			}
			if test.notDue && (!errors.Is(err, ErrNotDue) || strings.Join(tx.steps, ",") != "lock,schedule,rollback") {
				t.Fatal("not-due run performed work")
			}
			if test.count > maximumObjects && !strings.Contains(fmt.Sprint(tx.steps), "too-large") {
				t.Fatal("oversize inventory failure was not persisted")
			}
		})
	}
}
