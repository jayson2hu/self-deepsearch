package collectoringest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDecodeContract(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	good := Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abc", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "batch-1", Records: []Record{{ExternalID: "sample", CheckedAt: "2026-09-19T00:00:00Z", Payload: Payload{Name: "合成人物", Aliases: []string{}, ProfileURL: "https://example.test/models/sample/", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}}
	raw, _ := json.Marshal(good)
	if _, err := Decode(raw, now); err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"case variant":      strings.Replace(string(raw), `"name":`, `"Name":`, 1),
		"duplicate":         strings.Replace(string(raw), `"region":"japan"`, `"region":"japan","region":"japan"`, 1),
		"nested duplicate":  strings.Replace(string(raw), `"name":`, `"name":"shadow","name":`, 1),
		"escaped duplicate": strings.Replace(string(raw), `"name":`, `"n\u0061me":"shadow","name":`, 1),
		"unknown":           strings.Replace(string(raw), `"name":`, `"adult_status":"confirmed","name":`, 1),
		"unknown null":      strings.Replace(string(raw), `"name":`, `"publication_status":null,"name":`, 1),
		"trailing":          string(raw) + `{}`,
		"future":            strings.Replace(string(raw), "2026-09-19", "2027-09-19", 1),
		"timezone":          strings.Replace(string(raw), "00:00:00Z", "00:00:00", 1),
		"url token":         strings.Replace(string(raw), "/models/sample/", "/models/sample/?token=secret", 1),
		"url empty query":   strings.Replace(string(raw), "/models/sample/", "/models/sample/?", 1),
		"url fragment":      strings.Replace(string(raw), "/models/sample/", "/models/sample/#fragment", 1),
		"url credentials":   strings.Replace(string(raw), "https://example.test", "https://user:secret@example.test", 1),
		"url relative":      strings.Replace(string(raw), "https://example.test", "//example.test", 1),
		"url insecure":      strings.Replace(string(raw), "https://example.test", "http://example.test", 1),
		"url invalid port":  strings.Replace(string(raw), "https://example.test", "https://example.test:invalid", 1),
		"url control":       strings.Replace(string(raw), "/models/sample/", `/models/\u0000sample/`, 1),
		"empty records":     strings.Replace(string(raw), stringMustMarshal(good.Records), `[]`, 1),
		"null aliases":      strings.Replace(string(raw), `"aliases":[]`, `"aliases":null`, 1),
		"scalar aliases":    strings.Replace(string(raw), `"aliases":[]`, `"aliases":"name"`, 1),
		"null alias item":   strings.Replace(string(raw), `"aliases":[]`, `"aliases":[null]`, 1),
		"boolean alias":     strings.Replace(string(raw), `"aliases":[]`, `"aliases":[true]`, 1),
		"object aliases":    strings.Replace(string(raw), `"aliases":[]`, `"aliases":{}`, 1),
		"null name":         strings.Replace(string(raw), `"name":"合成人物"`, `"name":null`, 1),
		"number name":       strings.Replace(string(raw), `"name":"合成人物"`, `"name":42`, 1),
		"control name":      strings.Replace(string(raw), `"name":"合成人物"`, `"name":"合成\u0000人物"`, 1),
		"invalid utf8":      strings.Replace(string(raw), "合成人物", "invalid\xff", 1),
		"duplicate record":  strings.Replace(string(raw), stringMustMarshal(good.Records), stringMustMarshal(append(good.Records, good.Records[0])), 1),
		"too large":         strings.Repeat(" ", MaxBytes) + string(raw),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(body), now); err == nil {
				t.Fatal("invalid batch accepted")
			}
		})
	}
}

func TestDecodeLimits(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	fixture := Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abc", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "limits", Records: []Record{{ExternalID: "sample", CheckedAt: "2026-09-19T00:00:00Z", Payload: Payload{Name: "合成人物", Aliases: []string{}, ProfileURL: "https://example.test/models/sample/", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}}
	for _, tc := range []struct {
		name   string
		mutate func(*Batch)
		valid  bool
	}{
		{"name at limit", func(b *Batch) { b.Records[0].Payload.Name = strings.Repeat("名", 200) }, true},
		{"name over limit", func(b *Batch) { b.Records[0].Payload.Name = strings.Repeat("名", 201) }, false},
		{"future allowance", func(b *Batch) { b.Records[0].CheckedAt = now.Add(5 * time.Minute).Format(time.RFC3339Nano) }, true},
		{"future exceeded", func(b *Batch) {
			b.Records[0].CheckedAt = now.Add(5*time.Minute + time.Nanosecond).Format(time.RFC3339Nano)
		}, false},
		{"records at limit", func(b *Batch) {
			record := b.Records[0]
			for i := 1; i < MaxRecords; i++ {
				record.ExternalID = fmt.Sprintf("sample-%d", i)
				b.Records = append(b.Records, record)
			}
		}, true},
		{"records over limit", func(b *Batch) {
			record := b.Records[0]
			for i := 1; i <= MaxRecords; i++ {
				record.ExternalID = fmt.Sprintf("sample-%d", i)
				b.Records = append(b.Records, record)
			}
		}, false},
		{"aliases at limit", func(b *Batch) {
			for i := 0; i < 30; i++ {
				b.Records[0].Payload.Aliases = append(b.Records[0].Payload.Aliases, fmt.Sprintf("alias-%d", i))
			}
		}, true},
		{"aliases over limit", func(b *Batch) {
			for i := 0; i < 31; i++ {
				b.Records[0].Payload.Aliases = append(b.Records[0].Payload.Aliases, fmt.Sprintf("alias-%d", i))
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b Batch
			if err := json.Unmarshal([]byte(stringMustMarshal(fixture)), &b); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&b)
			_, err := Decode([]byte(stringMustMarshal(b)), now)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, decode error=%v", tc.valid, err)
			}
		})
	}
}
func stringMustMarshal(v any) string { b, _ := json.Marshal(v); return string(b) }
