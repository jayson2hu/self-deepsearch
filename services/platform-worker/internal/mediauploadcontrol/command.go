package mediauploadcontrol

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// RunCommand is an explicit one-shot operator tool, before database/config
// initialization. It is never invoked by observation/review/period rotation.
func RunCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runCommand(ctx, args, stdout, stderr, os.Getenv, nil)
}

type controlClient interface {
	Status(context.Context) (Receipt, error)
	Apply(context.Context, Command) (Receipt, error)
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string, client controlClient) int {
	flags := flag.NewFlagSet("media-upload-control", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	live := flags.Bool("live", false, "opt in to one real control request")
	action := flags.String("action", "status", "status, pause or resume")
	allowHTTP := flags.Bool("allow-http", false, "explicitly allow unencrypted HTTP with authenticated requests and responses")
	command := Command{}
	flags.StringVar(&command.ID, "command-id", "", "operator-generated UUID v4; reuse the exact request after an uncertain result")
	flags.StringVar(&command.ExpectedEpoch, "expected-epoch", "", "epoch from authenticated status; empty only for paused bootstrap")
	flags.Int64Var(&command.ExpectedGeneration, "expected-generation", -1, "generation from authenticated status; 0 only for paused bootstrap")
	flags.StringVar(&command.Reason, "reason", "", "2-1000 character private operational reason; never include secrets")
	flags.BoolVar(&command.ResumeConfirmed, "confirm-resume", false, "confirm usage, storage, pending uploads and in-flight writers were reviewed")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, "Usage: platform-worker media-upload-control --live --action status|pause|resume [options]")
			flags.SetOutput(stdout)
			flags.PrintDefaults()
			fmt.Fprintln(stdout, "Environment only: WORKER_REGION=japan, MEDIA_UPLOAD_CONTROL_URL (origin), MEDIA_UPLOAD_CONTROL_SECRET (separate secret). Does not change PostgreSQL, usage reviews, image delivery or pending upload evidence.")
			return 0
		}
		writeError(stderr, ErrInvalid)
		return 2
	}
	if !*live || flags.NArg() != 0 || getenv("WORKER_REGION") != "japan" {
		writeError(stderr, ErrInvalid)
		return 2
	}
	switch *action {
	case "status":
		if command.ID != "" || command.ExpectedEpoch != "" || command.ExpectedGeneration != -1 || command.Reason != "" || command.ResumeConfirmed {
			writeError(stderr, ErrInvalid)
			return 2
		}
	case "pause", "resume":
		command.Mode = "paused"
		if *action == "resume" {
			command.Mode = "enabled"
		}
		if !command.valid() {
			writeError(stderr, ErrInvalid)
			return 2
		}
	default:
		writeError(stderr, ErrInvalid)
		return 2
	}
	if client == nil {
		var err error
		client, err = NewClient(getenv("MEDIA_UPLOAD_CONTROL_URL"), getenv("MEDIA_UPLOAD_CONTROL_SECRET"), *allowHTTP, nil)
		if err != nil {
			writeError(stderr, ErrInvalid)
			return 2
		}
	}
	var receipt Receipt
	var err error
	if *action == "status" {
		receipt, err = client.Status(ctx)
	} else {
		receipt, err = client.Apply(ctx, command)
	}
	if err != nil {
		writeError(stderr, err)
		return 1
	}
	if json.NewEncoder(stdout).Encode(receipt) != nil {
		writeError(stderr, ErrUncertain)
		return 1
	}
	return 0
}
