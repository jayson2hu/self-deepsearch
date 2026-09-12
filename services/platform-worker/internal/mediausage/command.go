package mediausage

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

type report struct {
	Version                   int                 `json:"version"`
	Source                    string              `json:"source"`
	StorageSource             string              `json:"storage_source"`
	Scope                     string              `json:"scope"`
	Window                    Window              `json:"requested_window"`
	Policy                    Policy              `json:"policy"`
	StoragePolicy             StoragePolicy       `json:"storage_policy"`
	Observation               *Observation        `json:"observation"`
	StorageObservation        *StorageObservation `json:"storage_observation"`
	Assessment                Assessment          `json:"assessment"`
	StorageAssessment         StorageAssessment   `json:"storage_assessment"`
	BillingVerified           bool                `json:"billing_verified"`
	ProviderWatermarkVerified bool                `json:"provider_watermark_verified"`
	StorageIncluded           bool                `json:"storage_included"`
	Enforcement               string              `json:"enforcement"`
	AutomaticRecovery         bool                `json:"automatic_recovery"`
}

type collector func(context.Context, string, string, Window) (*Observation, error)
type storageCollector func(context.Context, string, string, Window) (*StorageObservation, error)

// RunCommand is an explicitly opted-in, one-shot, read-only command. It runs
// before worker config/DB initialization and is never scheduled by the daemon.
func RunCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runCommand(ctx, args, stdout, stderr, os.Getenv, time.Now,
		func(ctx context.Context, account, token string, w Window) (*Observation, error) {
			client, err := NewClient(account, token)
			if err != nil {
				return nil, err
			}
			return client.Fetch(ctx, w)
		},
		func(ctx context.Context, account, token string, w Window) (*StorageObservation, error) {
			client, err := NewClient(account, token)
			if err != nil {
				return nil, err
			}
			return client.FetchStorage(ctx, w)
		})
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, now func() time.Time, collect collector, collectStorage storageCollector) int {
	p := DefaultPolicy()
	flags := flag.NewFlagSet("media-usage", flag.ContinueOnError)
	// flag's default error output can echo attacker-supplied values/secrets.
	flags.SetOutput(io.Discard)
	live := flags.Bool("live", false, "opt in to one read-only Cloudflare analytics query run")
	startText := flags.String("period-start", "", "confirmed current billing period start (RFC3339, whole seconds)")
	endText := flags.String("until", "", "inclusive query end (RFC3339, defaults to current UTC second)")
	standardOnlyConfirmed := flags.Bool("standard-only-confirmed", false, "operator attests all account buckets use Standard storage; not provider verification")
	flags.Uint64Var(&p.ClassALimit, "class-a-limit", p.ClassALimit, "account Class A operations budget")
	flags.Uint64Var(&p.ClassBLimit, "class-b-limit", p.ClassBLimit, "account Class B operations budget")
	flags.Uint64Var(&p.ClassAReserve, "class-a-reserve", p.ClassAReserve, "Class A safety reserve")
	flags.Uint64Var(&p.ClassBReserve, "class-b-reserve", p.ClassBReserve, "Class B safety reserve")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, "Usage: platform-worker media-usage --live --period-start <RFC3339> [--until <RFC3339>]")
			flags.SetOutput(stdout)
			flags.PrintDefaults()
			fmt.Fprintln(stdout, "Credentials: R2_ANALYTICS_ACCOUNT_ID and R2_ANALYTICS_API_TOKEN (read-only), from environment only. Analytics are not billing; this command never changes delivery or storage. Storage free-tier comparison requires --standard-only-confirmed.")
			return 0
		}
		fmt.Fprintln(stderr, "media_usage_arguments_invalid; use --help")
		return 2
	}
	if !*live || flags.NArg() != 0 || !p.valid() {
		fmt.Fprintln(stderr, "media_usage_requires_live_opt_in_and_valid_policy; use --help")
		return 2
	}
	start, err := time.Parse(time.RFC3339, *startText)
	until := now().UTC().Truncate(time.Second)
	if *endText != "" {
		var endErr error
		until, endErr = time.Parse(time.RFC3339, *endText)
		if endErr != nil {
			err = endErr
		}
	}
	w := Window{Start: start.UTC(), EndInclusive: until.UTC()}
	if err != nil || !w.valid(now()) {
		fmt.Fprintln(stderr, "media_usage_window_invalid; use a confirmed period within the past 31 days")
		return 2
	}
	account, token := getenv("R2_ANALYTICS_ACCOUNT_ID"), getenv("R2_ANALYTICS_API_TOKEN")
	if !accountPattern.MatchString(account) || !tokenPattern.MatchString(token) {
		fmt.Fprintln(stderr, "media_usage_credentials_invalid; set R2_ANALYTICS_ACCOUNT_ID and R2_ANALYTICS_API_TOKEN")
		return 2
	}
	o, fetchError := collect(ctx, account, token, w)
	a := Assess(o, fetchError, w.Start, now().UTC(), p)
	if fetchError != nil || a.Status == "unknown" {
		o = nil
	}
	storagePolicy := DefaultStoragePolicy(*standardOnlyConfirmed)
	storageObservation, storageError := collectStorage(ctx, account, token, w)
	storageAssessment := AssessStorage(storageObservation, storageError, w.Start, now().UTC(), storagePolicy)
	if storageError != nil || storageAssessment.Status == "unknown" {
		storageObservation = nil
	}
	r := report{Version: 2, Source: "cloudflare_graphql_r2_operations", StorageSource: "cloudflare_graphql_r2_storage",
		Scope: "account", Window: w, Policy: p, StoragePolicy: storagePolicy, Observation: o,
		StorageObservation: storageObservation, Assessment: a, StorageAssessment: storageAssessment,
		StorageIncluded: storageObservation != nil, Enforcement: "not_connected"}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		fmt.Fprintln(stderr, "media_usage_output_failed")
		return 1
	}
	if a.Status == "unknown" || a.Status == "stop_recommended" || storageAssessment.Status == "unknown" || storageAssessment.Status == "stop_recommended" {
		return 1
	}
	return 0
}
