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

type mediaRegistrationRow func(...any) error

func (row mediaRegistrationRow) Scan(dest ...any) error { return row(dest...) }

type mediaRegistrationTx struct {
	pgx.Tx
	status        string
	primaryExists bool
	galleryCount  int
	positionUsed  bool
	steps         []string
	failOutbox    bool
	outboxSQL     string
	outboxArgs    []any
	committed     bool
}

func (tx *mediaRegistrationTx) begin(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
func (tx *mediaRegistrationTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return mediaRegistrationRow(func(dest ...any) error {
		switch {
		case strings.Contains(sql, "FOR UPDATE"):
			tx.steps = append(tx.steps, "lock")
			*dest[0].(*string) = "entity"
		case strings.Contains(sql, "SELECT publication_status"):
			tx.steps = append(tx.steps, "status")
			*dest[0].(*string) = tx.status
		case strings.Contains(sql, "count(*) FILTER"):
			tx.steps = append(tx.steps, "slots")
			*dest[0].(*bool) = tx.primaryExists
			*dest[1].(*int) = tx.galleryCount
			*dest[2].(*bool) = tx.positionUsed
		case strings.Contains(sql, "INSERT INTO platform.media_assets"):
			tx.steps = append(tx.steps, "asset")
			*dest[0].(*string), *dest[1].(*time.Time) = "asset", time.Now()
		case strings.Contains(sql, "INSERT INTO platform.media_objects"):
			tx.steps = append(tx.steps, "object")
			*dest[0].(*string) = "object"
		case strings.Contains(sql, "INSERT INTO platform.entity_media"):
			tx.steps = append(tx.steps, "link")
			*dest[0].(*string) = "link"
		default:
			return errors.New("unexpected media registration query")
		}
		return nil
	})
}
func (tx *mediaRegistrationTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := "other"
	if strings.Contains(sql, "INSERT INTO platform.outbox_events") {
		step = "outbox"
		tx.outboxSQL, tx.outboxArgs = sql, args
		if tx.failOutbox {
			tx.steps = append(tx.steps, step)
			return pgconn.CommandTag{}, errors.New("synthetic outbox failure")
		}
	}
	if strings.Contains(sql, "INSERT INTO audit.audit_logs") {
		step = "audit"
	}
	tx.steps = append(tx.steps, step)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (tx *mediaRegistrationTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	tx.committed = true
	return nil
}
func (tx *mediaRegistrationTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback")
	return nil
}

func registrationFixture() operations.MediaManifestInput {
	input := operations.MediaManifestInput{AssetID: "asset", EntityID: "entity", EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 0, IsPrimary: true}
	for _, rendition := range []string{"master", "w320", "w640", "w960"} {
		input.Objects = append(input.Objects, operations.MediaObjectInput{Rendition: rendition})
	}
	return input
}

func TestMediaRegistrationRejectsInvalidDisplayAssociationBeforeTransaction(t *testing.T) {
	cases := []operations.MediaManifestInput{
		{EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 0, IsPrimary: false},
		{EntityType: "work", AssetType: "work_image", Purpose: "cover", Position: 1, IsPrimary: true},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 0, IsPrimary: false},
		{EntityType: "work", AssetType: "work_image", Purpose: "gallery", Position: 4, IsPrimary: false},
		{EntityType: "performer", AssetType: "performer_avatar", Purpose: "avatar", Position: 0, IsPrimary: false},
	}
	for _, input := range cases {
		tx := &mediaRegistrationTx{status: "draft"}
		result, err := registerMediaManifest(context.Background(), tx.begin, input, "actor", "request", time.Now())
		if !errors.Is(err, operations.ErrInvalidInput) || result.AssetID != "" || len(tx.steps) != 0 {
			t.Fatalf("invalid association reached transaction: input=%#v result=%#v err=%v steps=%v", input, result, err, tx.steps)
		}
	}
}

func TestMediaRegistrationGalleryRequiresPrimaryAndAvailableSlot(t *testing.T) {
	input := registrationFixture()
	input.Purpose, input.Position, input.IsPrimary = "gallery", 1, false
	for _, scenario := range []struct {
		name          string
		primaryExists bool
		galleryCount  int
		positionUsed  bool
	}{
		{name: "missing_primary", primaryExists: false, galleryCount: 0},
		{name: "three_galleries", primaryExists: true, galleryCount: 3},
		{name: "occupied_position", primaryExists: true, galleryCount: 1, positionUsed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx := &mediaRegistrationTx{
				status: "draft", primaryExists: scenario.primaryExists,
				galleryCount: scenario.galleryCount, positionUsed: scenario.positionUsed,
			}
			result, err := registerMediaManifest(context.Background(), tx.begin, input, "actor", "request", time.Now())
			steps := strings.Join(tx.steps, ",")
			if !errors.Is(err, operations.ErrConflict) || result.AssetID != "" || tx.committed || !strings.Contains(steps, "lock,status,slots") || strings.Contains(steps, "asset") {
				t.Fatalf("gallery slot guard failed: result=%#v err=%v steps=%s", result, err, steps)
			}
		})
	}
}

func TestMediaRegistrationQueuesCacheRefreshInSameTransaction(t *testing.T) {
	for _, status := range []string{"draft", "reviewing", "approved", "published", "hidden"} {
		t.Run(status, func(t *testing.T) {
			tx := &mediaRegistrationTx{status: status}
			_, err := registerMediaManifest(context.Background(), tx.begin, registrationFixture(), "actor", "request", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			steps := strings.Join(tx.steps, ",")
			if !strings.HasPrefix(steps, "lock,status,asset,") || !strings.Contains(steps, "link,outbox,audit,commit,rollback") {
				t.Fatalf("media publication omitted status guard or cache event: %s", steps)
			}
			if !strings.Contains(tx.outboxSQL, "'cache_purge'") || !strings.Contains(tx.outboxSQL, "dedupe_key") {
				t.Fatal("media cache event lacks type or immutable dedupe key")
			}
			if len(tx.outboxArgs) != 3 || tx.outboxArgs[0] != "work" || tx.outboxArgs[1] != "entity" || tx.outboxArgs[2] != "asset" {
				t.Fatal("cache event lost entity and asset identity")
			}
		})
	}
}

func TestMediaRegistrationRejectsWithdrawnOrMergedTargets(t *testing.T) {
	for _, status := range []string{"takedown", "merged", "unknown"} {
		tx := &mediaRegistrationTx{status: status}
		result, err := registerMediaManifest(context.Background(), tx.begin, registrationFixture(), "actor", "request", time.Now())
		if !errors.Is(err, operations.ErrConflict) || tx.committed || result.AssetID != "" || strings.Contains(strings.Join(tx.steps, ","), "asset") {
			t.Fatalf("media attached to %s target: %v", status, err)
		}
	}
}

func TestMediaRegistrationOutboxFailureDoesNotPublish(t *testing.T) {
	tx := &mediaRegistrationTx{status: "published", failOutbox: true}
	result, err := registerMediaManifest(context.Background(), tx.begin, registrationFixture(), "actor", "request", time.Now())
	if err == nil || tx.committed || result.AssetID != "" || tx.steps[len(tx.steps)-1] != "rollback" {
		t.Fatal("media registration committed without durable cache refresh")
	}
}
