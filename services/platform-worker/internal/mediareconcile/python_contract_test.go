package mediareconcile

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"self-deepsearch/services/platform-worker/internal/mediausage"
	"self-deepsearch/services/platform-worker/internal/outbox"
)

// Real Go -> Python HTTP/signature/report/deletion logic; all remote storage
// and purge operations are replaced by files inside t.TempDir, never providers.
func TestReconciliationPythonHTTPRetainedMasterAndRecovery(t *testing.T) {
	python := os.Getenv("MEDIA_CONTRACT_PYTHON")
	if python == "" {
		t.Skip("set MEDIA_CONTRACT_PYTHON for the real cross-language HTTP contract")
	}
	root := t.TempDir()
	body := []byte("synthetic-image")
	digest := sha256.Sum256(body)
	current := Object{StorageScope: "public", StorageKey: "media-public/contract/current.webp", SHA256: hex.EncodeToString(digest[:]), ByteSize: int64(len(body))}
	retained := Object{StorageScope: "private", StorageKey: "media-master/contract/retained.webp", SHA256: current.SHA256, ByteSize: current.ByteSize}
	current.BackupPath, retained.BackupPath = current.StorageKey, retained.StorageKey
	writeObject := func(key string, body []byte) {
		for _, directory := range []string{"objects", "backups"} {
			path := filepath.Join(root, directory, filepath.FromSlash(key))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeObject(current.StorageKey, body)
	writeObject(retained.StorageKey, body)
	fixture, err := filepath.Abs("../../../../workers/media-python/tests/http_delete_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("test-only-reconcile-contract-", 2)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, fixture, "--root", root, "--enable-reconciliation")
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
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024), 1024)
	if !scanner.Scan() {
		t.Fatal("Python fixture did not announce its loopback port")
	}
	var started struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(scanner.Bytes(), &started) != nil || started.Port < 1 || started.Port > 65535 {
		t.Fatal("invalid fixture port")
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(started.Port)
	transport := &countingReconcileTransport{base: &http.Transport{Proxy: nil}}
	httpClient := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	client, err := NewHTTPClient(endpoint+"/v1/reconcile", secret, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "10000000-0000-4000-8000-000000000001", Objects: []Object{current, retained}}
	check := func() Report {
		t.Helper()
		report, err := client.Reconcile(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	if report := check(); report.IssueCount() != 0 || report.ExpectedCount != 2 {
		t.Fatal("retained master was not included as an expected object")
	}
	// Test the actual gate -> Runner -> Go HTTP -> Python path. The usage
	// reader is synthetic; no SQL/Analytics account is contacted here.
	gateNow := time.Now().UTC().Truncate(time.Second)
	schedule := mediausage.Schedule{AccountID: strings.Repeat("a", 32), PeriodStart: gateNow.Add(-time.Hour), PeriodEnd: gateNow.Add(time.Hour), Every: 15 * time.Minute, Policy: mediausage.DefaultPolicy()}
	usageState := mediausage.State{}
	admission, err := mediausage.NewTaskGuard(reconcileUsageReader(func(context.Context) (mediausage.State, error) { return usageState, nil }), schedule, func() time.Time { return gateNow })
	if err != nil {
		t.Fatal(err)
	}
	guardedRepo := &admissionRepo{fakeRepository: fakeRepository{run: run}}
	guardedRunner := Runner{Repository: guardedRepo, Client: client, Admission: admission, Every: 24 * time.Hour, Now: time.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	beforeRequests := transport.reconciliations.Load()
	guardedRunner.runOnce(ctx)
	if transport.reconciliations.Load() != beforeRequests || guardedRepo.prepares != 0 {
		t.Fatal("missing usage state still contacted Python or started inventory")
	}
	usageState = mediausage.State{LastSuccessAt: gateNow, LastUntil: gateNow.Add(-time.Minute), UpdatedAt: gateNow, HighWater: mediausage.Counts{ClassA: 100, ClassB: 200}, LastAssessment: mediausage.Assessment{Status: "low_estimate", Reason: "operations_below_warning", Recommendation: "observe_only"}}
	guardedRunner.runOnce(ctx)
	if transport.reconciliations.Load() != beforeRequests+1 || guardedRepo.completed == nil {
		t.Fatal("fresh state did not dispatch the real Python reconciliation")
	}
	usageState.HighWater.ClassA = 850000 // Stored low label must not bypass high water.
	guardedRunner.runOnce(ctx)
	if transport.reconciliations.Load() != beforeRequests+1 || guardedRepo.prepares != 1 {
		t.Fatal("high operation estimate dispatched another scan")
	}
	marker := filepath.Join(root, "reconcile-fails")
	if err := os.WriteFile(marker, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{run: run}
	runner := Runner{Repository: repository, Client: client, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	runner.runOnce(ctx)
	if repository.completed != nil || repository.failed != "reconcile_unavailable" {
		t.Fatal("failed inspection was marked clean")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"objects", "backups"} {
		if err := os.Remove(filepath.Join(root, directory, filepath.FromSlash(retained.StorageKey))); err != nil {
			t.Fatal(err)
		}
	}
	if report := check(); report.MissingS3Count != 1 || report.MissingBackupCount != 1 || report.IssueCount() != 2 {
		t.Fatal("missing retained master was silently ignored")
	}
	writeObject(retained.StorageKey, body)
	writeObject("media-public/contract/orphan.webp", []byte("orphan"))
	if report := check(); report.OrphanS3Count != 1 || report.OrphanBackupCount != 1 {
		t.Fatal("orphan objects were not detected")
	}
	for _, directory := range []string{"objects", "backups"} {
		if err := os.Remove(filepath.Join(root, directory, "media-public/contract/orphan.webp")); err != nil {
			t.Fatal(err)
		}
	}
	processor, err := outbox.NewMediaDeletionProcessor(endpoint+"/v1/delete", secret, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"storage_scope": retained.StorageScope, "storage_key": retained.StorageKey, "backup_path": retained.BackupPath, "public_url": nil})
	if err := processor.Process(ctx, outbox.Event{ID: "20000000-0000-4000-8000-000000000001", Type: "media_delete", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := admission.Allow(ctx); err == nil {
		t.Fatal("rights deletion must succeed while scan admission remains blocked")
	}
	// Simulate the post-ack database inventory (the real SQL is a separate
	// opt-in contract), not an assertion that this fixture exercised PostgreSQL.
	run.Objects = []Object{current}
	if report := check(); report.ExpectedCount != 1 || report.IssueCount() != 0 {
		t.Fatal("post-deletion inventory did not reconcile cleanly")
	}
}

type reconcileUsageReader func(context.Context) (mediausage.State, error)

func (f reconcileUsageReader) ReadMetricsState(ctx context.Context) (mediausage.State, error) {
	return f(ctx)
}

type countingReconcileTransport struct {
	base            http.RoundTripper
	reconciliations atomic.Int64
}

func (t *countingReconcileTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/reconcile" {
		t.reconciliations.Add(1)
	}
	return t.base.RoundTrip(r)
}
