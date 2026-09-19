package config

import "testing"

func TestSEOScheduleIsOptInAndValidated(t *testing.T) {
	t.Setenv("SEO_DAILY_SCAN_ENABLED", "")
	if FromEnv().SEODailyScanEnabled {
		t.Fatal("schedule must default off")
	}
	c := Config{SEODailyScanEnabled: true}
	if c.ValidateSEOSchedule() == nil {
		t.Fatal("missing config accepted")
	}
	c.DatabaseURL = "local"
	c.SEODailyScanActorID = "not-a-uuid"
	if c.ValidateSEOSchedule() == nil {
		t.Fatal("invalid actor accepted")
	}
	c.SEODailyScanActorID = "10000000-0000-4000-8000-000000000001"
	if err := c.ValidateSEOSchedule(); err != nil {
		t.Fatal(err)
	}
}
