package outbox

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This exercises the actual Go sender, HMAC, Python HTTP handler, and deletion
// service. Only S3/Cloudflare are replaced by isolated temporary-file fixtures.
func TestMediaDeletionPythonHTTPContractRetryAndRecovery(t *testing.T) {
	python := os.Getenv("MEDIA_CONTRACT_PYTHON")
	if python == "" {
		t.Skip("set MEDIA_CONTRACT_PYTHON to execute the cross-language HTTP contract")
	}
	root := t.TempDir()
	key := "media-public/contract/image.webp"
	for _, directory := range []string{"objects", "backups"} {
		target := filepath.Join(root, directory, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("synthetic-image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	failureMarker := filepath.Join(root, "purge-fails")
	if err := os.WriteFile(failureMarker, []byte("fail until removed"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs("../../../../workers/media-python/tests/http_delete_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("test-only-media-contract-", 2)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, fixture, "--root", root)
	// No provider/database credentials or proxy settings reach the fixture.
	for _, name := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env, "MEDIA_HMAC_SECRET="+secret, "MEDIA_DELETE_CONTRACT_CONFIRM=temporary-files-only")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})
	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 1024), 1024)
	if !reader.Scan() {
		t.Fatal("Python media fixture did not announce a listening port")
	}
	var started struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(reader.Bytes(), &started); err != nil || started.Port < 1 || started.Port > 65535 {
		t.Fatal("Python media fixture returned an invalid port")
	}
	processor, err := NewMediaDeletionProcessor(
		"http://127.0.0.1:"+strconv.Itoa(started.Port)+"/v1/delete", secret,
		&http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(mediaDeletePayload{
		StorageScope: "public", StorageKey: key, BackupPath: key,
		PublicURL: stringPointer("https://media.example.test/" + key),
	})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{ID: "11111111-1111-4111-8111-111111111111", Type: "media_delete", Payload: payload}
	repository := &fakeRepository{events: []Event{event}}
	dispatcher := Dispatcher{
		Repository: repository, WorkerID: "python-contract", Processor: processor,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := dispatcher.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repository.completed) != 0 || len(repository.failed) != 1 || repository.failureCodes[0] != "media_delete_unavailable" {
		t.Fatalf("failed purge must remain retryable: completed=%v failed=%v codes=%v", repository.completed, repository.failed, repository.failureCodes)
	}
	assertFileExists(t, filepath.Join(root, "backups", filepath.FromSlash(key)))
	assertFileAbsent(t, filepath.Join(root, "objects", filepath.FromSlash(key)))
	assertFileAbsent(t, filepath.Join(root, "purge-completed"))

	if err := os.Remove(failureMarker); err != nil {
		t.Fatal(err)
	}
	repository.events = []Event{event}
	if err := dispatcher.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repository.completed) != 1 || repository.completed[0] != event.ID || len(repository.failed) != 1 {
		t.Fatalf("recovery must complete the same event only after matching acknowledgement: completed=%v failed=%v", repository.completed, repository.failed)
	}
	assertFileAbsent(t, filepath.Join(root, "backups", filepath.FromSlash(key)))
	assertFileAbsent(t, filepath.Join(root, "objects", filepath.FromSlash(key)))
	assertFileExists(t, filepath.Join(root, "purge-completed"))
}

func stringPointer(value string) *string { return &value }

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected fixture file to remain: %v", err)
	}
}

func assertFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected fixture file to be absent, got %v", err)
	}
}
