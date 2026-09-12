package historyarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validHistoryContractURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "postgres" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" && parsed.User != nil &&
		parsed.Path == "/self_deepsearch_worker_test" && (parsed.RawQuery == "" || parsed.RawQuery == "sslmode=disable") &&
		!parsed.ForceQuery && parsed.Fragment == "" && !strings.Contains(value, "#")
}

func sameHistoryContractDatabase(adminURL, runtimeURL string) bool {
	if !validHistoryContractURL(adminURL) || !validHistoryContractURL(runtimeURL) {
		return false
	}
	admin, _ := url.Parse(adminURL)
	runtime, _ := url.Parse(runtimeURL)
	return admin.Host == runtime.Host
}

func TestHistoryContractGuard(t *testing.T) {
	valid := "postgres://test:password@127.0.0.1:5432/self_deepsearch_worker_test"
	if !validHistoryContractURL(valid) || !validHistoryContractURL(valid+"?sslmode=disable") ||
		!sameHistoryContractDatabase(valid, valid+"?sslmode=disable") ||
		sameHistoryContractDatabase(valid, strings.Replace(valid, ":5432/", ":5433/", 1)) {
		t.Fatal("safe configuration or same-server guard failed")
	}
	for _, value := range []string{
		strings.Replace(valid, "127.0.0.1", "example.test", 1), strings.Replace(valid, "worker_test", "production", 1),
		valid + "?host=example.test", valid + "#", valid + "?", "host=127.0.0.1 dbname=self_deepsearch_worker_test",
	} {
		if validHistoryContractURL(value) {
			t.Fatal("unsafe contract configuration accepted")
		}
	}
}

