package mediausage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func testEnvironment(key string) string {
	switch key {
	case "R2_ANALYTICS_ACCOUNT_ID":
		return testAccount
	case "R2_ANALYTICS_API_TOKEN":
		return testToken
	}
	return ""
}

func TestCommandOptInValidationAndNoSecretOutput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--period-start", testNow.Add(-time.Hour).Format(time.RFC3339)},
		{"--live"}, {"--live", "--period-start", testToken}, {"--token", testToken},
		{"--live", "--period-start", testNow.Add(-time.Hour).Format(time.RFC3339), "--until", testToken},
		{"--live", "--class-a-limit", testToken}, {"--live", "unexpected-secret-argument"},
		{"--live", "--class-a-reserve", "0"},
	} {
		var out, errOut bytes.Buffer
		code := runCommand(context.Background(), args, &out, &errOut, testEnvironment, func() time.Time { return testNow },
			func(context.Context, string, string, Window) (*Observation, error) {
				t.Fatal("unexpected live call")
				return nil, nil
			}, func(context.Context, string, string, Window) (*StorageObservation, error) {
				t.Fatal("unexpected live storage call")
				return nil, nil
			})
		if code != 2 || out.Len() != 0 || strings.Contains(errOut.String(), testToken) || strings.Contains(errOut.String(), "unexpected-secret-argument") {
			t.Fatalf("unsafe validation: code=%d out=%s err=%s", code, &out, &errOut)
		}
	}
	var out, errOut bytes.Buffer
	if code := RunCommand(context.Background(), []string{"--help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "Analytics are not billing") || errOut.Len() != 0 {
		t.Fatal("help should be offline")
	}
}

func TestCommandReportsNeverClaimBillingOrEnforcement(t *testing.T) {
	args := []string{"--live", "--period-start", testNow.Add(-time.Hour).Format(time.RFC3339)}
	for _, scenario := range []string{"low", "warning", "high", "stop", "failure", "stale", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			var out, errOut bytes.Buffer
			calls := 0
			code := runCommand(context.Background(), args, &out, &errOut, testEnvironment, func() time.Time { return testNow },
				func(_ context.Context, account, token string, w Window) (*Observation, error) {
					calls++
					if account != testAccount || token != testToken || w != shortWindow() {
						t.Error("wrong collector arguments")
					}
					o := observation()
					switch scenario {
					case "warning":
						o.Counts.ClassA = 700000
					case "high":
						o.Counts.ClassB = 8500000
					case "stop":
						o.Counts.ClassA = 950000
					case "failure":
						return o, errors.New(testToken)
					case "stale":
						o.Window.EndInclusive = testNow.Add(-time.Hour)
					case "missing":
						return nil, nil
					}
					return o, nil
				}, func(_ context.Context, account, token string, w Window) (*StorageObservation, error) {
					if account != testAccount || token != testToken || w != shortWindow() {
						t.Error("wrong storage collector arguments")
					}
					return storageObservation(), nil
				})
			var r report
			if json.Unmarshal(out.Bytes(), &r) != nil || calls != 1 || r.Version != 2 || r.Source != "cloudflare_graphql_r2_operations" ||
				r.StorageSource != "cloudflare_graphql_r2_storage" || r.Scope != "account" || r.StorageObservation == nil ||
				r.StorageAssessment.Status != "estimate_only" || r.StoragePolicy.StandardOnlyOperatorConfirmed ||
				r.BillingVerified || r.ProviderWatermarkVerified || !r.StorageIncluded || r.AutomaticRecovery || r.Enforcement != "not_connected" {
				t.Fatalf("bad report: %s", &out)
			}
			want := 0
			if scenario == "stop" || scenario == "failure" || scenario == "stale" || scenario == "missing" {
				want = 1
			}
			if code != want || strings.Contains(out.String()+errOut.String(), testToken) || strings.Contains(out.String(), testAccount) {
				t.Fatalf("bad exit/disclosure: %d %s %s", code, &out, &errOut)
			}
			if r.Assessment.Status == "unknown" && r.Observation != nil {
				t.Fatal("failure retained misleading partial counts")
			}
		})
	}
}

func TestCommandMissingCredentialsAndOutputFailure(t *testing.T) {
	args := []string{"--live", "--period-start", testNow.Add(-time.Hour).Format(time.RFC3339)}
	var errOut bytes.Buffer
	if code := runCommand(context.Background(), args, io.Discard, &errOut, func(string) string { return "" }, func() time.Time { return testNow },
		func(context.Context, string, string, Window) (*Observation, error) {
			t.Fatal("credentials missing but HTTP called")
			return nil, nil
		}, func(context.Context, string, string, Window) (*StorageObservation, error) {
			t.Fatal("credentials missing but storage HTTP called")
			return nil, nil
		}); code != 2 {
		t.Fatal(code)
	}
	if code := runCommand(context.Background(), args, brokenWriter{}, &errOut, testEnvironment, func() time.Time { return testNow },
		func(context.Context, string, string, Window) (*Observation, error) { return observation(), nil },
		func(context.Context, string, string, Window) (*StorageObservation, error) {
			return storageObservation(), nil
		}); code != 1 {
		t.Fatal(code)
	}
}

func TestCommandStorageFailureAndExplicitStandardOnlyAssessment(t *testing.T) {
	base := []string{"--live", "--period-start", testNow.Add(-time.Hour).Format(time.RFC3339)}
	for _, scenario := range []string{"storage_failure", "storage_stop", "storage_low"} {
		t.Run(scenario, func(t *testing.T) {
			args := append([]string{}, base...)
			if scenario != "storage_failure" {
				args = append(args, "--standard-only-confirmed")
			}
			var out, errOut bytes.Buffer
			code := runCommand(context.Background(), args, &out, &errOut, testEnvironment, func() time.Time { return testNow },
				func(context.Context, string, string, Window) (*Observation, error) { return observation(), nil },
				func(context.Context, string, string, Window) (*StorageObservation, error) {
					switch scenario {
					case "storage_failure":
						return storageObservation(), errors.New(testToken)
					case "storage_stop":
						return makeStorageObservation(285_000_000_000), nil
					default:
						return storageObservation(), nil
					}
				})
			var r report
			if json.Unmarshal(out.Bytes(), &r) != nil || r.BillingVerified || r.ProviderWatermarkVerified || r.AutomaticRecovery || r.Enforcement != "not_connected" ||
				strings.Contains(out.String()+errOut.String(), testToken) || strings.Contains(out.String(), testAccount) {
				t.Fatalf("unsafe storage report: %s %s", &out, &errOut)
			}
			switch scenario {
			case "storage_failure":
				if code != 1 || r.StorageIncluded || r.StorageObservation != nil || r.StorageAssessment.Status != "unknown" {
					t.Fatalf("storage failure was not fail-closed: %+v", r)
				}
			case "storage_stop":
				if code != 1 || !r.StorageIncluded || r.StorageAssessment.Status != "stop_recommended" || !r.StoragePolicy.StandardOnlyOperatorConfirmed {
					t.Fatalf("storage reserve was not surfaced: %+v", r)
				}
			case "storage_low":
				if code != 0 || !r.StorageIncluded || r.StorageAssessment.Status != "low_estimate" || !r.StoragePolicy.StandardOnlyOperatorConfirmed {
					t.Fatalf("confirmed low estimate was not reported: %+v", r)
				}
			}
		})
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
