package historyarchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
