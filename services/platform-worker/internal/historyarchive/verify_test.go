package historyarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtureRecords() []Record {
	viewed := time.Date(2026, 7, 1, 1, 2, 3, 123456000, time.UTC)
	return []Record{
		{HistoryID: 42, UserID: "00000000-0000-4000-8000-000000000001", ContentType: "work", ContentID: "00000000-0000-4000-8000-000000000010", ViewedAt: viewed, DeletedAt: viewed.Add(time.Hour)},
		{HistoryID: 43, UserID: "00000000-0000-4000-8000-000000000002", ContentType: "performer", ContentID: "00000000-0000-4000-8000-000000000011", ViewedAt: viewed.Add(time.Microsecond), DeletedAt: viewed.Add(time.Hour)},
	}
}

func manifestFor(t *testing.T, records []Record) ([]byte, Manifest) {
	t.Helper()
	var buffer bytes.Buffer
	if err := encode(records, &buffer); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(buffer.Bytes())
	return buffer.Bytes(), Manifest{
		StoragePath: "history/2026/09/example.jsonl.gz", SchemaVersion: 1,
		UserScope:  UserScope{Kind: "explicit", UserCount: 2, UserIDs: []string{fixtureRecords()[0].UserID, fixtureRecords()[1].UserID}},
		RangeStart: records[0].ViewedAt, RangeEnd: records[len(records)-1].ViewedAt,
		RecordCount: len(records), ByteSize: int64(buffer.Len()), SHA256: hex.EncodeToString(digest[:]),
	}
}

func TestVerifyArchivePreservesExactPrivateRecords(t *testing.T) {
	want := fixtureRecords()
	data, manifest := manifestFor(t, want)
	got, err := VerifyArchive(bytes.NewReader(data), manifest)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("archive restore verification failed: %v", err)
	}
}

func TestVerifyArchiveRejectsInvalidMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"schema":           func(m *Manifest) { m.SchemaVersion = 2 },
		"checksum":         func(m *Manifest) { m.SHA256 = strings.Repeat("0", 64) },
		"byte_size":        func(m *Manifest) { m.ByteSize++ },
		"zero_records":     func(m *Manifest) { m.RecordCount = 0 },
		"missing_record":   func(m *Manifest) { m.RecordCount++ },
		"too_many_records": func(m *Manifest) { m.RecordCount = 10001 },
		"oversized_input":  func(m *Manifest) { m.ByteSize = maxArchiveBytes + 1 },
		"time_range":       func(m *Manifest) { m.RangeEnd = m.RangeEnd.Add(time.Microsecond) },
		"scope_kind":       func(m *Manifest) { m.UserScope.Kind = "all" },
		"scope_count":      func(m *Manifest) { m.UserScope.UserCount++ },
		"scope_user":       func(m *Manifest) { m.UserScope.UserIDs[1] = "00000000-0000-4000-8000-000000000099" },
		"scope_duplicate":  func(m *Manifest) { m.UserScope.UserIDs[1] = m.UserScope.UserIDs[0] },
	} {
		t.Run(name, func(t *testing.T) {
			data, manifest := manifestFor(t, fixtureRecords())
			mutate(&manifest)
			if _, err := VerifyArchive(bytes.NewReader(data), manifest); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestVerifyArchiveRejectsInvalidContent(t *testing.T) {
	for name, mutate := range map[string]func([]Record){
		"duplicate_id":       func(records []Record) { records[1].HistoryID = records[0].HistoryID },
		"missing_user":       func(records []Record) { records[0].UserID = "" },
		"missing_deleted_at": func(records []Record) { records[0].DeletedAt = time.Time{} },
		"content_type":       func(records []Record) { records[0].ContentType = "email" },
		"ordering":           func(records []Record) { records[0], records[1] = records[1], records[0] },
	} {
		t.Run(name, func(t *testing.T) {
			records := fixtureRecords()
			mutate(records)
			data, manifest := manifestFor(t, records)
			if _, err := VerifyArchive(bytes.NewReader(data), manifest); err == nil {
				t.Fatal("invalid archive records accepted")
			}
		})
	}
	for _, name := range []string{"corrupt_gzip", "truncated_gzip", "trailing_bytes", "second_gzip_member"} {
		t.Run(name, func(t *testing.T) {
			data, manifest := manifestFor(t, fixtureRecords())
			switch name {
			case "corrupt_gzip":
				data[len(data)-5] ^= 0xff
			case "truncated_gzip":
				data = data[:len(data)-1]
			case "trailing_bytes":
				data = append(data, 'x')
			case "second_gzip_member":
				data = append(data, data...)
			}
			// A matching outer checksum must not bypass gzip/record validation.
			digest := sha256.Sum256(data)
			manifest.ByteSize, manifest.SHA256 = int64(len(data)), hex.EncodeToString(digest[:])
			if _, err := VerifyArchive(bytes.NewReader(data), manifest); err == nil {
				t.Fatal("invalid compressed archive accepted")
			}
		})
	}
}

func TestVerifyFileRejectsEscapesAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	data, manifest := manifestFor(t, fixtureRecords())
	if err := os.WriteFile(filepath.Join(directory, "archive.gz"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "archive.gz"), filepath.Join(directory, "link.gz")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(directory, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../archive.gz", "/archive.gz", "./archive.gz", "link.gz", "linked-directory/archive.gz"} {
		manifest.StoragePath = path
		if _, err := VerifyFile(directory, manifest); err == nil {
			t.Fatal("archive path escape or symlink accepted")
		}
	}
	manifest.StoragePath = "archive.gz"
	if _, err := VerifyFile(directory, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCommandIsReadOnlyAndRedactsPrivateData(t *testing.T) {
	directory := t.TempDir()
	data, manifest := manifestFor(t, fixtureRecords())
	manifest.StoragePath = "archive.gz"
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archivePath, manifestPath := filepath.Join(directory, manifest.StoragePath), filepath.Join(directory, "private.json")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := RunVerifyCommand([]string{"--archive-dir", directory, "--manifest", manifestPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("verification failed: %s", stderr.String())
	}
	var result struct {
		Status             string `json:"status"`
		RecordCount        int    `json:"record_count"`
		DatabaseModified   bool   `json:"database_modified"`
		HistoryMadeVisible bool   `json:"history_made_visible"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != "verified" || result.RecordCount != 2 || result.DatabaseModified || result.HistoryMadeVisible {
		t.Fatal("unexpected verification summary")
	}
	for _, private := range []string{directory, fixtureRecords()[0].UserID, fixtureRecords()[0].ContentID} {
		if strings.Contains(stdout.String()+stderr.String(), private) {
			t.Fatal("private archive data leaked in command output")
		}
	}
	after, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("verification changed archive")
	}
	stdout.Reset()
	stderr.Reset()
	if err := os.WriteFile(archivePath, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := RunVerifyCommand([]string{"--archive-dir", directory, "--manifest", manifestPath}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != "history_archive_verification_failed\n" {
		t.Fatal("corrupt archive did not fail closed with redacted output")
	}
}
