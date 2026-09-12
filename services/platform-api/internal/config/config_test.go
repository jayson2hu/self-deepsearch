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

func TestEmailDeliverySettingsFromEnvironment(t *testing.T) {
	t.Setenv("EMAIL_DAILY_LIMIT", "125")
	t.Setenv("EMAIL_FAILURE_THRESHOLD", "7")
	t.Setenv("EMAIL_CIRCUIT_COOLDOWN", "45m")
	t.Setenv("EMAIL_SEND_TIMEOUT", "4s")

	configuration := FromEnv()
	if configuration.EmailDailyLimit != 125 || configuration.EmailFailureThreshold != 7 {
		t.Fatalf("unexpected email limits: %#v", configuration)
	}
	if configuration.EmailCooldown != 45*time.Minute || configuration.EmailSendTimeout != 4*time.Second {
		t.Fatalf("unexpected email durations: %#v", configuration)
	}
}

func TestTurnstileExpectedHostnameFromEnvironment(t *testing.T) {
	t.Setenv("TURNSTILE_EXPECTED_HOSTNAME", " display.example.test ")
	if hostname := FromEnv().TurnstileExpectedHostname; hostname != "display.example.test" {
		t.Fatalf("unexpected Turnstile hostname %q", hostname)
	}
}

func TestInvalidEmailDeliverySettingsUseFailSafeDefaults(t *testing.T) {
	t.Setenv("EMAIL_DAILY_LIMIT", "0")
	t.Setenv("EMAIL_FAILURE_THRESHOLD", "invalid")
	t.Setenv("EMAIL_CIRCUIT_COOLDOWN", "0s")
	t.Setenv("EMAIL_SEND_TIMEOUT", "999h")

	configuration := FromEnv()
	defaults := struct {
		dailyLimit       int
		failureThreshold int
		cooldown         time.Duration
		sendTimeout      time.Duration
	}{500, 5, 15 * time.Minute, 10 * time.Second}
	if configuration.EmailDailyLimit != defaults.dailyLimit || configuration.EmailFailureThreshold != defaults.failureThreshold ||
		configuration.EmailCooldown != defaults.cooldown || configuration.EmailSendTimeout != defaults.sendTimeout {
		t.Fatalf("invalid values did not use safe defaults: %#v", configuration)
	}
}

func TestValidateProductionRequiresReleaseAIntegrations(t *testing.T) {
	base := Config{
		DatabaseURL:               "postgres://platform-api@db/self_deepsearch",
		AuthHMACSecret:            "01234567890123456789012345678901",
		SMTPURL:                   "smtps://smtp.example.test",
		TurnstileSecret:           "turnstile-secret",
		TurnstileSiteKey:          "turnstile-site-key",
		TurnstileExpectedHostname: "display.example.test",
		AllowedOrigins:            []string{"https://display.example.test"},
		MediaPublicBaseURL:        "https://media.example.test",
		MediaDeliveryMode:         "normal",
		MediaDeliveryDynamicMode:  "off",
		MediaEdgePolicyMode:       "off",
	}
	if err := base.ValidateProduction(); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "database", mutate: func(configuration *Config) { configuration.DatabaseURL = "" }},
		{name: "media mode", mutate: func(configuration *Config) { configuration.MediaDeliveryMode = "typo" }},
		{name: "dynamic media mode", mutate: func(configuration *Config) { configuration.MediaDeliveryDynamicMode = "typo" }},
		{name: "edge policy mode", mutate: func(configuration *Config) { configuration.MediaEdgePolicyMode = "typo" }},
		{name: "edge policy token", mutate: func(configuration *Config) { configuration.MediaEdgePolicyMode = "enforce" }},
		{name: "edge policy token reuse", mutate: func(configuration *Config) {
			configuration.MediaEdgePolicyMode = "enforce"
			configuration.MediaEdgePolicyToken = configuration.AuthHMACSecret
		}},
		{name: "edge direct r2 bypass", mutate: func(configuration *Config) {
			configuration.MediaEdgePolicyMode = "enforce"
			configuration.MediaEdgePolicyToken = "edge-policy-token-012345678901234567890123"
			configuration.MediaPublicBaseURL = "https://public-bucket.example.r2.dev"
		}},
		{name: "auth secret", mutate: func(configuration *Config) { configuration.AuthHMACSecret = "short" }},
		{name: "smtp", mutate: func(configuration *Config) { configuration.SMTPURL = "" }},
		{name: "turnstile", mutate: func(configuration *Config) { configuration.TurnstileSecret = "" }},
		{name: "turnstile site key", mutate: func(configuration *Config) { configuration.TurnstileSiteKey = "" }},
		{name: "turnstile hostname", mutate: func(configuration *Config) { configuration.TurnstileExpectedHostname = "" }},
		{name: "turnstile hostname URL", mutate: func(configuration *Config) { configuration.TurnstileExpectedHostname = "https://display.example.test" }},
		{name: "turnstile hostname port", mutate: func(configuration *Config) { configuration.TurnstileExpectedHostname = "display.example.test:443" }},
		{name: "turnstile hostname label", mutate: func(configuration *Config) { configuration.TurnstileExpectedHostname = "display_.example.test" }},
		{name: "turnstile hostname IP", mutate: func(configuration *Config) { configuration.TurnstileExpectedHostname = "192.0.2.10" }},
		{name: "origins", mutate: func(configuration *Config) { configuration.AllowedOrigins = nil }},
		{name: "media base", mutate: func(configuration *Config) { configuration.MediaPublicBaseURL = "" }},
		{name: "media base path", mutate: func(configuration *Config) { configuration.MediaPublicBaseURL = "https://media.example.test/public" }},
		{name: "turnstile bypass", mutate: func(configuration *Config) { configuration.TurnstileBypass = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := base
			test.mutate(&configuration)
			if err := configuration.ValidateProduction(); err == nil {
				t.Fatalf("ValidateProduction accepted missing %s configuration", test.name)
			}
		})
	}
}

func TestValidateProductionAcceptsIndependentEdgePolicyCredential(t *testing.T) {
	configuration := Config{
		DatabaseURL:               "postgres://platform-api@db/self_deepsearch",
		AuthHMACSecret:            "01234567890123456789012345678901",
		SMTPURL:                   "smtps://smtp.example.test",
		TurnstileSecret:           "turnstile-secret",
		TurnstileSiteKey:          "turnstile-site-key",
		TurnstileExpectedHostname: "display.example.test",
		AllowedOrigins:            []string{"https://display.example.test"},
		MetricsToken:              "metrics-token-01234567890123456789",
		MediaPublicBaseURL:        "https://media.example.test",
		MediaDeliveryMode:         "normal",
		MediaDeliveryDynamicMode:  "off",
		MediaEdgePolicyMode:       "enforce",
		MediaEdgePolicyToken:      "edge-policy-token-012345678901234567890123",
	}
	if err := configuration.ValidateProduction(); err != nil {
		t.Fatalf("valid edge policy configuration rejected: %v", err)
	}
}
