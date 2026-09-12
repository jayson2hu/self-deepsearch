package database

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/operations"
)

type roleRow struct {
	values []any
	err    error
}

func (row roleRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range row.values {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type roleTransaction struct {
	pgx.Tx                     // Unexpected methods panic; the role path only uses the methods below.
	oldRole, actorRole, failAt string
	steps                      []string
	queryCount                 int
	revokeSQL                  string
	revokeArgs                 []any
	committed                  bool
}

func (tx *roleTransaction) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.queryCount++
	step := []string{"target", "actor", "update"}[tx.queryCount-1]
	tx.steps = append(tx.steps, step)
	if tx.failAt == step {
		return roleRow{err: errors.New("synthetic query failure")}
	}
	switch step {
	case "target":
		if !strings.Contains(sql, "FOR UPDATE") {
			return roleRow{err: errors.New("target row must be locked")}
		}
		return roleRow{values: []any{tx.oldRole}}
	case "actor":
		return roleRow{values: []any{tx.actorRole}}
	default:
		return roleRow{values: []any{args[0].(string), "role@example.test", args[1].(string), "active", time.Now(), (*time.Time)(nil)}}
	}
}
func (tx *roleTransaction) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := "audit"
	if strings.Contains(sql, "UPDATE platform.sessions") {
		step = "revoke"
		tx.revokeSQL, tx.revokeArgs = sql, args
	}
	tx.steps = append(tx.steps, step)
	if tx.failAt == step {
		return pgconn.CommandTag{}, errors.New("synthetic write failure")
	}
	return pgconn.NewCommandTag("UPDATE 2"), nil
}
func (tx *roleTransaction) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	if tx.failAt == "commit" {
		return errors.New("synthetic commit failure")
	}
	tx.committed = true
	return nil
}
func (tx *roleTransaction) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
}
func (tx *roleTransaction) begin(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

func TestRoleChangeRevokesBeforeAuditAndCommit(t *testing.T) {
	for _, roles := range [][2]string{{"user", "editor"}, {"user", "admin"}, {"admin", "user"}, {"editor", "user"}, {"editor", "admin"}} {
		t.Run(roles[0]+"_to_"+roles[1], func(t *testing.T) {
			tx := &roleTransaction{oldRole: roles[0], actorRole: "owner"}
			now := time.Now().UTC()
			item, err := changeUserRole(context.Background(), tx.begin, "target", roles[1], "actor", "role-request", now)
			if err != nil || item.Role != roles[1] {
				t.Fatalf("role change failed: %v", err)
			}
			if !reflect.DeepEqual(tx.steps, []string{"target", "actor", "update", "revoke", "audit", "commit", "rollback"}) {
				t.Fatalf("role transaction did not revoke sessions atomically: %v", tx.steps)
			}
			for _, required := range []string{"greatest($2, created_at)", "revoke_reason = 'role_change'", "WHERE user_id = $1::uuid AND revoked_at IS NULL"} {
				if !strings.Contains(tx.revokeSQL, required) {
					t.Fatalf("unsafe revocation SQL: missing %s", required)
				}
			}
			if len(tx.revokeArgs) != 2 || tx.revokeArgs[0] != "target" || tx.revokeArgs[1] != now {
				t.Fatal("revocation targeted the wrong user/time")
			}
		})
	}
}

func TestRoleChangeFailuresNeverCommit(t *testing.T) {
	for _, step := range []string{"target", "actor", "update", "revoke", "audit", "commit"} {
		t.Run(step, func(t *testing.T) {
			tx := &roleTransaction{oldRole: "user", actorRole: "owner", failAt: step}
			item, err := changeUserRole(context.Background(), tx.begin, "target", "editor", "actor", "request", time.Now())
			if err == nil || item.ID != "" || tx.committed || tx.steps[len(tx.steps)-1] != "rollback" {
				t.Fatal("failed role mutation returned success or failed to roll back")
			}
		})
	}
}

func TestRoleChangeSameRoleAndForbiddenDoNotRevoke(t *testing.T) {
	for _, test := range []struct {
		oldRole, nextRole, actorRole, target string
		want                                 error
	}{
		{"user", "user", "owner", "target", operations.ErrConflict},
		{"owner", "user", "owner", "target", operations.ErrForbidden},
		{"user", "editor", "admin", "target", operations.ErrForbidden},
		{"user", "editor", "owner", "actor", operations.ErrForbidden},
		{"user", "owner", "owner", "target", operations.ErrForbidden},
		{"user", "invalid", "owner", "target", operations.ErrInvalidInput},
	} {
		tx := &roleTransaction{oldRole: test.oldRole, actorRole: test.actorRole}
		_, err := changeUserRole(context.Background(), tx.begin, test.target, test.nextRole, "actor", "request", time.Now())
		if !errors.Is(err, test.want) || tx.committed || tx.revokeSQL != "" {
			t.Fatalf("invalid/no-op role mutation had side effects: %v", err)
		}
	}
}
