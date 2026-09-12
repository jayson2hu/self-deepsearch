package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"self-deepsearch/services/platform-api/internal/config"
	"self-deepsearch/services/platform-api/internal/database"
	"self-deepsearch/services/platform-api/internal/httpapi"
	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/logsafe"
	"self-deepsearch/services/platform-api/internal/security"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		os.Exit(runHealthcheck("HEALTHCHECK_URL", "http://127.0.0.1:8080/readyz"))
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	configuration := config.FromEnv()
	if configuration.AppEnv == "production" {
		if err := configuration.ValidateProduction(); err != nil {
			logger.Error("production_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store *database.Store
	if configuration.DatabaseURL != "" {
		var err error
		store, err = database.Open(ctx, configuration.DatabaseURL)
		if err != nil {
			logger.Error("database_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
		defer store.Close()
	}

	var identityService *identity.Service
	if store != nil && configuration.AuthHMACSecret != "" && configuration.SMTPURL != "" {
		emailSender, err := identity.NewSMTPSender(configuration.SMTPURL)
		if err != nil {
			logger.Error("smtp_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
		if configuration.AppEnv == "production" && emailSender.AllowsPlaintext() {
			logger.Error("smtp_config_invalid", "error", "plaintext SMTP cannot be enabled in production")
			os.Exit(1)
		}
		identityService, err = identity.NewServiceWithEmailPolicy(store, emailSender, configuration.AuthHMACSecret, identity.EmailDeliveryPolicy{
			DailyLimit:       configuration.EmailDailyLimit,
			FailureThreshold: configuration.EmailFailureThreshold,
			CircuitCooldown:  configuration.EmailCooldown,
			SendTimeout:      configuration.EmailSendTimeout,
		}, nil)
		if err != nil {
			logger.Error("identity_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
	} else {
		logger.Warn("identity_disabled", "reason", "database, AUTH_HMAC_SECRET, or SMTP_URL is missing")
	}

	var humanVerifier security.HumanVerifier
	if configuration.TurnstileBypass {
		humanVerifier = security.DevelopmentVerifier{}
	} else if configuration.TurnstileSecret != "" {
		humanVerifier = security.NewTurnstile(configuration.TurnstileSecret, configuration.TurnstileExpectedHostname)
	}

	server := &http.Server{
		Addr: configuration.Addr,
		Handler: httpapi.New(httpapi.Options{
			Service:                  "platform-api",
			Version:                  configuration.BuildVersion,
			Database:                 store,
			Catalog:                  store,
			Account:                  store,
			Identity:                 identityService,
			Operations:               store,
			HumanVerifier:            humanVerifier,
			AllowedOrigins:           configuration.AllowedOrigins,
			SecureCookies:            configuration.AppEnv == "production",
			TrustProxyHeaders:        configuration.TrustProxyHeaders,
			Logger:                   logger,
			MetricsToken:             configuration.MetricsToken,
			EmailMetrics:             store,
			EmailDailyLimit:          configuration.EmailDailyLimit,
			MediaPublicBaseURL:       configuration.MediaPublicBaseURL,
			MediaDeliveryMode:        configuration.MediaDeliveryMode,
			MediaDeliveryDynamicMode: configuration.MediaDeliveryDynamicMode,
			MediaEdgePolicyMode:      configuration.MediaEdgePolicyMode,
			MediaEdgePolicyToken:     configuration.MediaEdgePolicyToken,
			RequireRecentAuth:        identityService != nil,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server_started", "service", "platform-api", "addr", configuration.Addr, "version", configuration.BuildVersion)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error_class", logsafe.ErrorClass(err))
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server_shutdown_failed", "error_class", logsafe.ErrorClass(err))
	}
}

func runHealthcheck(environmentKey, fallbackURL string) int {
	url := os.Getenv(environmentKey)
	if url == "" {
		url = fallbackURL
	}
	client := http.Client{Timeout: 2 * time.Second}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 1
	}
	response, err := client.Do(request)
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
