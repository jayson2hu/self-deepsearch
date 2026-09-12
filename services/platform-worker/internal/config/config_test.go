package config

import (
	"testing"
	"time"
)

func TestFromEnvUsesEmbeddedBuildVersionFallback(t *testing.T) {
	previous := DefaultBuildVersion
	DefaultBuildVersion = "release-a-test"
	t.Cleanup(func() { DefaultBuildVersion = previous })

	t.Setenv("BUILD_VERSION", "")
	if configuration := FromEnv(); configuration.BuildVersion != "release-a-test" {
		t.Fatalf("expected embedded build version fallback, got %q", configuration.BuildVersion)
	}
	t.Setenv("BUILD_VERSION", "runtime-override")
	if configuration := FromEnv(); configuration.BuildVersion != "runtime-override" {
		t.Fatalf("expected runtime build version override, got %q", configuration.BuildVersion)
	}
}

func TestValidateRegions(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "japan", config: Config{Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test", DisplayRevalidateURL: "http://display/revalidate", CacheHMACSecret: "12345678901234567890123456789012", MediaDeleteURL: "http://media/delete", MediaHMACSecret: "12345678901234567890123456789012", PollInterval: time.Second, LeaseDuration: 30 * time.Second}},
		{name: "beijing", config: Config{Region: "beijing", Capabilities: capabilitiesFor("beijing"), PollInterval: time.Second, LeaseDuration: 30 * time.Second}},
		{name: "unknown", config: Config{Region: "unknown"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestReleaseARejectsAutomatedCollection(t *testing.T) {
	configuration := Config{
		Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test",
		DisplayRevalidateURL: "http://display/revalidate", CacheHMACSecret: "12345678901234567890123456789012",
		MediaDeleteURL: "http://media/delete", MediaHMACSecret: "12345678901234567890123456789012",
		PollInterval: time.Second, LeaseDuration: 30 * time.Second, CollectionEnabled: true,
	}
	if err := configuration.Validate(); err == nil {
		t.Fatal("Release A worker must reject COLLECTION_ENABLED=true")
	}
}

func TestFromEnvReadsCollectionEnabled(t *testing.T) {
	t.Setenv("COLLECTION_ENABLED", "true")
	if configuration := FromEnv(); !configuration.CollectionEnabled {
		t.Fatal("COLLECTION_ENABLED=true was not read")
	}
	t.Setenv("COLLECTION_ENABLED", "false")
	if configuration := FromEnv(); configuration.CollectionEnabled {
		t.Fatal("COLLECTION_ENABLED=false was not read")
	}
}

func TestValidateHistoryArchive(t *testing.T) {
	base := Config{
		Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test",
		DisplayRevalidateURL: "http://display/revalidate", CacheHMACSecret: "12345678901234567890123456789012",
		MediaDeleteURL: "http://media/delete", MediaHMACSecret: "12345678901234567890123456789012",
		PollInterval: time.Second, LeaseDuration: 30 * time.Second,
		HistoryArchiveEnabled: true, HistoryArchiveDir: "/archives",
		HistoryArchiveEvery: time.Hour, HistoryArchiveAge: 30 * 24 * time.Hour, HistoryArchiveBatch: 1000,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid archive config rejected: %v", err)
	}
	base.Region = "beijing"
	base.DatabaseURL = ""
	if err := base.Validate(); err == nil {
		t.Fatal("beijing worker must not run database history archives")
	}
}

func TestJapanRequiresCacheRevalidationConfiguration(t *testing.T) {
	configuration := Config{Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test", PollInterval: time.Second, LeaseDuration: 30 * time.Second}
	if err := configuration.Validate(); err == nil {
		t.Fatal("japan worker must not complete publication events without cache revalidation configuration")
	}
}

func TestValidateMediaReconciliation(t *testing.T) {
	configuration := Config{
		Region: "japan", Capabilities: capabilitiesFor("japan"), DatabaseURL: "postgres://test",
		DisplayRevalidateURL: "http://display/revalidate", CacheHMACSecret: "12345678901234567890123456789012",
		MediaDeleteURL: "http://media/delete", MediaHMACSecret: "12345678901234567890123456789012",
		MediaReconcileEnabled: true, MediaReconcileURL: "http://media/reconcile", MediaReconcileEvery: 24 * time.Hour,
		PollInterval: time.Second, LeaseDuration: 30 * time.Second,
	}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("valid media reconciliation config rejected: %v", err)
	}
	configuration.MediaReconcileEvery = 30 * time.Minute
	if err := configuration.Validate(); err == nil {
		t.Fatal("sub-hour reconciliation interval must be rejected")
	}
}

func TestMediaInspectionConfiguration(t *testing.T) {
	t.Setenv("MEDIA_INSPECT_ENABLED", "false")
	if FromEnv().MediaInspectEnabled {
		t.Fatal("inspection should default to disabled in local development")
	}
	t.Setenv("MEDIA_INSPECT_ENABLED", "true")
	t.Setenv("MEDIA_INSPECT_DISPLAY_ORIGIN", "https://display.test")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("DISPLAY_REVALIDATE_URL", "http://display/revalidate")
	t.Setenv("CACHE_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("MEDIA_DELETE_URL", "http://media/delete")
	t.Setenv("MEDIA_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("WORKER_REGION", "japan")
	configuration := FromEnv()
	if !configuration.MediaInspectEnabled || configuration.MediaInspectDisplayOrigin != "https://display.test" || configuration.Validate() != nil {
		t.Fatal("valid inspection config rejected")
	}
	configuration.Region, configuration.Capabilities = "beijing", capabilitiesFor("beijing")
	if configuration.Validate() == nil {
		t.Fatal("Beijing must not run shared database inspection")
	}
	configuration.Region = "japan"
	configuration.MediaInspectDisplayOrigin = "https://display.test/from-record"
	if configuration.Validate() == nil {
		t.Fatal("record URL accepted as inspection origin")
	}
}
