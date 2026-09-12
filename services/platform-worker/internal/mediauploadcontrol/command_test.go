package mediauploadcontrol

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type stubClient struct {
	calls   int
	command Command
	err     error
}

func (s *stubClient) Status(context.Context) (Receipt, error) {
	s.calls++
	return Receipt{Status: "observed", Mode: "paused"}, s.err
}
func (s *stubClient) Apply(_ context.Context, c Command) (Receipt, error) {
	s.calls++
	s.command = c
	return ackFor(c), s.err
}

func TestCommandOptInRegionValidationAndUncertainOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		region string
		want   int
		calls  int
	}{
		{"help", []string{"--help"}, "", 0, 0},
		{"offline", nil, "japan", 2, 0},
		{"wrong_region", []string{"--live"}, "beijing", 2, 0},
		{"extra_arg", []string{"--live", "unexpected"}, "japan", 2, 0},
		{"secret_argument", []string{"--live", "--secret=never-print-me"}, "japan", 2, 0},
		{"missing_snapshot", []string{"--live", "--action=pause"}, "japan", 2, 0},
		{"status", []string{"--live"}, "japan", 0, 1},
		{"status_mutation_flags", []string{"--live", "--reason=accidental"}, "japan", 2, 0},
		{"pause", []string{"--live", "--action=pause", "--command-id=" + testID, "--expected-generation=0", "--reason=test-only reviewed"}, "japan", 0, 1},
		{"unconfirmed_resume", []string{"--live", "--action=resume", "--command-id=" + testID, "--expected-generation=1", "--expected-epoch=" + testID, "--reason=test-only reviewed"}, "japan", 2, 0},
		{"confirmed_resume", []string{"--live", "--action=resume", "--command-id=" + testID, "--expected-generation=1", "--expected-epoch=" + testID, "--reason=test-only reviewed", "--confirm-resume"}, "japan", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			client := &stubClient{}
			getenv := func(key string) string {
				if key == "WORKER_REGION" {
					return tc.region
				}
				return ""
			}
			code := runCommand(context.Background(), tc.args, &out, &stderr, getenv, client)
			if code != tc.want || client.calls != tc.calls {
				t.Fatalf("exit=%d calls=%d", code, client.calls)
			}
			if strings.Contains(stderr.String()+out.String(), "never-print-me") {
				t.Fatal("argument echoed secret")
			}
		})
	}
	var out, stderr bytes.Buffer
	client := &stubClient{err: ErrUncertain}
	code := runCommand(context.Background(), []string{"--live"}, &out, &stderr, func(string) string { return "japan" }, client)
	if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "not_confirmed") {
		t.Fatal("uncertain operation reported success")
	}
}
