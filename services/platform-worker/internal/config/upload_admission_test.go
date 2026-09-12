package config

import (
	"strings"
	"testing"
)

func TestUploadAdmissionExplicitPrivateDependencies(t *testing.T) {
	for key, value := range map[string]string{
		"WORKER_REGION": "japan", "DATABASE_URL": "postgres://test", "DISPLAY_REVALIDATE_URL": "http://display/revalidate",
		"CACHE_HMAC_SECRET": strings.Repeat("c", 40), "MEDIA_DELETE_URL": "http://media/delete", "MEDIA_HMAC_SECRET": strings.Repeat("m", 40),
		"MEDIA_USAGE_MONITOR_MODE": "observe", "MEDIA_USAGE_PERIOD_START": "2026-09-01T00:00:00Z", "MEDIA_USAGE_PERIOD_END": "2026-10-01T00:00:00Z",
		"MEDIA_USAGE_INTERVAL": "15m", "R2_ANALYTICS_ACCOUNT_ID": strings.Repeat("a", 32), "R2_ANALYTICS_API_TOKEN": strings.Repeat("t", 40),
		"MEDIA_UPLOAD_QUEUE_MODE": "dispatch", "MEDIA_UPLOAD_CONTROL_URL": "https://192.0.2.20", "MEDIA_UPLOAD_CONTROL_SECRET": strings.Repeat("u", 40),
		"MEDIA_UPLOAD_ADMISSION_MODE": "enforce", "MEDIA_UPLOAD_ADMISSION_SECRET": strings.Repeat("g", 40), "METRICS_TOKEN": strings.Repeat("z", 40),
	} {
		t.Setenv(key, value)
	}
	if err := FromEnv().Validate(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"MEDIA_UPLOAD_ADMISSION_MODE": "typo-private", "MEDIA_UPLOAD_ADMISSION_SECRET": "short-private", "MEDIA_UPLOAD_QUEUE_MODE": "off", "MEDIA_USAGE_MONITOR_MODE": "off", "WORKER_REGION": "beijing"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			err := FromEnv().Validate()
			if err == nil || strings.Contains(err.Error(), "typo-private") || strings.Contains(err.Error(), "short-private") {
				t.Fatal("invalid configuration allowed or leaked")
			}
		})
	}
	for _, secret := range []string{strings.Repeat("m", 40), strings.Repeat("u", 40), strings.Repeat("c", 40), strings.Repeat("z", 40), strings.Repeat("g", 4097)} {
		c := FromEnv()
		c.MediaUploadAdmissionSecret = secret
		if c.Validate() == nil {
			t.Fatal("shared or oversized admission secret accepted")
		}
	}
	t.Setenv("MEDIA_UPLOAD_ADMISSION_MODE", "")
	if FromEnv().MediaUploadAdmissionMode != "off" {
		t.Fatal("admission not default off")
	}
}