// This opt-in contract runs serially in the dedicated, unseeded worker database.
// Only synthetic history is inserted. Recovery uses a transaction-local TEMP
// table, never a production table and never an undelete operation. Append-only
// manifests remain as synthetic evidence until the whole test database is disposed.
func TestPostgresHistoryArchiveContract(t *testing.T) {
	databaseURL := os.Getenv("HISTORY_ARCHIVE_CONTRACT_DATABASE_URL")
	adminURL := os.Getenv("HISTORY_ARCHIVE_CONTRACT_ADMIN_DATABASE_URL")
	if databaseURL == "" && adminURL == "" {
		t.Skip("dedicated history archive PostgreSQL contract database is not configured")
	}
	if os.Getenv("CONFIRM_HISTORY_ARCHIVE_CONTRACT") != "disposable-database" || !sameHistoryContractDatabase(adminURL, databaseURL) {
		t.Fatal("explicit disposable loopback contract configuration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("cannot open restricted test database")
	}
	defer pool.Close()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open disposable database guard")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire test guard")
	}
	defer guard.Release()
	var locked bool
	if err := guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("worker contracts must run serially in the isolated database")
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = guard.Exec(cleanup, `SELECT pg_advisory_unlock(hashtext('release-a-worker-outbox-contract'))`)
	}()
	var restricted bool
	if err := pool.QueryRow(ctx, `SELECT current_user='platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls
AND pg_has_role(current_user,'platform_worker','member') AND current_database()='self_deepsearch_worker_test'
AND NOT has_table_privilege(current_user,'platform.users','SELECT')
AND NOT has_table_privilege(current_user,'platform.view_history','DELETE')
AND NOT has_table_privilege(current_user,'audit.archived_history_manifests','UPDATE')
FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("restricted worker login without private-account reads or delete permissions required")
	}
	var version, userData int
	if err := admin.QueryRow(ctx, `SELECT schema_version,
 (SELECT count(*) FROM platform.users) + (SELECT count(*) FROM platform.works) +
 (SELECT count(*) FROM platform.view_history) + (SELECT count(*) FROM platform.media_assets) +
 (SELECT count(*) FROM platform.outbox_events)
FROM platform.system_metadata WHERE singleton`).Scan(&version, &userData); err != nil || version != 21 || userData != 0 {
		t.Fatal("schema 21 and empty dedicated worker database required")
	}
	userIDs := []string{fixtureRecords()[0].UserID, fixtureRecords()[1].UserID}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, `DELETE FROM platform.view_history WHERE user_id=ANY($1::uuid[])`, userIDs); err != nil {
			t.Error("cannot clean synthetic history fixtures")
		}
		if _, err := admin.Exec(cleanup, `DELETE FROM platform.users WHERE user_id=ANY($1::uuid[])`, userIDs); err != nil {
			t.Error("cannot clean synthetic history users")
		}
	}()
	for index, userID := range userIDs {
		if _, err := admin.Exec(ctx, `INSERT INTO platform.users (user_id,normalized_email,password_hash,account_status,closed_at)
VALUES ($1::uuid,$2,'synthetic-history-contract-never-login','closed',now())`, userID, []string{"history-one@example.invalid", "history-two@example.invalid"}[index]); err != nil {
			t.Fatalf("insert synthetic user: %v", err)
		}
	}
	now := time.Date(2026, 9, 12, 1, 2, 3, 654321000, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)
	directory := t.TempDir()
	repository, err := NewPostgresRepository(pool, directory)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(user int, viewed time.Time, deleted, archived *time.Time) Record {
		t.Helper()
		record := Record{UserID: userIDs[user], ContentType: "work", ContentID: fixtureRecords()[0].ContentID, ViewedAt: viewed}
		if deleted != nil {
			record.DeletedAt = *deleted
		}
		if err := admin.QueryRow(ctx, `INSERT INTO platform.view_history
(user_id,content_type,content_id,viewed_at,is_deleted,deleted_at,archived_at)
VALUES ($1::uuid,$2,$3::uuid,$4,$5,$6,$7) RETURNING history_id`,
			record.UserID, record.ContentType, record.ContentID, viewed, deleted != nil, deleted, archived).Scan(&record.HistoryID); err != nil {
			t.Fatalf("insert synthetic history: %v", err)
		}
		return record
	}
	old := cutoff.Add(-time.Hour)
	recent := cutoff.Add(time.Microsecond)
	visible := insert(0, old.Add(-time.Hour), nil, nil)
	first := insert(0, old, nil, nil)
	second := insert(1, old, nil, nil)
	boundary := insert(0, old.Add(time.Microsecond), nil, nil)
	first.DeletedAt, second.DeletedAt, boundary.DeletedAt = old, old, cutoff
	tooRecent := insert(1, old, &recent, nil)
	alreadyArchived := insert(1, old, &old, &old)
	eligible := map[int64]Record{first.HistoryID: first, second.HistoryID: second, boundary.HistoryID: boundary}
	var results []Result
	var restoredIDs []int64

	if !t.Run("soft_delete_retains_rows_before_archiving", func(t *testing.T) {
		for _, record := range []Record{first, second, boundary} {
			command, err := admin.Exec(ctx, `UPDATE platform.view_history SET is_deleted=true,deleted_at=$2
WHERE history_id=$1 AND NOT is_deleted`, record.HistoryID, record.DeletedAt)
			if err != nil || command.RowsAffected() != 1 {
				t.Fatalf("soft-delete synthetic history: %v", err)
			}
			assertNotArchived(t, ctx, pool, record.HistoryID)
		}
		var total, visibleCount int
		if err := admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE NOT is_deleted)
FROM platform.view_history`).Scan(&total, &visibleCount); err != nil || total != 6 || visibleCount != 1 {
			t.Fatal("soft deletion physically removed history or left cleared rows visible")
		}
	}) {
		return
	}
	if !t.Run("locked_rows_are_skipped_and_batch_limit_respected", func(t *testing.T) {
		blocker, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback(ctx)
		if _, err := blocker.Exec(ctx, `SELECT history_id FROM platform.view_history WHERE history_id=$1 FOR UPDATE`, first.HistoryID); err != nil {
			t.Fatal(err)
		}
		result, err := repository.ArchiveEligible(ctx, cutoff, now, 2)
		if err != nil || result.RecordCount != 2 {
			t.Fatalf("archive unlocked batch: result=%#v error=%v", result, err)
		}
		results = append(results, result)
		var archived bool
		if err := admin.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM platform.view_history WHERE history_id=$1`, first.HistoryID).Scan(&archived); err != nil || archived {
			t.Fatal("locked history row was archived")
		}
	}) {
		return
	}
	if !t.Run("remaining_batch_and_idempotent_retry", func(t *testing.T) {
		result, err := repository.ArchiveEligible(ctx, cutoff, now, 2)
		if err != nil || result.RecordCount != 1 {
			t.Fatalf("archive remaining batch: result=%#v error=%v", result, err)
		}
		results = append(results, result)
		if result, err := repository.ArchiveEligible(ctx, cutoff, now, 2); err != nil || result != (Result{}) {
			t.Fatalf("repeat archive must be empty: result=%#v error=%v", result, err)
		}
	}) {
		return
	}

	if !t.Run("manifest_file_and_temp_table_restore_match_exactly", func(t *testing.T) {
		for _, result := range results {
			manifest := readContractManifest(t, ctx, pool, result.StoragePath)
			if manifest.RecordCount != result.RecordCount || manifest.ByteSize != result.ByteSize || manifest.SHA256 != result.SHA256 {
				t.Fatal("file result and durable manifest do not match")
			}
			records, err := VerifyFile(directory, manifest)
			if err != nil {
				t.Fatalf("verify real SQL archive: %v", err)
			}
			path := filepath.Join(directory, filepath.FromSlash(result.StoragePath))
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("history archive must have private file permissions")
			}
			info, err = os.Stat(filepath.Dir(path))
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatal("history archive directory must have private permissions")
			}
			for _, record := range records {
				want, exists := eligible[record.HistoryID]
				if !exists || record.UserID != want.UserID || record.ContentType != want.ContentType || record.ContentID != want.ContentID ||
					!record.ViewedAt.Equal(want.ViewedAt) || !record.DeletedAt.Equal(want.DeletedAt) {
					t.Fatal("archive changed UUIDs, exact timestamps or selection scope")
				}
				restoredIDs = append(restoredIDs, record.HistoryID)
			}
			verifyTempRestore(t, ctx, admin, records, now)
			// A copied artifact with one changed byte must fail closed.
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)/2] ^= 0xff
			if err := os.WriteFile(filepath.Join(directory, "corrupted-copy.gz"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			manifest.StoragePath = "corrupted-copy.gz"
			if _, err := VerifyFile(directory, manifest); err == nil {
				t.Fatal("corrupted copied archive passed restore verification")
			}
		}
		if len(restoredIDs) != len(eligible) {
			t.Fatal("restore did not cover all eligible history")
		}
	}) {
		return
	}
	if !t.Run("original_rows_retained_and_visibility_unchanged", func(t *testing.T) {
		var total, visibleCount, archivedCount int
		if err := admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE NOT is_deleted),
count(*) FILTER(WHERE history_id=ANY($1::bigint[]) AND is_deleted AND archived_at=$2)
FROM platform.view_history`, restoredIDs, now).Scan(&total, &visibleCount, &archivedCount); err != nil || total != 6 || visibleCount != 1 || archivedCount != 3 {
			t.Fatal("archiving deleted rows or changed visible history")
		}
		for _, record := range []Record{visible, tooRecent} {
			var untouched bool
			if err := admin.QueryRow(ctx, `SELECT archived_at IS NULL FROM platform.view_history WHERE history_id=$1`, record.HistoryID).Scan(&untouched); err != nil || !untouched {
				t.Fatal("visible or too-recent history was archived")
			}
		}
		var originalTime time.Time
		if err := admin.QueryRow(ctx, `SELECT archived_at FROM platform.view_history WHERE history_id=$1`, alreadyArchived.HistoryID).Scan(&originalTime); err != nil || !originalTime.Equal(old) {
			t.Fatal("previous archive marker changed")
		}
	}) {
		return
	}

	if !t.Run("filesystem_failure_does_not_mark_rows", func(t *testing.T) {
		candidate := insert(0, old.Add(time.Second), &old, nil)
		badPath := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(badPath, []byte("synthetic blocker"), 0o600); err != nil {
			t.Fatal(err)
		}
		badRepository, err := NewPostgresRepository(pool, badPath)
		if err != nil {
			t.Fatal(err)
		}
		before := manifestCount(t, ctx, pool)
		if _, err := badRepository.ArchiveEligible(ctx, cutoff, now, 2); err == nil {
			t.Fatal("filesystem failure was ignored")
		}
		assertNotArchived(t, ctx, pool, candidate.HistoryID)
		if manifestCount(t, ctx, pool) != before {
			t.Fatal("failed file creation committed a manifest")
		}
		if result, err := repository.ArchiveEligible(ctx, cutoff, now, 2); err != nil || result.RecordCount != 1 {
			t.Fatalf("filesystem failure retry failed: %v", err)
		}
	}) {
		return
	}
	for _, failureLevel := range []string{"leaf", "base_parent"} {
		if !t.Run("directory_sync_failure_"+failureLevel+"_does_not_commit", func(t *testing.T) {
			candidate := insert(0, old.Add(2*time.Second), &old, nil)
			failureRepository := *repository
			failureRepository.baseDir = filepath.Join(t.TempDir(), "new", "archive-root")
			leaf := filepath.Join(failureRepository.baseDir, "history", now.Format("2006"), now.Format("01"))
			target := leaf
			if failureLevel == "base_parent" {
				target = filepath.Dir(failureRepository.baseDir)
			}
			injected := errors.New("synthetic archive directory sync failure")
			failureRepository.syncDirectory = func(directory string) error {
				if directory == target {
					return injected
				}
				return syncArchiveDirectory(directory)
			}
			before := manifestCount(t, ctx, pool)
			if result, err := failureRepository.ArchiveEligible(ctx, cutoff, now, 2); !errors.Is(err, injected) || result != (Result{}) {
				t.Fatalf("directory sync failure did not propagate: %v", err)
			}
			assertNotArchived(t, ctx, pool, candidate.HistoryID)
			if manifestCount(t, ctx, pool) != before {
				t.Fatal("directory sync failure committed an archive manifest")
			}
			files, err := filepath.Glob(filepath.Join(leaf, "*.jsonl.gz"))
			if err != nil || len(files) != 1 {
				t.Fatal("directory sync failure did not occur after file installation")
			}
			original, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			failureRepository.syncDirectory = nil
			result, err := failureRepository.ArchiveEligible(ctx, cutoff, now, 2)
			if err != nil || result.RecordCount != 1 || manifestCount(t, ctx, pool) != before+1 {
				t.Fatalf("directory sync retry failed: %v", err)
			}
			after, err := os.Stat(files[0])
			if err != nil || !os.SameFile(original, after) {
				t.Fatal("directory sync retry replaced existing evidence")
			}
			manifest := readContractManifest(t, ctx, pool, result.StoragePath)
			if _, err := VerifyFile(failureRepository.baseDir, manifest); err != nil {
				t.Fatalf("verify directory sync retry: %v", err)
			}
		}) {
			return
		}
	}
	if !t.Run("commit_failure_rolls_back_and_reuses_orphan_file", func(t *testing.T) {
		candidate := insert(1, old.Add(2*time.Second), &old, nil)
		// Inject a real deferred constraint failure: the file has been installed,
		// INSERT and UPDATE succeeded, but COMMIT must fail and roll both back.
		if _, err := guard.Exec(ctx, `CREATE FUNCTION pg_temp.history_archive_contract_reject() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic history archive commit failure'; END $$;
CREATE CONSTRAINT TRIGGER history_archive_contract_reject AFTER UPDATE ON platform.view_history
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.archived_at IS NOT NULL)
EXECUTE FUNCTION pg_temp.history_archive_contract_reject()`); err != nil {
			t.Fatalf("install synthetic commit failure: %v", err)
		}
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			if _, err := guard.Exec(cleanup, `DROP TRIGGER IF EXISTS history_archive_contract_reject ON platform.view_history;
DROP FUNCTION IF EXISTS pg_temp.history_archive_contract_reject()`); err != nil {
				t.Error("cannot remove synthetic commit failure trigger")
			}
		}()
		before := manifestCount(t, ctx, pool)
		archiveDir := filepath.Join(directory, "history", now.Format("2006"), now.Format("01"))
		beforeFiles, err := filepath.Glob(filepath.Join(archiveDir, "*.jsonl.gz"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.ArchiveEligible(ctx, cutoff, now, 2); err == nil || !strings.Contains(err.Error(), "commit history archive") {
			t.Fatalf("expected deferred commit failure: %v", err)
		}
		assertNotArchived(t, ctx, pool, candidate.HistoryID)
		if manifestCount(t, ctx, pool) != before {
			t.Fatal("failed commit retained a manifest")
		}
		afterFiles, err := filepath.Glob(filepath.Join(archiveDir, "*.jsonl.gz"))
		if err != nil {
			t.Fatal(err)
		}
		previousFiles := make(map[string]bool, len(beforeFiles))
		for _, path := range beforeFiles {
			previousFiles[path] = true
		}
		var orphans []string
		for _, path := range afterFiles {
			if !previousFiles[path] {
				orphans = append(orphans, path)
			}
		}
		if len(orphans) != 1 {
			t.Fatal("failed commit must leave exactly one installed orphan for controlled retry")
		}
		path := orphans[0]
		original, err := os.Stat(path)
		if err != nil {
			t.Fatal("archive file was not retained after database rollback")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			t.Fatal(err)
		}
		orphan := Result{StoragePath: filepath.ToSlash(relative), RecordCount: 1, ByteSize: original.Size(), SHA256: hex.EncodeToString(digest[:])}
		if _, err := guard.Exec(ctx, `DROP TRIGGER history_archive_contract_reject ON platform.view_history;
DROP FUNCTION pg_temp.history_archive_contract_reject()`); err != nil {
			t.Fatal(err)
		}
		result, err := repository.ArchiveEligible(ctx, cutoff, now, 2)
		if err != nil || result != orphan || manifestCount(t, ctx, pool) != before+1 {
			t.Fatalf("retry did not adopt the verified orphan: %v", err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(original, after) {
			t.Fatal("retry replaced the existing matching archive")
		}
		manifest := readContractManifest(t, ctx, pool, result.StoragePath)
		records, err := VerifyFile(directory, manifest)
		if err != nil {
			t.Fatal(err)
		}
		verifyTempRestore(t, ctx, admin, records, now)
		partials, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".history-*.partial"))
		if err != nil || len(partials) != 0 {
			t.Fatal("successful retry left temporary archive files")
		}
	}) {
		return
	}
}

