package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"self-deepsearch/services/platform-worker/internal/mediainspect"
	"self-deepsearch/services/platform-worker/internal/mediauploadcontrol"
	"self-deepsearch/services/platform-worker/internal/mediausage"
)

var DefaultBuildVersion = "dev"

type Config struct {
	Addr                       string
	BuildVersion               string
	Region                     string
	Capabilities               []string
	DatabaseURL                string
	WorkerID                   string
	CollectionEnabled          bool
	PollInterval               time.Duration
	LeaseDuration              time.Duration
	MetricsToken               string
	DisplayRevalidateURL       string
	CacheHMACSecret            string
	MediaDeleteURL             string
	MediaHMACSecret            string
	MediaReconcileEnabled      bool
	MediaReconcileURL          string
	MediaReconcileEvery        time.Duration
	MediaInspectEnabled        bool
	MediaInspectDisplayOrigin  string
	MediaUsageMode             string
	MediaReconcileUsageGuard   string
	MediaUsageSchedule         mediausage.Schedule
	MediaUsageToken            string
	MediaUploadQueueMode       string
	MediaUploadControlURL      string
	MediaUploadControlSecret   string
	MediaUploadAllowHTTP       bool
	MediaUploadAdmissionMode   string
	MediaUploadAdmissionSecret string
	HistoryArchiveEnabled      bool
	HistoryArchiveDir          string
	HistoryArchiveEvery        time.Duration
	HistoryArchiveAge          time.Duration
	HistoryArchiveBatch        int
}

