package historyarchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	cutoff time.Time
	now    time.Time
	limit  int
	result Result
}

func TestWriteArchiveMatchesManifestMetadata(t *testing.T) {
	repository := PostgresRepository{baseDir: t.TempDir(), fileMode: 0o600, dirMode: 0o700}
	records := []Record{{
		HistoryID: 1, UserID: "user-1", ContentType: "work", ContentID: "work-1",
		ViewedAt: time.Date(2026, 7, 1, 1, 2, 3, 0, time.UTC), DeletedAt: time.Date(2026, 7, 2, 1, 2, 3, 0, time.UTC),
	}}
	result, err := repository.writeArchive(records, time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository.baseDir, filepath.FromSlash(result.StoragePath))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if result.RecordCount != 1 || result.ByteSize != int64(len(data)) || result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("archive metadata does not match file: %#v", result)
	}
	if !strings.Contains(filepath.Base(path), result.SHA256) {
		t.Fatal("archive filename must carry its full checksum")
	}
}

func (repository *fakeRepository) ArchiveEligible(_ context.Context, cutoff, now time.Time, limit int) (Result, error) {
	repository.cutoff, repository.now, repository.limit = cutoff, now, limit
	return repository.result, nil
}

func TestEncodeIsDeterministicAndPreservesExactTimes(t *testing.T) {
	record := Record{HistoryID: 42, UserID: "user-1", ContentType: "work", ContentID: "work-1",
		ViewedAt: time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC), DeletedAt: time.Date(2026, 8, 19, 4, 5, 6, 0, time.UTC)}
	var first, second bytes.Buffer
	if err := encode([]Record{record}, &first); err != nil {
		t.Fatal(err)
	}
	if err := encode([]Record{record}, &second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("archive encoding must be deterministic for stable checksums")
	}
	reader, err := gzip.NewReader(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Record
	if err := json.Unmarshal(bytes.TrimSpace(data), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.ViewedAt.Equal(record.ViewedAt) || !decoded.DeletedAt.Equal(record.DeletedAt) || decoded.UserID != record.UserID {
		t.Fatalf("archive record changed: %#v", decoded)
	}
}

func TestRunnerUsesAgeAndBatch(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}
	runner := Runner{Repository: repository, Every: time.Hour, MinimumAge: 30 * 24 * time.Hour, BatchSize: 500, Now: func() time.Time { return now }}
	runner.runOnce(context.Background())
	if !repository.cutoff.Equal(now.Add(-30*24*time.Hour)) || !repository.now.Equal(now) || repository.limit != 500 {
		t.Fatalf("unexpected archive request: cutoff=%s now=%s limit=%d", repository.cutoff, repository.now, repository.limit)
	}
}

func TestInstallArchiveNeverOverwritesExistingFile(t *testing.T) {
	directory := t.TempDir()
	temporary := filepath.Join(directory, "new.partial")
	final := filepath.Join(directory, "archive.jsonl.gz")
	if err := os.WriteFile(temporary, []byte("new archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("original evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("new archive"))
	if err := installArchive(temporary, final, digest); err == nil {
		t.Fatal("existing different archive must be rejected, not replaced")
	}
	data, err := os.ReadFile(final)
	if err != nil || string(data) != "original evidence" {
		t.Fatal("archive installation overwrote existing evidence")
	}
}

func TestInstallArchiveReusesMatchingFileAndRemovesTemporary(t *testing.T) {
	directory := t.TempDir()
	temporary := filepath.Join(directory, "new.partial")
	final := filepath.Join(directory, "archive.jsonl.gz")
	for _, path := range []string{temporary, final} {
		if err := os.WriteFile(path, []byte("same archive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.Stat(final)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("same archive"))
	if err := installArchive(temporary, final, digest); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(final)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("matching archive must retain the existing inode")
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary archive must be removed after successful reuse")
	}
}

func TestWriteArchiveSyncsInstalledDirectoryAndAllAncestorsOnEveryAttempt(t *testing.T) {
	repository := PostgresRepository{baseDir: filepath.Join(t.TempDir(), "new", "archive-root"), fileMode: 0o600, dirMode: 0o700}
	now := time.Date(2026, 9, 12, 1, 2, 3, 0, time.UTC)
	leaf := filepath.Join(repository.baseDir, "history", "2026", "09")
	var expected []string
	for current := leaf; ; current = filepath.Dir(current) {
		expected = append(expected, current)
		if filepath.Dir(current) == current {
			break
		}
	}
	for range 2 {
		var synced []string
		repository.syncDirectory = func(directory string) error {
			// Directory persistence must happen after the final link is installed
			// and the temporary link removed, before writeArchive reports success.
			files, err := filepath.Glob(filepath.Join(leaf, "*.jsonl.gz"))
			if err != nil || len(files) != 1 {
				t.Fatal("directory synced before final archive installation")
			}
			partials, err := filepath.Glob(filepath.Join(leaf, ".history-*.partial"))
			if err != nil || len(partials) != 0 {
				t.Fatal("directory synced before temporary archive removal")
			}
			synced = append(synced, directory)
			return syncArchiveDirectory(directory)
		}
		if _, err := repository.writeArchive(fixtureRecords(), now); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(synced, expected) {
			t.Fatal("archive directory or ancestor persistence was omitted, reordered or skipped on retry")
		}
	}
}

func TestWriteArchivePropagatesDirectorySyncFailure(t *testing.T) {
	now := time.Date(2026, 9, 12, 1, 2, 3, 0, time.UTC)
	for _, failureLevel := range []string{"leaf", "parent", "base", "ancestor"} {
		t.Run(failureLevel, func(t *testing.T) {
			repository := PostgresRepository{baseDir: filepath.Join(t.TempDir(), "new-root"), fileMode: 0o600, dirMode: 0o700}
			leaf := filepath.Join(repository.baseDir, "history", "2026", "09")
			target := map[string]string{"leaf": leaf, "parent": filepath.Dir(leaf), "base": repository.baseDir, "ancestor": filepath.Dir(repository.baseDir)}[failureLevel]
			injected := errors.New("synthetic directory persistence failure")
			repository.syncDirectory = func(directory string) error {
				if directory == target {
					return injected
				}
				return syncArchiveDirectory(directory)
			}
			result, err := repository.writeArchive(fixtureRecords(), now)
			if !errors.Is(err, injected) || result != (Result{}) {
				t.Fatal("directory persistence failure was not propagated")
			}
		})
	}
}