func readContractManifest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, path string) Manifest {
	t.Helper()
	var manifest Manifest
	var scope []byte
	if err := pool.QueryRow(ctx, `SELECT storage_path,schema_version,user_scope,range_start,range_end,record_count,byte_size,sha256
FROM audit.archived_history_manifests WHERE storage_path=$1`, path).Scan(&manifest.StoragePath, &manifest.SchemaVersion,
		&scope, &manifest.RangeStart, &manifest.RangeEnd, &manifest.RecordCount, &manifest.ByteSize, &manifest.SHA256); err != nil {
		t.Fatalf("read durable history manifest: %v", err)
	}
	if err := json.Unmarshal(scope, &manifest.UserScope); err != nil {
		t.Fatal("invalid durable manifest user scope")
	}
	return manifest
}

func manifestCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.archived_history_manifests`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertNotArchived(t *testing.T, ctx context.Context, pool *pgxpool.Pool, historyID int64) {
	t.Helper()
	var safe bool
	if err := pool.QueryRow(ctx, `SELECT is_deleted AND archived_at IS NULL FROM platform.view_history WHERE history_id=$1`, historyID).Scan(&safe); err != nil || !safe {
		t.Fatal("failed archive changed the durable deletion/archive markers")
	}
}

func verifyTempRestore(t *testing.T, ctx context.Context, admin *pgxpool.Pool, records []Record, archivedAt time.Time) {
	t.Helper()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE recovered_history (
history_id bigint PRIMARY KEY,user_id uuid NOT NULL,content_type text NOT NULL,content_id uuid NOT NULL,
viewed_at timestamptz NOT NULL,is_deleted boolean NOT NULL CHECK(is_deleted),deleted_at timestamptz NOT NULL,
archived_at timestamptz NOT NULL) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	rows := make([][]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, []any{record.HistoryID, record.UserID, record.ContentType, record.ContentID,
			record.ViewedAt, true, record.DeletedAt, archivedAt})
	}
	if count, err := tx.CopyFrom(ctx, pgx.Identifier{"pg_temp", "recovered_history"},
		[]string{"history_id", "user_id", "content_type", "content_id", "viewed_at", "is_deleted", "deleted_at", "archived_at"},
		pgx.CopyFromRows(rows)); err != nil || count != int64(len(records)) {
		t.Fatalf("restore verified records into TEMP table: %v", err)
	}
	var mismatch, visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE original.history_id IS NULL OR
(restored.user_id,restored.content_type,restored.content_id,restored.viewed_at,restored.is_deleted,restored.deleted_at,restored.archived_at)
IS DISTINCT FROM (original.user_id,original.content_type,original.content_id,original.viewed_at,original.is_deleted,original.deleted_at,original.archived_at)),
count(*) FILTER(WHERE NOT restored.is_deleted)
FROM pg_temp.recovered_history restored LEFT JOIN platform.view_history original USING(history_id)`).Scan(&mismatch, &visible); err != nil || mismatch != 0 || visible != 0 {
		t.Fatalf("restored history differs from retained originals or became visible: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
