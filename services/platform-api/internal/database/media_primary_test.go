package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/operations"
)

type primaryTx struct {
	mediaRegistrationTx
	retirement         retirementTx
	primary, rechecked string
	reads              int
	shared, withdrawn  bool
	failStep           string
}

func (tx *primaryTx) begin(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
func (tx *primaryTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "SELECT asset_id::text, entity_media_id::text"):
		return mediaRegistrationRow(func(dest ...any) error {
			tx.steps = append(tx.steps, "read-primary")
			tx.reads++
			id := tx.primary
			if tx.reads > 1 && tx.rechecked != "" {
				id = tx.rechecked
			}
			if id == "" {
				return pgx.ErrNoRows
			}
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = id, "old-link", "cover"
			return nil
		})
	case strings.Contains(sql, "SELECT asset_status, rights_status"):
		return mediaRegistrationRow(func(dest ...any) error {
			tx.steps = append(tx.steps, "lock-old-asset")
			*dest[0].(*string), *dest[1].(*string) = "published", "allowed"
			if tx.withdrawn {
				*dest[1].(*string) = "takedown"
			}
			return nil
		})
	case strings.Contains(sql, "SELECT EXISTS"):
		return mediaRegistrationRow(func(dest ...any) error {
			tx.steps = append(tx.steps, "shared")
			*dest[0].(*bool) = tx.shared
			return nil
		})
	case strings.Contains(sql, "INSERT INTO platform.media_assets") && tx.failStep == "new-asset":
		return mediaRegistrationRow(func(...any) error { return operations.ErrConflict })
	}
	return tx.mediaRegistrationTx.QueryRow(ctx, sql, args...)
}
func (tx *primaryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.steps = append(tx.steps, "retirement")
	return tx.retirement.Query(ctx, sql, args...)
}
func (tx *primaryTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := ""
	switch {
	case strings.Contains(sql, "UPDATE platform.entity_media"):
		step = "hide-link"
	case strings.Contains(sql, "UPDATE platform.media_assets"):
		step = "hide-asset"
	case strings.Contains(sql, "INSERT INTO audit.audit_logs") && args[1] == "media.primary_replace":
		step = "replace-audit"
	case strings.Contains(sql, "'media_delete'"):
		return tx.retirement.Exec(ctx, sql, args...)
	}
	if step != "" {
		tx.steps = append(tx.steps, step)
		if step == tx.failStep {
			return pgconn.CommandTag{}, errors.New("synthetic " + step + " failure")
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return tx.mediaRegistrationTx.Exec(ctx, sql, args...)
}

func replacementFixture() operations.ReplacePrimaryMediaInput {
	manifest := registrationFixture()
	manifest.IsPrimary = true
	return operations.ReplacePrimaryMediaInput{ExpectedAssetID: "old-asset", Manifest: manifest}
}

func newPrimaryTx() *primaryTx {
	return &primaryTx{mediaRegistrationTx: mediaRegistrationTx{status: "published"}, primary: "old-asset",
		retirement: retirementTx{objects: []retiredMediaObject{retiredObjectFixture("private"), retiredObjectFixture("public")}}}
}

func TestPrimaryReplacementAtomicRetirementAndSharedPreservation(t *testing.T) {
	for _, shared := range []bool{false, true} {
		tx := newPrimaryTx()
		tx.shared = shared
		now := time.Now().UTC()
		result, err := replacePrimaryMedia(context.Background(), tx.begin, replacementFixture(), "actor", "request", now)
		if err != nil || !tx.committed || result.PreviousAssetID != "old-asset" || result.OldAssetRetired == shared {
			t.Fatalf("replacement: %+v %v", result, err)
		}
		steps := strings.Join(tx.steps, ",")
		if !strings.HasPrefix(steps, "lock,status,read-primary,lock-old-asset,read-primary,hide-link,asset,") || !strings.HasSuffix(steps, "replace-audit,commit,rollback") {
			t.Fatalf("unsafe lock or transaction ordering: %s", steps)
		}
		if shared {
			if result.PrivateRetainedUntil != nil || len(tx.retirement.queued) != 0 || strings.Contains(steps, "hide-asset") {
				t.Fatal("shared asset retired")
			}
		} else if result.PrivateRetainedUntil == nil || !result.PrivateRetainedUntil.Equal(now.Add(30*24*time.Hour)) || len(tx.retirement.queued) != 2 || !tx.retirement.deadlines[0].Equal(*result.PrivateRetainedUntil) || !tx.retirement.deadlines[1].Equal(now) {
			t.Fatal("retirement deadline lost")
		}
	}
}

func TestPrimaryReplacementConflictsDoNotWrite(t *testing.T) {
	for _, name := range []string{"missing", "stale", "withdrawn-asset", "takedown-parent", "merged-parent", "changed-while-locking"} {
		t.Run(name, func(t *testing.T) {
			tx := newPrimaryTx()
			switch name {
			case "missing":
				tx.primary = ""
			case "stale":
				tx.primary = "different"
			case "withdrawn-asset":
				tx.withdrawn = true
			case "takedown-parent":
				tx.status = "takedown"
			case "merged-parent":
				tx.status = "merged"
			case "changed-while-locking":
				tx.rechecked = "different"
			}
			result, err := replacePrimaryMedia(context.Background(), tx.begin, replacementFixture(), "actor", "request", time.Now())
			if !errors.Is(err, operations.ErrConflict) || tx.committed || result.Media.AssetID != "" || strings.Contains(strings.Join(tx.steps, ","), "hide-link") {
				t.Fatalf("conflict mutated data: %v %v", tx.steps, err)
			}
		})
	}
}

func TestPrimaryReplacementFailuresRollback(t *testing.T) {
	for _, step := range []string{"new-asset", "hide-link", "hide-asset", "replace-audit", "cache", "delete"} {
		t.Run(step, func(t *testing.T) {
			tx := newPrimaryTx()
			tx.failStep = step
			tx.failOutbox = step == "cache"
			tx.retirement.failQueue = step == "delete"
			result, err := replacePrimaryMedia(context.Background(), tx.begin, replacementFixture(), "actor", "request", time.Now())
			if err == nil || tx.committed || result.Media.AssetID != "" || tx.steps[len(tx.steps)-1] != "rollback" {
				t.Fatalf("failure committed: %v %v", tx.steps, err)
			}
		})
	}
}

func TestPrimaryReplacementRejectsInvalidIntentBeforeTransaction(t *testing.T) {
	for _, mutate := range []func(*operations.ReplacePrimaryMediaInput){
		func(in *operations.ReplacePrimaryMediaInput) { in.Manifest.IsPrimary = false },
		func(in *operations.ReplacePrimaryMediaInput) { in.ExpectedAssetID = "" },
		func(in *operations.ReplacePrimaryMediaInput) {
			in.ExpectedAssetID = strings.ToUpper(in.Manifest.AssetID)
		},
	} {
		input := replacementFixture()
		mutate(&input)
		tx := newPrimaryTx()
		_, err := replacePrimaryMedia(context.Background(), tx.begin, input, "actor", "request", time.Now())
		if !errors.Is(err, operations.ErrInvalidInput) || len(tx.steps) > 0 {
			t.Fatalf("invalid intent began transaction: %v", err)
		}
	}
}
