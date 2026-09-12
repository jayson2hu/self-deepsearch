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

	"self-deepsearch/services/platform-worker/internal/config"
	"self-deepsearch/services/platform-worker/internal/health"
	"self-deepsearch/services/platform-worker/internal/historyarchive"
	"self-deepsearch/services/platform-worker/internal/logsafe"
	"self-deepsearch/services/platform-worker/internal/mediainspect"
	"self-deepsearch/services/platform-worker/internal/mediareconcile"
	"self-deepsearch/services/platform-worker/internal/mediauploadadmission"
	"self-deepsearch/services/platform-worker/internal/mediauploadcontrol"
	"self-deepsearch/services/platform-worker/internal/mediausage"
	"self-deepsearch/services/platform-worker/internal/outbox"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "media-upload-control" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := mediauploadcontrol.RunCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		stop()
		os.Exit(code)
	}
	if len(os.Args) > 1 && os.Args[1] == "media-usage" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := mediausage.RunCommand(ctx, os.Args[2:], os.Stdout, os.Stderr)
		stop()
		os.Exit(code)
	}
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		os.Exit(runHealthcheck("HEALTHCHECK_URL", "http://127.0.0.1:8081/readyz"))
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configuration := config.FromEnv()
	if err := configuration.Validate(); err != nil {
		logger.Error("worker_config_invalid", "error_class", logsafe.ErrorClass(err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workerTelemetry := health.NewTracker(time.Now())

	var outboxRepository *outbox.PostgresRepository
	var usageMetrics health.UsageMetricsProvider
	var reconcileAdmission *mediausage.TaskGuard
	var uploadControlMetrics health.UsageMetricsProvider
	var uploadResumeGuard mediauploadcontrol.ResumeGuard
	var uploadAdmission *mediauploadadmission.Handler
	if configuration.Region == "japan" {
		var err error
		outboxRepository, err = outbox.OpenPostgres(ctx, configuration.DatabaseURL)
		if err != nil {
			logger.Error("worker_database_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
		defer outboxRepository.Close()
		if configuration.MediaUsageMode == "observe" {
			usageRepository, err := mediausage.NewPostgresRepository(outboxRepository.Pool(), configuration.MediaUsageSchedule)
			if err != nil {
				logger.Error("media_usage_config_invalid")
				os.Exit(1)
			}
			usageClient, err := mediausage.NewClient(configuration.MediaUsageSchedule.AccountID, configuration.MediaUsageToken)
			if err != nil {
				logger.Error("media_usage_config_invalid")
				os.Exit(1)
			}
			usageMetrics = usageRepository
			if configuration.MediaUploadAdmissionMode == "enforce" {
				guard, err := mediausage.NewTaskGuard(usageRepository, configuration.MediaUsageSchedule, nil)
				if err != nil {
					logger.Error("media_upload_admission_guard_invalid")
					os.Exit(1)
				}
				uploadAdmission, err = mediauploadadmission.NewHandler(configuration.MediaUploadAdmissionSecret, guard, nil)
				if err != nil {
					logger.Error("media_upload_admission_handler_invalid")
					os.Exit(1)
				}
			}
			if configuration.MediaUploadQueueMode == "dispatch" {
				guard, err := mediausage.NewTaskGuard(usageRepository, configuration.MediaUsageSchedule, nil)
				if err != nil {
					logger.Error("media_upload_resume_guard_invalid")
					os.Exit(1)
				}
				uploadResumeGuard = guard
			}
			if configuration.MediaReconcileUsageGuard == "enforce" {
				reconcileAdmission, err = mediausage.NewTaskGuard(usageRepository, configuration.MediaUsageSchedule, nil)
				if err != nil {
					logger.Error("media_reconciliation_usage_guard_invalid")
					os.Exit(1)
				}
			}
			go func() {
				runner := mediausage.Runner{Repository: usageRepository, Reviews: usageRepository, Client: usageClient, Logger: logger}
				if err := runner.Run(ctx); err != nil {
					logger.Error("media_usage_runner_stopped", "error_class", logsafe.ErrorClass(err))
					stop()
				}
			}()
		}
		revalidationProcessor, err := outbox.NewRevalidationProcessor(configuration.DisplayRevalidateURL, configuration.CacheHMACSecret, nil)
		if err != nil {
			logger.Error("cache_revalidation_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
		mediaDeletionProcessor, err := outbox.NewMediaDeletionProcessor(configuration.MediaDeleteURL, configuration.MediaHMACSecret, nil)
		if err != nil {
			logger.Error("media_deletion_config_invalid", "error_class", logsafe.ErrorClass(err))
			os.Exit(1)
		}
		go func() {
			dispatcher := outbox.Dispatcher{
				Repository: outboxRepository, WorkerID: configuration.WorkerID,
				PollEvery: configuration.PollInterval, LeaseFor: configuration.LeaseDuration,
				BatchSize: 2, Processor: outbox.RoutingProcessor{Revalidation: revalidationProcessor, MediaDelete: mediaDeletionProcessor}, Logger: logger,
				Telemetry: workerTelemetry,
			}
			if err := dispatcher.Run(ctx); err != nil {
				logger.Error("outbox_dispatcher_stopped", "error_class", logsafe.ErrorClass(err))
				stop()
			}
		}()
		if configuration.HistoryArchiveEnabled {
			archiveRepository, err := historyarchive.NewPostgresRepository(outboxRepository.Pool(), configuration.HistoryArchiveDir)
			if err != nil {
				logger.Error("history_archive_config_invalid", "error_class", logsafe.ErrorClass(err))
				os.Exit(1)
			}
			go func() {
				runner := historyarchive.Runner{
					Repository: archiveRepository, Every: configuration.HistoryArchiveEvery,
					MinimumAge: configuration.HistoryArchiveAge, BatchSize: configuration.HistoryArchiveBatch,
					Logger: logger,
				}
				if err := runner.Run(ctx); err != nil {
					logger.Error("history_archive_runner_stopped", "error_class", logsafe.ErrorClass(err))
					stop()
				}
			}()
		}
		if configuration.MediaUploadQueueMode == "dispatch" {
			client, err := mediauploadcontrol.NewClient(configuration.MediaUploadControlURL, configuration.MediaUploadControlSecret, configuration.MediaUploadAllowHTTP, nil)
			if err != nil {
				logger.Error("media_upload_queue_config_invalid")
				os.Exit(1)
			}
			queue, err := mediauploadcontrol.NewPostgresQueue(outboxRepository.Pool(), client.TargetRef())
			if err != nil {
				logger.Error("media_upload_queue_repository_invalid")
				os.Exit(1)
			}
			uploadControlMetrics = queue
			go func() {
				runner := mediauploadcontrol.QueueRunner{Repository: queue, Client: client, ResumeGuard: uploadResumeGuard, Logger: logger}
				if err := runner.Run(ctx); err != nil {
					logger.Error("media_upload_queue_stopped")
					stop()
				}
			}()
		}
		if configuration.MediaInspectEnabled {
			inspectionRepository, err := mediainspect.NewPostgresRepository(outboxRepository.Pool())
			if err != nil {
				logger.Error("media_inspection_config_invalid", "error_class", logsafe.ErrorClass(err))
				os.Exit(1)
			}
			defaults, err := mediainspect.NewHTTPDefaults(configuration.MediaInspectDisplayOrigin, nil)
			if err != nil {
				logger.Error("media_inspection_config_invalid", "error_class", logsafe.ErrorClass(err))
				os.Exit(1)
			}
			go func() {
				runner := mediainspect.Runner{Repository: inspectionRepository, Defaults: defaults, Logger: logger}
				if err := runner.Run(ctx); err != nil {
					logger.Error("media_inspection_runner_stopped", "error_class", logsafe.ErrorClass(err))
					stop()
				}
			}()
		}
		if configuration.MediaReconcileEnabled {
			reconcileRepository, err := mediareconcile.NewPostgresRepository(outboxRepository.Pool())
			if err != nil {
				logger.Error("media_reconciliation_config_invalid", "error_class", logsafe.ErrorClass(err))
				os.Exit(1)
			}
			reconcileClient, err := mediareconcile.NewHTTPClient(configuration.MediaReconcileURL, configuration.MediaHMACSecret, nil)
			if err != nil {
				logger.Error("media_reconciliation_config_invalid", "error_class", logsafe.ErrorClass(err))
				os.Exit(1)
			}
			go func() {
				runner := mediareconcile.Runner{Repository: reconcileRepository, Client: reconcileClient, Every: configuration.MediaReconcileEvery, Logger: logger}
				if reconcileAdmission != nil {
					runner.Admission = reconcileAdmission
				}
				if err := runner.Run(ctx); err != nil {
					logger.Error("media_reconciliation_runner_stopped", "error_class", logsafe.ErrorClass(err))
					stop()
				}
			}()
		}
	}

	var reconcileGuardMetrics health.UsageMetricsProvider
	var uploadAdmissionHTTP http.Handler
	var uploadAdmissionMetrics health.UsageMetricsProvider
	if uploadAdmission != nil {
		uploadAdmissionHTTP, uploadAdmissionMetrics = uploadAdmission, uploadAdmission
	}
	if reconcileAdmission != nil {
		reconcileGuardMetrics = reconcileAdmission
	}
	server := &http.Server{
		Addr: configuration.Addr,
		Handler: health.Handler{
			Service:                "platform-worker",
			Version:                configuration.BuildVersion,
			Region:                 configuration.Region,
			Capabilities:           configuration.Capabilities,
			Checker:                outboxRepository,
			RequireChecker:         configuration.Region == "japan",
			MetricsToken:           configuration.MetricsToken,
			Telemetry:              workerTelemetry,
			RequireTelemetry:       configuration.Region == "japan",
			HeartbeatMaxAge:        workerHeartbeatMaxAge(configuration.PollInterval),
			UsageMetrics:           usageMetrics,
			ReconcileGuardMetrics:  reconcileGuardMetrics,
			UploadControlMetrics:   uploadControlMetrics,
			UploadAdmissionHTTP:    uploadAdmissionHTTP,
			UploadAdmissionMetrics: uploadAdmissionMetrics,
		}.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("worker_started",
			"region", configuration.Region,
			"capabilities", configuration.Capabilities,
			"addr", configuration.Addr,
			"version", configuration.BuildVersion,
			"collection_enabled", configuration.CollectionEnabled,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker_server_failed", "error_class", logsafe.ErrorClass(err))
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("worker_shutdown_failed", "error_class", logsafe.ErrorClass(err))
	}
}

func workerHeartbeatMaxAge(pollInterval time.Duration) time.Duration {
	maxAge := 3 * pollInterval
	if maxAge < 30*time.Second {
		return 30 * time.Second
	}
	return maxAge
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
