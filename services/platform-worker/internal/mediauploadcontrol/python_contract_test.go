package mediauploadcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-worker/internal/outbox"
)

// This is real Go -> Python HTTP, signatures, durable files and Python SDK
// admission. The SDK and Cloudflare operations remain temporary-file/fake
// substitutes: no database, credentials or external providers are contacted.
func TestPythonHTTPUploadPauseResumeAndDeletion(t *testing.T) {
	python := os.Getenv("MEDIA_CONTRACT_PYTHON")
	if python == "" {
		t.Skip("set MEDIA_CONTRACT_PYTHON for the real cross-language HTTP contract")
	}
	root := t.TempDir()
	fixture, err := filepath.Abs("../../../../workers/media-python/tests/http_delete_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, fixture, "--root", root, "--enable-upload-control")
	for _, name := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	deletionSecret := strings.Repeat("test-only-deletion-secret-", 2)
	command.Env = append(command.Env, "MEDIA_HMAC_SECRET="+deletionSecret, "MEDIA_UPLOAD_CONTROL_SECRET="+testSecret, "MEDIA_DELETE_CONTRACT_CONFIRM=temporary-files-only")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024), 1024)
	if !scanner.Scan() {
		t.Fatal("Python fixture did not announce a loopback port")
	}
	var started struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(scanner.Bytes(), &started) != nil || started.Port < 1 || started.Port > 65535 {
		t.Fatal("invalid loopback port")
	}
	origin := "http://127.0.0.1:" + strconv.Itoa(started.Port)
	client, err := NewClient(origin, testSecret, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := client.Status(ctx)
	if err != nil || initial.Generation != 0 || initial.Mode != "paused" {
		t.Fatalf("unexpected initial state: %+v %v", initial, err)
	}
	first := testCommand()
	paused, err := client.Apply(ctx, first)
	if err != nil || paused.Generation != 1 {
		t.Fatalf("bootstrap failed: %v", err)
	}
	replay, err := client.Apply(ctx, first)
	if err != nil || replay.Status != "replayed" {
		t.Fatalf("retry failed: %v", err)
	}

	// Invoke the production S3 adapter in a fresh Python process each time.
	// The mock SDK must receive no PUT until a confirmed enable command.
	checkUpload := func(want int) {
		t.Helper()
		const source = `
import json, pathlib, sys
from unittest.mock import Mock
from media_worker.storage import S3ObjectStore
from media_worker.upload_control import UploadControlBlocked
sdk = Mock()
store = S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=sdk, upload_lock_file=pathlib.Path(sys.argv[1]))
try:
    with store.upload_guard():
        store.put("media-master/contract/upload.webp", b"abc", content_type="image/webp", public=False, sha256="a"*64)
except UploadControlBlocked:
    pass
print(json.dumps({"puts": sdk.put_object.call_count}))
`
		probe := exec.CommandContext(ctx, python, "-c", source, filepath.Join(root, "state", "upload.lock"))
		probe.Env = append([]string{}, command.Env...)
		probe.Env = append(probe.Env, "PYTHONPATH="+filepath.Dir(filepath.Dir(fixture)), "MEDIA_UPLOAD_CONTROL_MODE=enforce", "MEDIA_DELIVERY_MODE=normal")
		probe.Stderr = io.Discard
		body, err := probe.Output()
		if err != nil {
			t.Fatal("Python SDK admission probe failed")
		}
		var result struct {
			Puts int `json:"puts"`
		}
		if json.Unmarshal(body, &result) != nil || result.Puts != want {
			t.Fatalf("SDK PUT count mismatch: %s", body)
		}
	}
	checkUpload(0)
	resume := Command{ID: "20000000-0000-4000-8000-000000000002", ExpectedEpoch: paused.Epoch, ExpectedGeneration: paused.Generation, Mode: "enabled", ResumeConfirmed: true, Reason: "test-only independent review"}
	resumed, err := client.Apply(ctx, resume)
	if err != nil || resumed.Mode != "enabled" {
		t.Fatalf("resume failed: %v", err)
	}
	checkUpload(1)
	secondPause := Command{ID: "30000000-0000-4000-8000-000000000003", ExpectedEpoch: resumed.Epoch, ExpectedGeneration: resumed.Generation, Mode: "paused", Reason: "test-only quota stop"}
	if _, err := client.Apply(ctx, secondPause); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Apply(ctx, resume); !errors.Is(err, ErrConflict) {
		t.Fatal("stale resume reopened upload")
	}
	checkUpload(0)
	state, err := client.Status(ctx)
	if err != nil || state.Generation != 3 || state.Mode != "paused" {
		t.Fatal("status does not reflect applied pause")
	}
	ledger, err := os.ReadFile(filepath.Join(root, "state", "upload.lock.control.jsonl"))
	if err != nil || len(strings.Split(strings.TrimSpace(string(ledger)), "\n")) != 3 {
		t.Fatal("durable ledger missing or duplicate commands appended")
	}
	// Existing private rights deletion must still work while uploads are paused.
	key := "media-master/contract/delete.webp"
	for _, directory := range []string{"objects", "backups"} {
		path := filepath.Join(root, directory, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	processor, err := outbox.NewMediaDeletionProcessor(origin+"/v1/delete", deletionSecret, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"storage_scope": "private", "storage_key": key, "backup_path": key, "public_url": nil})
	if err := processor.Process(ctx, outbox.Event{ID: "40000000-0000-4000-8000-000000000004", Type: "media_delete", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"objects", "backups"} {
		if _, err := os.Stat(filepath.Join(root, directory, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatal("paused uploads blocked rights deletion")
		}
	}
	// Actual queue driver -> signed HTTP -> Python timed command, with only
	// the database replaced. Drop the first applied reply after Python has
	// durably written it, then recover through authenticated status alone.
	now := time.Now().UTC().Truncate(time.Second)
	queued := queuedAt(now)
	queued.ID = "50000000-0000-4000-8000-000000000005"
	queued.ExpectedEpoch, queued.ExpectedGeneration = state.Epoch, state.Generation
	queue := &memoryQueue{command: queued, now: &now, authorized: true}
	drop := &dropAppliedReply{base: &http.Transport{Proxy: nil}, drop: true}
	queuedClient, err := NewClient(origin, testSecret, true, &http.Client{Transport: drop, Timeout: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runner := QueueRunner{Repository: queue, Client: queuedClient, Now: func() time.Time { return now }}
	if err = runner.RunOnce(ctx); err != nil || queue.out.Status != "uncertain" {
		t.Fatalf("lost real queue receipt: %+v %v", queue.out, err)
	}
	if err = runner.RunOnce(ctx); err != nil || queue.out.Status != "applied" || drop.applies != 1 {
		t.Fatalf("queue did not recover real application without redispatch: %+v %v", queue.out, err)
	}
	checkUpload(0)
}

type dropAppliedReply struct {
	base    http.RoundTripper
	drop    bool
	applies int
}

func (d *dropAppliedReply) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := d.base.RoundTrip(r)
	if err != nil {
		return response, err
	}
	if r.URL.Path == "/v1/upload-control/apply" {
		d.applies++
		if d.drop {
			d.drop = false
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			return nil, errors.New("synthetic lost reply")
		}
	}
	return response, nil
}