func FromEnv() Config {
	region := envOr("WORKER_REGION", "japan")
	pollInterval, _ := time.ParseDuration(envOr("WORKER_POLL_INTERVAL", "2s"))
	leaseDuration, _ := time.ParseDuration(envOr("WORKER_LEASE_DURATION", "30s"))
	archiveEvery, _ := time.ParseDuration(envOr("HISTORY_ARCHIVE_INTERVAL", "6h"))
	archiveAge, _ := time.ParseDuration(envOr("HISTORY_ARCHIVE_MIN_AGE", "720h"))
	archiveBatch, _ := strconv.Atoi(envOr("HISTORY_ARCHIVE_BATCH_SIZE", "1000"))
	reconcileEvery, _ := time.ParseDuration(envOr("MEDIA_RECONCILE_INTERVAL", "24h"))
	usageEvery, _ := time.ParseDuration(envOr("MEDIA_USAGE_INTERVAL", "15m"))
	usageStart, _ := time.Parse(time.RFC3339, os.Getenv("MEDIA_USAGE_PERIOD_START"))
	usageEnd, _ := time.Parse(time.RFC3339, os.Getenv("MEDIA_USAGE_PERIOD_END"))
	hostname, _ := os.Hostname()
	return Config{
		Addr:                       envOr("WORKER_ADDR", ":8081"),
		BuildVersion:               envOr("BUILD_VERSION", DefaultBuildVersion),
		Region:                     region,
		Capabilities:               capabilitiesFor(region),
		DatabaseURL:                os.Getenv("DATABASE_URL"),
		WorkerID:                   envOr("WORKER_ID", region+"-"+hostname),
		CollectionEnabled:          strings.EqualFold(envOr("COLLECTION_ENABLED", "false"), "true"),
		PollInterval:               pollInterval,
		LeaseDuration:              leaseDuration,
		MetricsToken:               os.Getenv("METRICS_TOKEN"),
		DisplayRevalidateURL:       os.Getenv("DISPLAY_REVALIDATE_URL"),
		CacheHMACSecret:            os.Getenv("CACHE_HMAC_SECRET"),
		MediaDeleteURL:             os.Getenv("MEDIA_DELETE_URL"),
		MediaHMACSecret:            os.Getenv("MEDIA_HMAC_SECRET"),
		MediaReconcileEnabled:      envOr("MEDIA_RECONCILE_ENABLED", "false") == "true",
		MediaReconcileURL:          os.Getenv("MEDIA_RECONCILE_URL"),
		MediaReconcileEvery:        reconcileEvery,
		MediaInspectEnabled:        envOr("MEDIA_INSPECT_ENABLED", "false") == "true",
		MediaInspectDisplayOrigin:  os.Getenv("MEDIA_INSPECT_DISPLAY_ORIGIN"),
		MediaUsageMode:             envOr("MEDIA_USAGE_MONITOR_MODE", "off"),
		MediaReconcileUsageGuard:   envOr("MEDIA_RECONCILE_USAGE_GUARD", "off"),
		MediaUsageSchedule:         mediausage.Schedule{AccountID: os.Getenv("R2_ANALYTICS_ACCOUNT_ID"), PeriodStart: usageStart, PeriodEnd: usageEnd, Every: usageEvery, Policy: mediausage.DefaultPolicy()},
		MediaUsageToken:            os.Getenv("R2_ANALYTICS_API_TOKEN"),
		MediaUploadQueueMode:       envOr("MEDIA_UPLOAD_QUEUE_MODE", "off"),
		MediaUploadControlURL:      os.Getenv("MEDIA_UPLOAD_CONTROL_URL"),
		MediaUploadControlSecret:   os.Getenv("MEDIA_UPLOAD_CONTROL_SECRET"),
		MediaUploadAllowHTTP:       envOr("MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "false") == "true",
		MediaUploadAdmissionMode:   envOr("MEDIA_UPLOAD_ADMISSION_MODE", "off"),
		MediaUploadAdmissionSecret: os.Getenv("MEDIA_UPLOAD_ADMISSION_SECRET"),
		HistoryArchiveEnabled:      envOr("HISTORY_ARCHIVE_ENABLED", "false") == "true",
		HistoryArchiveDir:          envOr("HISTORY_ARCHIVE_DIR", "/var/lib/self-deepsearch/history-archives"),
		HistoryArchiveEvery:        archiveEvery,
		HistoryArchiveAge:          archiveAge,
		HistoryArchiveBatch:        archiveBatch,
	}
}

func (configuration Config) Validate() error {
	if configuration.MediaUploadAdmissionMode != "" && configuration.MediaUploadAdmissionMode != "off" && configuration.MediaUploadAdmissionMode != "enforce" {
		return fmt.Errorf("MEDIA_UPLOAD_ADMISSION_MODE must be off or enforce")
	}
	if configuration.MediaUploadAdmissionMode == "enforce" {
		secret := strings.TrimSpace(configuration.MediaUploadAdmissionSecret)
		if configuration.Region != "japan" || configuration.MediaUsageMode != "observe" || configuration.MediaUploadQueueMode != "dispatch" || len(secret) < 32 || len(secret) > 4096 ||
			secret == strings.TrimSpace(configuration.MediaHMACSecret) || secret == strings.TrimSpace(configuration.MediaUploadControlSecret) || secret == strings.TrimSpace(configuration.MetricsToken) || secret == strings.TrimSpace(configuration.CacheHMACSecret) {
			return fmt.Errorf("upload admission requires Japan, observe, dispatch and an independent private secret")
		}
	}
	if configuration.MediaUploadQueueMode != "" && configuration.MediaUploadQueueMode != "off" && configuration.MediaUploadQueueMode != "dispatch" {
		return fmt.Errorf("MEDIA_UPLOAD_QUEUE_MODE must be off or dispatch")
	}
	if configuration.MediaUploadQueueMode == "dispatch" {
		if configuration.Region != "japan" || strings.TrimSpace(configuration.MediaUploadControlSecret) == strings.TrimSpace(configuration.MediaHMACSecret) {
			return fmt.Errorf("upload control queue requires Japan and a separate control secret")
		}
		if _, err := mediauploadcontrol.NewClient(configuration.MediaUploadControlURL, configuration.MediaUploadControlSecret, configuration.MediaUploadAllowHTTP, nil); err != nil {
			return fmt.Errorf("upload control queue requires a valid private origin, secret and explicit HTTP opt-in")
		}
	}
	if configuration.Region != "japan" && configuration.Region != "beijing" {
		return fmt.Errorf("unsupported WORKER_REGION %q", configuration.Region)
	}
	if len(configuration.Capabilities) == 0 {
		return fmt.Errorf("worker region %q has no capabilities", configuration.Region)
	}
	if configuration.CollectionEnabled {
		return fmt.Errorf("automated collection is unavailable in Release A; COLLECTION_ENABLED must be false")
	}
	if configuration.MediaUsageMode != "" && configuration.MediaUsageMode != "off" && configuration.MediaUsageMode != "observe" {
		return fmt.Errorf("MEDIA_USAGE_MONITOR_MODE must be off or observe")
	}
	if configuration.MediaUsageMode == "observe" {
		if configuration.Region != "japan" || configuration.MediaUsageSchedule.Validate() != nil {
			return fmt.Errorf("media usage observer requires Japan and valid R2 account, explicit period and interval")
		}
		if _, err := mediausage.NewClient(configuration.MediaUsageSchedule.AccountID, configuration.MediaUsageToken); err != nil {
			return fmt.Errorf("R2_ANALYTICS_API_TOKEN must be a valid read-only analytics token")
		}
	}
	if configuration.MediaReconcileUsageGuard != "" && configuration.MediaReconcileUsageGuard != "off" && configuration.MediaReconcileUsageGuard != "enforce" {
		return fmt.Errorf("MEDIA_RECONCILE_USAGE_GUARD must be off or enforce")
	}
	if configuration.MediaReconcileUsageGuard == "enforce" &&
		(configuration.Region != "japan" || configuration.MediaUsageMode != "observe" || !configuration.MediaReconcileEnabled) {
		return fmt.Errorf("reconciliation usage guard requires Japan, observe mode and enabled reconciliation")
	}
	if configuration.Region == "japan" && strings.TrimSpace(configuration.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for the japan worker")
	}
	if configuration.Region == "japan" {
		if strings.TrimSpace(configuration.DisplayRevalidateURL) == "" {
			return fmt.Errorf("DISPLAY_REVALIDATE_URL is required for the japan worker")
		}
		if len(strings.TrimSpace(configuration.CacheHMACSecret)) < 32 {
			return fmt.Errorf("CACHE_HMAC_SECRET must contain at least 32 characters")
		}
		if strings.TrimSpace(configuration.MediaDeleteURL) == "" {
			return fmt.Errorf("MEDIA_DELETE_URL is required for the japan worker")
		}
		if len(strings.TrimSpace(configuration.MediaHMACSecret)) < 32 {
			return fmt.Errorf("MEDIA_HMAC_SECRET must contain at least 32 characters")
		}
	}
	if configuration.PollInterval <= 0 || configuration.PollInterval > time.Minute {
		return fmt.Errorf("WORKER_POLL_INTERVAL must be between 1ns and 1m")
	}
	if configuration.LeaseDuration < 5*time.Second || configuration.LeaseDuration > 10*time.Minute {
		return fmt.Errorf("WORKER_LEASE_DURATION must be between 5s and 10m")
	}
	if configuration.HistoryArchiveEnabled {
		if configuration.Region != "japan" {
			return fmt.Errorf("history archive is only supported by the japan worker")
		}
		if strings.TrimSpace(configuration.HistoryArchiveDir) == "" {
			return fmt.Errorf("HISTORY_ARCHIVE_DIR is required when history archive is enabled")
		}
		if configuration.HistoryArchiveEvery < time.Minute || configuration.HistoryArchiveEvery > 7*24*time.Hour {
			return fmt.Errorf("HISTORY_ARCHIVE_INTERVAL must be between 1m and 168h")
		}
		if configuration.HistoryArchiveAge < 24*time.Hour {
			return fmt.Errorf("HISTORY_ARCHIVE_MIN_AGE must be at least 24h")
		}
		if configuration.HistoryArchiveBatch < 1 || configuration.HistoryArchiveBatch > 10000 {
			return fmt.Errorf("HISTORY_ARCHIVE_BATCH_SIZE must be between 1 and 10000")
		}
	}
	if configuration.MediaReconcileEnabled {
		if configuration.Region != "japan" {
			return fmt.Errorf("media reconciliation is only supported by the japan worker")
		}
		if strings.TrimSpace(configuration.MediaReconcileURL) == "" {
			return fmt.Errorf("MEDIA_RECONCILE_URL is required when media reconciliation is enabled")
		}
		if configuration.MediaReconcileEvery < time.Hour || configuration.MediaReconcileEvery > 7*24*time.Hour {
			return fmt.Errorf("MEDIA_RECONCILE_INTERVAL must be between 1h and 168h")
		}
	}
	if configuration.MediaInspectEnabled {
		if configuration.Region != "japan" {
			return fmt.Errorf("media inspection is only supported by the japan worker")
		}
		if _, err := mediainspect.NewHTTPDefaults(configuration.MediaInspectDisplayOrigin, nil); err != nil {
			return err
		}
	}
	return nil
}

func capabilitiesFor(region string) []string {
	switch region {
	case "japan":
		return []string{"ingest", "publish", "email", "metrics", "archive", "media_reconcile", "media_inspect", "media_usage"}
	case "beijing":
		return []string{"ingest", "media", "backup"}
	default:
		return nil
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
