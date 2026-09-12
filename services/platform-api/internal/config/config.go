package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var DefaultBuildVersion = "dev"

type Config struct {
	Addr                      string
	BuildVersion              string
	DatabaseURL               string
	AllowedOrigins            []string
	AppEnv                    string
	AuthHMACSecret            string
	SMTPURL                   string
	EmailDailyLimit           int
	EmailFailureThreshold     int
	EmailCooldown             time.Duration
	EmailSendTimeout          time.Duration
	TurnstileSecret           string
	TurnstileSiteKey          string
	TurnstileExpectedHostname string
	TurnstileBypass           bool
	TrustProxyHeaders         bool
	MetricsToken              string
	MediaPublicBaseURL        string
	MediaDeliveryMode         string
	MediaDeliveryDynamicMode  string
	MediaEdgePolicyMode       string
	MediaEdgePolicyToken      string
}

func FromEnv() Config {
	return Config{
		Addr:                      envOr("API_ADDR", ":8080"),
		BuildVersion:              envOr("BUILD_VERSION", DefaultBuildVersion),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		AllowedOrigins:            splitList(envOr("ALLOWED_ORIGINS", "http://127.0.0.1:3000,http://127.0.0.1:3001")),
		AppEnv:                    envOr("APP_ENV", "development"),
		AuthHMACSecret:            os.Getenv("AUTH_HMAC_SECRET"),
		SMTPURL:                   os.Getenv("SMTP_URL"),
		EmailDailyLimit:           positiveIntEnv("EMAIL_DAILY_LIMIT", 500, 10_000_000),
		EmailFailureThreshold:     positiveIntEnv("EMAIL_FAILURE_THRESHOLD", 5, 10_000),
		EmailCooldown:             positiveDurationEnv("EMAIL_CIRCUIT_COOLDOWN", 15*time.Minute, 7*24*time.Hour),
		EmailSendTimeout:          positiveDurationEnv("EMAIL_SEND_TIMEOUT", 10*time.Second, 5*time.Minute),
		TurnstileSecret:           os.Getenv("TURNSTILE_SECRET_KEY"),
		TurnstileSiteKey:          os.Getenv("TURNSTILE_SITE_KEY"),
		TurnstileExpectedHostname: strings.TrimSpace(os.Getenv("TURNSTILE_EXPECTED_HOSTNAME")),
		TurnstileBypass:           envOr("TURNSTILE_BYPASS", "false") == "true",
		TrustProxyHeaders:         envOr("TRUST_PROXY_HEADERS", "false") == "true",
		MetricsToken:              os.Getenv("METRICS_TOKEN"),
		MediaPublicBaseURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("S3_PUBLIC_BASE_URL")), "/"),
		MediaDeliveryMode:         envOr("MEDIA_DELIVERY_MODE", "normal"),
		MediaDeliveryDynamicMode:  envOr("MEDIA_DELIVERY_DYNAMIC_MODE", "off"),
		MediaEdgePolicyMode:       envOr("MEDIA_EDGE_POLICY_MODE", "off"),
		MediaEdgePolicyToken:      os.Getenv("MEDIA_EDGE_POLICY_TOKEN"),
	}
}

// ValidateProduction rejects a configuration that would start an apparently
// healthy API while silently disabling required Release A account flows.
// Development keeps the existing degraded-start behavior for page-shell work.
func (configuration Config) ValidateProduction() error {
	if configuration.MediaDeliveryMode != "normal" && configuration.MediaDeliveryMode != "default_only" {
		return fmt.Errorf("MEDIA_DELIVERY_MODE must be normal or default_only")
	}
	if configuration.MediaDeliveryDynamicMode != "off" && configuration.MediaDeliveryDynamicMode != "enforce" {
		return fmt.Errorf("MEDIA_DELIVERY_DYNAMIC_MODE must be off or enforce")
	}
	if configuration.MediaEdgePolicyMode != "off" && configuration.MediaEdgePolicyMode != "enforce" {
		return fmt.Errorf("MEDIA_EDGE_POLICY_MODE must be off or enforce")
	}
	if configuration.MediaEdgePolicyMode == "enforce" {
		token := strings.TrimSpace(configuration.MediaEdgePolicyToken)
		if len(token) < 32 || len(token) > 4096 {
			return fmt.Errorf("MEDIA_EDGE_POLICY_TOKEN must contain 32 to 4096 characters when the edge policy is enabled")
		}
		for _, other := range []string{configuration.AuthHMACSecret, configuration.MetricsToken} {
			if token == strings.TrimSpace(other) {
				return fmt.Errorf("MEDIA_EDGE_POLICY_TOKEN must be independent from other service credentials")
			}
		}
	}
	mediaBase, err := url.Parse(configuration.MediaPublicBaseURL)
	if err != nil || mediaBase.Scheme != "https" || mediaBase.Host == "" || mediaBase.User != nil ||
		mediaBase.RawQuery != "" || mediaBase.Fragment != "" || (mediaBase.Path != "" && mediaBase.Path != "/") {
		return fmt.Errorf("S3_PUBLIC_BASE_URL must be an HTTPS origin in production")
	}
	if configuration.MediaEdgePolicyMode == "enforce" {
		host := strings.ToLower(mediaBase.Hostname())
		if strings.HasSuffix(host, ".r2.dev") || strings.HasSuffix(host, ".r2.cloudflarestorage.com") {
			return fmt.Errorf("S3_PUBLIC_BASE_URL must use the controlled media gateway origin while the edge policy is enabled")
		}
	}
	if strings.TrimSpace(configuration.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required in production")
	}
	if len(strings.TrimSpace(configuration.AuthHMACSecret)) < 32 {
		return fmt.Errorf("AUTH_HMAC_SECRET must contain at least 32 characters in production")
	}
	if strings.TrimSpace(configuration.SMTPURL) == "" {
		return fmt.Errorf("SMTP_URL is required in production")
	}
	if configuration.TurnstileBypass {
		return fmt.Errorf("TURNSTILE_BYPASS cannot be enabled in production")
	}
	if strings.TrimSpace(configuration.TurnstileSecret) == "" {
		return fmt.Errorf("TURNSTILE_SECRET_KEY is required in production")
	}
	if strings.TrimSpace(configuration.TurnstileSiteKey) == "" {
		return fmt.Errorf("TURNSTILE_SITE_KEY is required in production")
	}
	if !validPlainHostname(configuration.TurnstileExpectedHostname) {
		return fmt.Errorf("TURNSTILE_EXPECTED_HOSTNAME must be a plain DNS hostname in production")
	}
	if len(configuration.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_ORIGINS must contain at least one origin in production")
	}
	return nil
}

func validPlainHostname(value string) bool {
	hostname := strings.TrimSpace(value)
	if hostname == "" || hostname != value || len(hostname) > 253 || strings.HasSuffix(hostname, ".") || net.ParseIP(hostname) != nil {
		return false
	}
	labels := strings.Split(hostname, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func positiveIntEnv(key string, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 || value > maximum {
		return fallback
	}
	return value
}

func positiveDurationEnv(key string, fallback, maximum time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 || value > maximum {
		return fallback
	}
	return value
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
