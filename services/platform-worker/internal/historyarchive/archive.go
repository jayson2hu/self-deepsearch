package historyarchive

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"self-deepsearch/services/platform-worker/internal/logsafe"
)

type Record struct {
	HistoryID   int64     `json:"history_id"`
	UserID      string    `json:"user_id"`
	ContentType string    `json:"content_type"`
	ContentID   string    `json:"content_id"`
	ViewedAt    time.Time `json:"viewed_at"`
	DeletedAt   time.Time `json:"deleted_at"`
}

type Result struct {
	StoragePath string
	RecordCount int
	ByteSize    int64
	SHA256      string
}

type Repository interface {
	ArchiveEligible(context.Context, time.Time, time.Time, int) (Result, error)
}

type Runner struct {
	Repository Repository
	Every      time.Duration
	MinimumAge time.Duration
	BatchSize  int
	Logger     *slog.Logger
	Now        func() time.Time
}

func (runner Runner) Run(ctx context.Context) error {
	if runner.Repository == nil {
		return errors.New("history archive repository is required")
	}
	if runner.Every <= 0 || runner.MinimumAge <= 0 || runner.BatchSize <= 0 {
		return errors.New("history archive interval, age and batch size must be positive")
	}
	if runner.Logger == nil {
		runner.Logger = slog.Default()
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
	runner.runOnce(ctx)
	ticker := time.NewTicker(runner.Every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runner.runOnce(ctx)
		}
	}
}

func (runner Runner) runOnce(ctx context.Context) {
	now := runner.Now().UTC()
	result, err := runner.Repository.ArchiveEligible(ctx, now.Add(-runner.MinimumAge), now, runner.BatchSize)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			runner.Logger.ErrorContext(ctx, "history_archive_failed", "error_class", logsafe.ErrorClass(err))
		}
		return
	}
	if result.RecordCount > 0 {
		runner.Logger.InfoContext(ctx, "history_archive_completed", "storage_path", result.StoragePath,
			"record_count", result.RecordCount, "byte_size", result.ByteSize, "sha256", result.SHA256)
	}
}

type PostgresRepository struct {
	pool          *pgxpool.Pool
	baseDir       string
	fileMode      os.FileMode
	dirMode       os.FileMode
	syncDirectory func(string) error
}

func NewPostgresRepository(pool *pgxpool.Pool, baseDir string) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("history archive database pool is required")
	}
	absolute, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve history archive directory: %w", err)
	}
	volumeRoot := filepath.Clean(filepath.VolumeName(absolute) + string(os.PathSeparator))
	if filepath.Clean(absolute) == volumeRoot {
		return nil, errors.New("history archive directory must not be a filesystem root")
	}
	return &PostgresRepository{pool: pool, baseDir: absolute, fileMode: 0o600, dirMode: 0o700}, nil
}

