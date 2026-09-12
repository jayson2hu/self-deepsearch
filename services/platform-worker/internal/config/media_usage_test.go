package config

import (
	"strings"
	"testing"
)

func TestUsageObserverIsExplicitJapanOnlyAndDoesNotGuessPeriod(t *testing.T) {
	t.Setenv("MEDIA_USAGE_MONITOR_MODE", "off")
	if FromEnv().MediaUsageMode != "off" {
		t.Fatal("observer default must be off")
	}
	for key, value := range map[string]string{
		"WORKER_REGION": "japan", "DATABASE_URL": "postgres://test", "DISPLAY_REVALIDATE_URL": "http://display/revalidate",
		"CACHE_HMAC_SECRET": strings.Repeat("c", 32), "MEDIA_DELETE_URL": "http://media/delete", "MEDIA_HMAC_SECRET": strings.Repeat("m", 32),
		"MEDIA_USAGE_MONITOR_MODE": "observe", "MEDIA_USAGE_PERIOD_START": "2026-09-01T00:00:00Z", "MEDIA_USAGE_PERIOD_END": "2026-10-01T00:00:00Z",
		"MEDIA_USAGE_INTERVAL": "15m", "R2_ANALYTICS_ACCOUNT_ID": strings.Repeat("a", 32), "R2_ANALYTICS_API_TOKEN": strings.Repeat("t", 40),
	} {
		t.Setenv(key, value)
	}
	base := FromEnv()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid observer rejected: %v", err)
	}
	for key, value := range map[string]string{"MEDIA_USAGE_MONITOR_MODE": "enforce", "MEDIA_USAGE_PERIOD_START": "", "MEDIA_USAGE_PERIOD_END": "bad-secret-value", "MEDIA_USAGE_INTERVAL": "1m", "R2_ANALYTICS_ACCOUNT_ID": "wrong", "R2_ANALYTICS_API_TOKEN": "bad-secret-value", "WORKER_REGION": "beijing"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if err := FromEnv().Validate(); err == nil || strings.Contains(err.Error(), "bad-secret-value") {
				t.Fatal("invalid/sensitive observer config mishandled")
			}
		})
	}
}

func TestReconciliationUsageGuardIsExplicitAndCannotRunWithoutItsDependencies(t *testing.T) {
	for key, value := range map[string]string{
		"WORKER_REGION": "japan", "DATABASE_URL": "postgres://test", "DISPLAY_REVALIDATE_URL": "http://display/revalidate",
		"CACHE_HMAC_SECRET": strings.Repeat("c", 32), "MEDIA_DELETE_URL": "http://media/delete", "MEDIA_HMAC_SECRET": strings.Repeat("m", 32),
		"MEDIA_USAGE_MONITOR_MODE": "observe", "MEDIA_USAGE_PERIOD_START": "2026-09-01T00:00:00Z", "MEDIA_USAGE_PERIOD_END": "2026-10-01T00:00:00Z",
		"MEDIA_USAGE_INTERVAL": "15m", "R2_ANALYTICS_ACCOUNT_ID": strings.Repeat("a", 32), "R2_ANALYTICS_API_TOKEN": strings.Repeat("t", 40),
		"MEDIA_RECONCILE_ENABLED": "true", "MEDIA_RECONCILE_URL": "http://media/v1/reconcile", "MEDIA_RECONCILE_USAGE_GUARD": "enforce",
	} {
		t.Setenv(key, value)
	}
	if err := FromEnv().Validate(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"MEDIA_RECONCILE_USAGE_GUARD": "typo-private", "MEDIA_USAGE_MONITOR_MODE": "off", "MEDIA_RECONCILE_ENABLED": "false", "WORKER_REGION": "beijing"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if err := FromEnv().Validate(); err == nil || strings.Contains(err.Error(), "typo-private") {
				t.Fatal("invalid admission configuration accepted or leaked")
			}
		})
	}
	t.Setenv("MEDIA_RECONCILE_USAGE_GUARD", "")
	if FromEnv().MediaReconcileUsageGuard != "off" {
		t.Fatal("guard must default off")
	}
}
