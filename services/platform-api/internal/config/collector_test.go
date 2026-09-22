package config

import (
	"self-deepsearch/services/platform-api/internal/collectoringest"
	"strings"
	"testing"
)

func TestCollectorConfig(t *testing.T) {
	c := Config{}
	if c.ValidateCollector() != nil {
		t.Fatal("disabled rejected")
	}
	c.CollectorCredentials = collectoringest.Credentials{SourceID: "12345678-1234-1234-1234-123456789abc", Token: strings.Repeat("x", 40)}
	if c.ValidateCollector() == nil {
		t.Fatal("missing database accepted")
	}
	c.DatabaseURL = "postgres://example.test/test"
	if c.ValidateCollector() != nil {
		t.Fatal("valid config rejected")
	}
	c.MetricsToken = c.CollectorCredentials.Token
	if c.ValidateCollector() == nil {
		t.Fatal("shared credential accepted")
	}
}