func (repository *PostgresRepository) ArchiveEligible(ctx context.Context, cutoff, archivedAt time.Time, limit int) (Result, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin history archive: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT history_id, user_id::text, content_type, content_id::text, viewed_at, deleted_at
FROM platform.view_history
WHERE is_deleted AND archived_at IS NULL AND deleted_at <= $1
ORDER BY viewed_at, history_id
FOR UPDATE SKIP LOCKED
LIMIT $2`, cutoff, limit)
	if err != nil {
		return Result{}, fmt.Errorf("select history archive batch: %w", err)
	}
	records := make([]Record, 0, limit)
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.HistoryID, &record.UserID, &record.ContentType, &record.ContentID, &record.ViewedAt, &record.DeletedAt); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan history archive record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, fmt.Errorf("iterate history archive records: %w", err)
	}
	rows.Close()
	if len(records) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("commit empty history archive: %w", err)
		}
		return Result{}, nil
	}

	result, err := repository.writeArchive(records, archivedAt)
	if err != nil {
		return Result{}, err
	}
	userIDs := make(map[string]struct{})
	historyIDs := make([]int64, 0, len(records))
	for _, record := range records {
		userIDs[record.UserID] = struct{}{}
		historyIDs = append(historyIDs, record.HistoryID)
	}
	userScopeIDs := make([]string, 0, len(userIDs))
	for userID := range userIDs {
		userScopeIDs = append(userScopeIDs, userID)
	}
	sort.Strings(userScopeIDs)
	userScope, err := json.Marshal(map[string]any{"kind": "explicit", "user_count": len(userScopeIDs), "user_ids": userScopeIDs})
	if err != nil {
		return Result{}, fmt.Errorf("encode history archive user scope: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit.archived_history_manifests (
  storage_path, schema_version, user_scope, range_start, range_end, record_count, byte_size, sha256
) VALUES ($1, 1, $2::jsonb, $3, $4, $5, $6, $7)
ON CONFLICT (storage_path) DO NOTHING`, result.StoragePath, userScope,
		records[0].ViewedAt, records[len(records)-1].ViewedAt, len(records), result.ByteSize, result.SHA256)
	if err != nil {
		return Result{}, fmt.Errorf("insert history archive manifest: %w", err)
	}
	command, err := tx.Exec(ctx, `
UPDATE platform.view_history SET archived_at = $2
WHERE history_id = ANY($1::bigint[]) AND is_deleted AND archived_at IS NULL`, historyIDs, archivedAt)
	if err != nil {
		return Result{}, fmt.Errorf("mark history archive records: %w", err)
	}
	if command.RowsAffected() != int64(len(records)) {
		return Result{}, errors.New("history archive batch changed while locked")
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit history archive: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) writeArchive(records []Record, archivedAt time.Time) (Result, error) {
	relativeDir := filepath.Join("history", archivedAt.UTC().Format("2006"), archivedAt.UTC().Format("01"))
	directory := filepath.Join(repository.baseDir, relativeDir)
	if err := os.MkdirAll(directory, repository.dirMode); err != nil {
		return Result{}, fmt.Errorf("create history archive directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".history-*.partial")
	if err != nil {
		return Result{}, fmt.Errorf("create history archive temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		_ = temporary.Close()
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(repository.fileMode); err != nil {
		return Result{}, fmt.Errorf("secure history archive file: %w", err)
	}
	digest := sha256.New()
	if err := encode(records, io.MultiWriter(temporary, digest)); err != nil {
		return Result{}, err
	}
	if err := temporary.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync history archive file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Result{}, fmt.Errorf("close history archive file: %w", err)
	}
	checksum := hex.EncodeToString(digest.Sum(nil))
	filename := fmt.Sprintf("history-%d-%d-%s.jsonl.gz", records[0].HistoryID, records[len(records)-1].HistoryID, checksum)
	finalPath := filepath.Join(directory, filename)
	if err := installArchive(temporaryPath, finalPath, digest); err != nil {
		return Result{}, err
	}
	keepTemporary = false
	if err := syncArchiveDirectories(directory, repository.syncDirectory); err != nil {
		return Result{}, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return Result{}, fmt.Errorf("stat history archive file: %w", err)
	}
	return Result{StoragePath: filepath.ToSlash(filepath.Join(relativeDir, filename)), RecordCount: len(records), ByteSize: info.Size(), SHA256: checksum}, nil
}

func encode(records []Record, destination io.Writer) error {
	compressed := gzip.NewWriter(destination)
	compressed.Header.ModTime = time.Unix(0, 0).UTC()
	compressed.Header.OS = 255
	encoder := json.NewEncoder(compressed)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			_ = compressed.Close()
			return fmt.Errorf("encode history archive record: %w", err)
		}
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("close history archive gzip: %w", err)
	}
	return nil
}

func installArchive(temporaryPath, finalPath string, expected hash.Hash) error {
	// Both files are in the same directory. Link installs the fully synced file
	// atomically without replacement; Rename silently overwrites on Unix.
	if err := os.Link(temporaryPath, finalPath); err == nil {
		return os.Remove(temporaryPath)
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("install history archive file: %w", err)
	}
	info, err := os.Lstat(finalPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("existing history archive is not a regular file")
	}
	file, err := os.Open(finalPath)
	if err != nil {
		return fmt.Errorf("open existing history archive file: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("verify existing history archive file: %w", err)
	}
	if !equalDigest(digest, expected) {
		return errors.New("existing history archive checksum mismatch")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync existing history archive file: %w", err)
	}
	return os.Remove(temporaryPath)
}

// Sync the installed link/removal and every ancestor entry, including any
// directory newly created by MkdirAll. Do this on retries too: existence does
// not prove a previous attempt durably synced that directory. This visits only
// the finite ancestor chain, not other filesystem entries.
func syncArchiveDirectories(directory string, syncDirectory func(string) error) error {
	if syncDirectory == nil {
		syncDirectory = syncArchiveDirectory
	}
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		if err := syncDirectory(current); err != nil {
			return fmt.Errorf("sync history archive directory: %w", err)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func syncArchiveDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func equalDigest(left, right hash.Hash) bool {
	return string(left.Sum(nil)) == string(right.Sum(nil))
}
