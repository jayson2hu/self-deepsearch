package database

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/operations"
)

type retirementRows struct {
	pgx.Rows
	data  [][]any
	index int
	close func()
}

func (rows *retirementRows) Next() bool { rows.index++; return rows.index <= len(rows.data) }
func (rows *retirementRows) Err() error { return nil }
func (rows *retirementRows) Close() {
	if rows.close != nil {
		rows.close()
		rows.close = nil
	}
}
func (rows *retirementRows) Scan(dest ...any) error {
	values := rows.data[rows.index-1]
	if len(dest) != len(values) {
		return errors.New("unexpected retirement row shape")
	}
	for i, value := range values {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.SetZero()
		} else {
			target.Set(reflect.ValueOf(value))
		}
	}
	return nil
}

type retirementTx struct {
	pgx.Tx
	steps       []string
	objects     []retiredMediaObject
	queued      []retiredMediaObject
	deadlines   []time.Time
	queueSQL    string
	failQueue   bool
	withdrawSQL string
	previous    []retiredMediaObject
}

func (tx *retirementTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "SELECT event_id::text") {
		tx.steps = append(tx.steps, "lock-events")
		if !strings.Contains(sql, "ORDER BY event_id FOR UPDATE") {
			return nil, errors.New("missing event lock")
		}
		rows := &retirementRows{close: func() { tx.steps = append(tx.steps, "close-events") }}
		for _, previous := range tx.previous {
			payload, err := json.Marshal(previous)
			if err != nil {
				return nil, err
			}
			rows.data = append(rows.data, []any{"event", "asset", payload, time.Now().UTC()})
		}
		return rows, nil
	}
	tx.steps = append(tx.steps, "withdraw-objects")
	tx.withdrawSQL = sql
	rows := &retirementRows{close: func() { tx.steps = append(tx.steps, "close-objects") }}
	for _, object := range tx.objects {
		rows.data = append(rows.data, []any{object.ID, object.AssetID, object.StorageScope, object.StorageKey, object.BackupPath, object.PublicURL})
	}
	return rows, nil
}

func (tx *retirementTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.steps = append(tx.steps, "queue")
	tx.queueSQL = sql
	if tx.failQueue {
		return pgconn.CommandTag{}, errors.New("synthetic queue failure")
	}
	var object retiredMediaObject
	if err := json.Unmarshal(args[1].([]byte), &object); err != nil {
		return pgconn.CommandTag{}, err
	}
	if object.ID != args[2] || object.AssetID != args[0] {
		return pgconn.CommandTag{}, errors.New("lost immutable identity")
	}
	tx.queued = append(tx.queued, object)
	tx.deadlines = append(tx.deadlines, args[3].(time.Time))
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func retiredObjectFixture(scope string) retiredMediaObject {
	object := retiredMediaObject{ID: "object", AssetID: "asset", StorageScope: scope, StorageKey: "media-master/asset/v1/master.webp"}
	if scope == "public" {
		object.StorageKey = "media-public/default/works/work/asset/v1/cover-w320.webp"
		publicURL := "https://media.example.test/" + object.StorageKey
		object.PublicURL = &publicURL
	}
	object.BackupPath = object.StorageKey
	return object
}

func TestMediaRetirementLocksEventsBeforeObjectsAndPreservesDeadlines(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(30 * 24 * time.Hour)
	tx := &retirementTx{objects: []retiredMediaObject{retiredObjectFixture("private"), retiredObjectFixture("public")}}
	if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, deadline, now); err != nil {
		t.Fatal(err)
	}
	if strings.Join(tx.steps, ",") != "lock-events,close-events,withdraw-objects,close-objects,queue,queue" {
		t.Fatalf("unsafe event/object lock or cursor order: %v", tx.steps)
	}
	if !tx.deadlines[0].Equal(deadline) || !tx.deadlines[1].Equal(now) {
		t.Fatal("public deletion delayed or private retention shortened")
	}
	for _, predicate := range []string{
		"targets AS MATERIALIZED", "object.public_url", "FOR UPDATE OF object", "object.object_status <> 'deleted'",
	} {
		if !strings.Contains(tx.withdrawSQL, predicate) {
			t.Fatalf("lost original URL / identity boundary: %s", predicate)
		}
	}
	update := strings.Split(tx.queueSQL, "DO UPDATE")[1]
	if !strings.Contains(tx.queueSQL, "'media-delete:' || $3::text") ||
		!strings.Contains(update, "least(existing.available_at, EXCLUDED.available_at)") ||
		!strings.Contains(update, "existing.status IN ('pending', 'retry')") ||
		strings.Contains(update, "payload =") || strings.Contains(update, "status =") || strings.Contains(update, "attempt_count =") {
		t.Fatal("duplicate deletion can overwrite identity, delay work or reset its lease/retry state")
	}
}

