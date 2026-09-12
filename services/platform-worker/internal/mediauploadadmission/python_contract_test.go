package mediauploadadmission

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"self-deepsearch/services/platform-worker/internal/mediausage"
)

func TestRealPythonSDKAdmissionAndDurableAutomaticPause(t *testing.T) {
	python := os.Getenv("MEDIA_CONTRACT_PYTHON")
	if python == "" {
		t.Skip("set MEDIA_CONTRACT_PYTHON for the loopback Go/Python admission contract")
	}
	var checks atomic.Int64
	var reviewed atomic.Bool
	now := time.Now().UTC().Truncate(time.Second)
	schedule := mediausage.Schedule{AccountID: strings.Repeat("a", 32), PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), Every: 15 * time.Minute, Policy: mediausage.DefaultPolicy()}
	reader := stateFunc(func(context.Context) (mediausage.State, error) {
		at := time.Now().UTC().Truncate(time.Second)
		state := mediausage.State{LastSuccessAt: at, LastUntil: at.Add(-time.Second), UpdatedAt: at, LastAssessment: mediausage.Assessment{Status: "low_estimate", Recommendation: "observe_only"}}
		if checks.Add(1) > 3 && !reviewed.Load() {
			state.HighWater.ClassA = 950000
		}
		return state, nil
	})
	guard, err := mediausage.NewTaskGuard(reader, schedule, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(secret, guard, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(Path, h)
	mux.HandleFunc("POST /__test/reviewed", func(w http.ResponseWriter, _ *http.Request) { reviewed.Store(true); w.WriteHeader(204) })
	server := httptest.NewServer(mux)
	defer server.Close()
	fixture, err := filepath.Abs("../../../../workers/media-python/tests/admission_http_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, fixture, server.URL, t.TempDir())
	for _, name := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env, "MEDIA_ADMISSION_CONTRACT_CONFIRM=temporary-files-only", "MEDIA_UPLOAD_ADMISSION_SECRET="+secret,
		"MEDIA_HMAC_SECRET="+strings.Repeat("m", 40), "MEDIA_UPLOAD_CONTROL_SECRET="+strings.Repeat("u", 40))
	body, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated admission fixture failed: %v\n%s", err, body)
	}
	var result struct {
		Paused    int  `json:"paused_generation"`
		Resumed   int  `json:"resumed_generation"`
		Puts      int  `json:"puts"`
		Deletes   int  `json:"deletes"`
		Preserved bool `json:"pending_preserved"`
	}
	if json.Unmarshal(body, &result) != nil || result.Paused != 3 || result.Resumed != 4 || result.Puts != 1 || result.Deletes != 1 || !result.Preserved || checks.Load() != 6 {
		t.Fatalf("invalid actual HTTP/storage evidence: %s; checks=%d", body, checks.Load())
	}
}
