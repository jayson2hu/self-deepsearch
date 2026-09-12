package historyarchive

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxArchiveBytes = 64 << 20

var archiveUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var archiveChecksum = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Manifest is the explicit subset exported from archived_history_manifests.
// It contains private user identifiers and must be handled like the archive.
type Manifest struct {
	StoragePath   string    `json:"storage_path"`
	SchemaVersion int       `json:"schema_version"`
	UserScope     UserScope `json:"user_scope"`
	RangeStart    time.Time `json:"range_start"`
	RangeEnd      time.Time `json:"range_end"`
	RecordCount   int       `json:"record_count"`
	ByteSize      int64     `json:"byte_size"`
	SHA256        string    `json:"sha256"`
}

type UserScope struct {
	Kind      string   `json:"kind"`
	UserCount int      `json:"user_count"`
	UserIDs   []string `json:"user_ids"`
}

// VerifyArchive verifies a bounded gzip JSONL stream against its trusted
// database manifest. Returned records are private, still-deleted history;
// verification never reconnects to a database or makes history visible again.
func VerifyArchive(source io.Reader, manifest Manifest) ([]Record, error) {
	if manifest.SchemaVersion != 1 || manifest.RecordCount < 1 || manifest.RecordCount > 10000 ||
		manifest.ByteSize < 1 || manifest.ByteSize > maxArchiveBytes || !archiveChecksum.MatchString(manifest.SHA256) ||
		manifest.RangeStart.IsZero() || manifest.RangeEnd.Before(manifest.RangeStart) ||
		manifest.UserScope.Kind != "explicit" || manifest.UserScope.UserCount < 1 ||
		manifest.UserScope.UserCount != len(manifest.UserScope.UserIDs) || manifest.UserScope.UserCount > manifest.RecordCount {
		return nil, errors.New("invalid or unsupported history archive manifest")
	}
	for index, userID := range manifest.UserScope.UserIDs {
		if !archiveUUID.MatchString(userID) || (index > 0 && userID <= manifest.UserScope.UserIDs[index-1]) {
			return nil, errors.New("invalid history archive user scope")
		}
	}
	data, err := io.ReadAll(io.LimitReader(source, manifest.ByteSize+1))
	if err != nil || int64(len(data)) != manifest.ByteSize {
		return nil, errors.New("history archive byte size mismatch")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != manifest.SHA256 {
		return nil, errors.New("history archive checksum mismatch")
	}
	compressed := bytes.NewReader(data)
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, errors.New("invalid history archive gzip")
	}
	defer reader.Close()
	reader.Multistream(false)
	limited := &io.LimitedReader{R: reader, N: maxArchiveBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	records := make([]Record, 0, manifest.RecordCount)
	historyIDs := make(map[int64]bool, manifest.RecordCount)
	users := make(map[string]bool)
	for scanner.Scan() {
		if len(records) >= manifest.RecordCount {
			return nil, errors.New("history archive record count mismatch")
		}
		var record Record
		if err := decodeStrict(scanner.Bytes(), &record); err != nil {
			return nil, errors.New("invalid history archive JSONL record")
		}
		if record.HistoryID < 1 || historyIDs[record.HistoryID] || !archiveUUID.MatchString(record.UserID) ||
			!archiveUUID.MatchString(record.ContentID) || record.ViewedAt.IsZero() || record.DeletedAt.IsZero() ||
			(record.ContentType != "work" && record.ContentType != "performer" && record.ContentType != "studio") {
			return nil, errors.New("invalid history archive record fields")
		}
		if len(records) > 0 {
			previous := records[len(records)-1]
			if record.ViewedAt.Before(previous.ViewedAt) || (record.ViewedAt.Equal(previous.ViewedAt) && record.HistoryID <= previous.HistoryID) {
				return nil, errors.New("history archive records are not in canonical order")
			}
		}
		historyIDs[record.HistoryID], users[record.UserID] = true, true
		records = append(records, record)
	}
	if scanner.Err() != nil || limited.N == 0 || compressed.Len() != 0 {
		return nil, errors.New("invalid, oversized or trailing history archive data")
	}
	if len(records) != manifest.RecordCount || !records[0].ViewedAt.Equal(manifest.RangeStart) ||
		!records[len(records)-1].ViewedAt.Equal(manifest.RangeEnd) {
		return nil, errors.New("history archive count or time range mismatch")
	}
	userIDs := make([]string, 0, len(users))
	for userID := range users {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)
	if strings.Join(userIDs, ",") != strings.Join(manifest.UserScope.UserIDs, ",") {
		return nil, errors.New("history archive user scope mismatch")
	}
	return records, nil
}

// VerifyFile accepts only a relative manifest path inside an explicitly chosen
// archive directory. Nested symlinks are rejected. Run on an offline copy whose
// directories cannot be changed concurrently by an untrusted writer.
func VerifyFile(baseDir string, manifest Manifest) ([]Record, error) {
	if baseDir == "" || !filepath.IsLocal(manifest.StoragePath) ||
		filepath.ToSlash(filepath.Clean(manifest.StoragePath)) != manifest.StoragePath {
		return nil, errors.New("history archive requires a canonical relative storage path")
	}
	base, err := filepath.Abs(baseDir)
	if err != nil || base == filepath.VolumeName(base)+string(os.PathSeparator) {
		return nil, errors.New("invalid history archive base directory")
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil || base == filepath.VolumeName(base)+string(os.PathSeparator) {
		return nil, errors.New("history archive base directory unavailable")
	}
	current := base
	parts := strings.Split(filepath.FromSlash(manifest.StoragePath), string(os.PathSeparator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 ||
			(index < len(parts)-1 && !info.IsDir()) || (index == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, errors.New("history archive path is unavailable or not a regular file")
		}
	}
	file, err := os.Open(current)
	if err != nil {
		return nil, errors.New("history archive file unavailable")
	}
	defer file.Close()
	return VerifyArchive(file, manifest)
}

func decodeStrict(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}
