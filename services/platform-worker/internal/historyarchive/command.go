package historyarchive

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// RunVerifyCommand is an offline, read-only recovery check. Its output omits
// records, user identifiers, local paths and database credentials.
func RunVerifyCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("history-archive-verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("archive-dir", "", "offline archive base directory")
	manifestPath := flags.String("manifest", "", "private exported manifest JSON file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *directory == "" || *manifestPath == "" {
		fmt.Fprintln(stderr, "usage: platform-worker history-archive-verify --archive-dir <offline-copy> --manifest <private-manifest.json>")
		return 2
	}
	file, err := os.Open(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "history_archive_manifest_unavailable")
		return 1
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	_ = file.Close()
	var manifest Manifest
	if err != nil || len(data) > 1<<20 || decodeStrict(data, &manifest) != nil {
		fmt.Fprintln(stderr, "history_archive_manifest_invalid")
		return 1
	}
	records, err := VerifyFile(*directory, manifest)
	if err != nil {
		fmt.Fprintln(stderr, "history_archive_verification_failed")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(map[string]any{
		"status": "verified", "schema_version": manifest.SchemaVersion,
		"record_count": len(records), "byte_size": manifest.ByteSize, "sha256": manifest.SHA256,
		"database_modified": false, "history_made_visible": false,
	}); err != nil {
		fmt.Fprintln(stderr, "history_archive_result_write_failed")
		return 1
	}
	return 0
}