func TestMediaRetirementRestoresOriginalURLOnlyFromMatchingLegacyPayload(t *testing.T) {
	original := retiredObjectFixture("public")
	original.ID = "" // Old tasks did not include media_object_id.
	withdrawn := retiredObjectFixture("public")
	withdrawn.PublicURL = nil
	now := time.Now().UTC()
	tx := &retirementTx{objects: []retiredMediaObject{withdrawn}, previous: []retiredMediaObject{original}}
	if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, now, now); err != nil {
		t.Fatal(err)
	}
	if len(tx.queued) != 1 || tx.queued[0].PublicURL == nil || *tx.queued[0].PublicURL != *original.PublicURL || tx.queued[0].ID != withdrawn.ID {
		t.Fatal("legacy recovery lost the original URL or current immutable object ID")
	}
	mutations := []func(*retiredMediaObject){
		func(object *retiredMediaObject) { object.AssetID = "other-asset" },
		func(object *retiredMediaObject) { object.StorageScope = "private" },
		func(object *retiredMediaObject) {
			object.StorageKey += ".other"
			object.BackupPath = object.StorageKey
			value := "https://media.example.test/" + object.StorageKey
			object.PublicURL = &value
		},
		func(object *retiredMediaObject) { object.BackupPath += ".other" },
		func(object *retiredMediaObject) { object.PublicURL = nil },
		func(object *retiredMediaObject) {
			value := "https://media.example.test/wrong.webp"
			object.PublicURL = &value
		},
	}
	for i, mutate := range mutations {
		unrelated := original
		mutate(&unrelated)
		tx := &retirementTx{objects: []retiredMediaObject{withdrawn}, previous: []retiredMediaObject{unrelated}}
		if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, now, now); err == nil || len(tx.queued) != 0 {
			t.Fatalf("reused unrelated legacy evidence %d", i)
		}
	}
}

func TestMediaRetirementRightsDeadlineIsImmediateAndMissingURLFails(t *testing.T) {
	now := time.Now().UTC()
	tx := &retirementTx{objects: []retiredMediaObject{retiredObjectFixture("private")}}
	if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, now, now); err != nil || !tx.deadlines[0].Equal(now) {
		t.Fatalf("rights request retained private object: %v", err)
	}
	object := retiredObjectFixture("public")
	object.PublicURL = nil
	tx = &retirementTx{objects: []retiredMediaObject{object}}
	if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, now, now); err == nil || len(tx.queued) != 0 {
		t.Fatal("missing preserved public URL generated an invalid successful deletion task")
	}
	tx = &retirementTx{objects: []retiredMediaObject{retiredObjectFixture("public")}, failQueue: true}
	if err := retireMediaObjects(context.Background(), tx, []string{"asset"}, now, now); err == nil {
		t.Fatal("queue failure was swallowed")
	}
}

func TestRetiredMediaValidationRejectsAmbiguousOrUnsafeLocations(t *testing.T) {
	mutations := []func(*retiredMediaObject){
		func(object *retiredMediaObject) { object.ID = "" },
		func(object *retiredMediaObject) { object.BackupPath += ".other" },
		func(object *retiredMediaObject) {
			object.StorageKey = "media-public/../other"
			object.BackupPath = object.StorageKey
		},
		func(object *retiredMediaObject) { object.StorageScope = "private" },
		func(object *retiredMediaObject) { object.PublicURL = nil },
		func(object *retiredMediaObject) {
			value := "http://media.example.test/" + object.StorageKey
			object.PublicURL = &value
		},
		func(object *retiredMediaObject) {
			value := *object.PublicURL + "?token=secret"
			object.PublicURL = &value
		},
		func(object *retiredMediaObject) {
			value := "https://user:pass@media.example.test/" + object.StorageKey
			object.PublicURL = &value
		},
		func(object *retiredMediaObject) {
			value := "https://media.example.test/wrong.webp"
			object.PublicURL = &value
		},
	}
	for i, mutate := range mutations {
		object := retiredObjectFixture("public")
		mutate(&object)
		if err := validateRetiredMediaObject(object); err == nil {
			t.Fatalf("accepted invalid case %d", i)
		}
	}
	for _, scope := range []string{"public", "private"} {
		if err := validateRetiredMediaObject(retiredObjectFixture(scope)); err != nil {
			t.Fatal(err)
		}
	}
}

type unpublishedTakedownTx struct {
	pgx.Tx
	missingParent bool
	queued        bool
}

func (tx *unpublishedTakedownTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "UPDATE platform.publications") || (tx.missingParent && strings.Contains(sql, "UPDATE platform.works")) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if strings.Contains(sql, "INSERT INTO platform.outbox_events") {
		tx.queued = true
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (*unpublishedTakedownTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &retirementRows{}, nil
}

func TestTakedownAllowsUnpublishedParentWithoutCreatingPublication(t *testing.T) {
	for _, entityType := range []string{"work", "performer", "studio"} {
		tx := &unpublishedTakedownTx{}
		if err := applyTakedown(context.Background(), tx, entityType, "entity", "request", time.Now()); err != nil || !tx.queued {
			t.Fatalf("cannot withdraw %s without an existing publication: %v", entityType, err)
		}
	}
	tx := &unpublishedTakedownTx{missingParent: true}
	if err := applyTakedown(context.Background(), tx, "work", "missing", "request", time.Now()); !errors.Is(err, operations.ErrNotFound) || tx.queued {
		t.Fatalf("missing entity must still be rejected: %v", err)
	}
}
