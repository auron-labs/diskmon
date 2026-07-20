package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"diskmon/internal/api"
	"diskmon/internal/config"
	"diskmon/internal/health"
	"diskmon/internal/notification"
	"diskmon/internal/smart"
	"diskmon/internal/storage"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

func newDaemonCmd(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run diskmon daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			drives, err := resolveDrives(ctx, cfg.Drives, logger)
			if err != nil {
				return err
			}

			db, err := storage.OpenDuckDB(cfg.Database)
			if err != nil {
				return err
			}
			defer db.Close()

			collector := smart.NewCollector(smart.NewExecRunner(), logger)
			evaluator := health.NewEvaluator(health.DefaultRules())
			events := api.NewEventBroker()

			markedRuns, err := db.MarkIncompleteSmartTestRuns(ctx, time.Now().UTC())
			if err != nil {
				logger.Error("failed marking incomplete SMART test runs", "error", err)
			} else if markedRuns > 0 {
				logger.Info("marked incomplete SMART test runs", "updated", markedRuns)
			}

			notificationTargets, err := buildNotificationTargets(cfg)
			if err != nil {
				return err
			}

			apiServer := api.NewServer(cfg.WebListen, cfg.WebAPIKey, logger, db, events)
			errCh := make(chan error, 1)
			go func() {
				if err := apiServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()

			cronScheduler, err := configureSmartTestCron(ctx, cfg, drives, collector, db, logger, events)
			if err != nil {
				return err
			}
			if cronScheduler != nil {
				cronScheduler.Start()
				defer cronScheduler.Stop()
			}

			ticker := time.NewTicker(cfg.Interval)
			defer ticker.Stop()

			// Prune on an hourly cadence (or every cycle if the interval
			// is longer than an hour). A retention of zero disables pruning.
			pruneInterval := time.Hour
			if cfg.Interval > pruneInterval {
				pruneInterval = cfg.Interval
			}
			pruneTicker := time.NewTicker(pruneInterval)
			defer pruneTicker.Stop()

			runCollection := func() {
				runCollectionCycle(ctx, drives, collector, evaluator, db, events, notificationTargets, logger)
			}
			runPrune := func() {
				if cfg.Retention <= 0 {
					return
				}
				deleted, err := db.PruneSamples(ctx, cfg.Retention, time.Now().UTC())
				if err != nil {
					logger.Error("failed pruning old samples", "error", err)
					return
				}
				if deleted > 0 {
					logger.Info("pruned old samples", "deleted", deleted, "retention", cfg.Retention)
				}
			}

			runCollection()
			runPrune()
			for {
				select {
				case <-ctx.Done():
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := apiServer.Shutdown(shutdownCtx); err != nil {
						if errors.Is(err, context.DeadlineExceeded) {
							logger.Warn("graceful shutdown timed out; forcing close")
							_ = apiServer.Close()
							return nil
						}
						return err
					}
					return nil
				case err := <-errCh:
					return err
				case <-ticker.C:
					runCollection()
				case <-pruneTicker.C:
					runPrune()
				}
			}
		},
	}
}

type sampleCollector interface {
	CollectAll(ctx context.Context, devices []string) ([]smart.CollectResult, error)
}

type healthEvaluator interface {
	Evaluate(sample smart.SmartSample) health.Result
}

type daemonStorage interface {
	InsertSample(ctx context.Context, info smart.DriveInfo, sample smart.SmartSample, result health.Result) (sampleID int64, driveID int64, err error)
	GetNotificationState(ctx context.Context, driveID int64, notificationName string) (*storage.NotificationState, error)
	UpsertNotificationState(ctx context.Context, driveID int64, notificationName string, state string, updatedAt time.Time) error
	PruneSamples(ctx context.Context, retention time.Duration, now time.Time) (int64, error)
}

type eventPublisher interface {
	Publish(eventType string, device string)
}

type notificationDispatcher interface {
	DispatchIfNeeded(ctx context.Context, req notification.DispatchRequest) (notification.DispatchResult, error)
}

type notificationTarget struct {
	name       string
	dispatcher notificationDispatcher
}

func runCollectionCycle(
	ctx context.Context,
	drives []string,
	collector sampleCollector,
	evaluator healthEvaluator,
	db daemonStorage,
	events eventPublisher,
	targets []notificationTarget,
	logger *slog.Logger,
) {
	results, err := collector.CollectAll(ctx, drives)
	if err != nil {
		logger.Error("collection failed", "error", err)
		return
	}

	for _, res := range results {
		healthResult := evaluator.Evaluate(res.Sample)
		_, driveID, err := db.InsertSample(ctx, res.Info, res.Sample, healthResult)
		if err != nil {
			logger.Error("failed storing sample", "device", res.Info.Device, "error", err)
			continue
		}

		events.Publish("sample.inserted", res.Info.Device)

		if len(targets) == 0 {
			continue
		}

		updatedAt := res.Sample.CollectedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now().UTC()
		}

		dispatchNotificationsForDrive(ctx, db, targets, res.Info.Device, driveID, healthResult, updatedAt, logger)
	}
}

// dispatchNotificationsForDrive fans out notification dispatch across the
// configured targets concurrently. Dedupe state persistence happens after
// each target's dispatch completes; concurrent targets do not share state
// because each (driveID, notification_name) pair is independent.
func dispatchNotificationsForDrive(
	ctx context.Context,
	db daemonStorage,
	targets []notificationTarget,
	device string,
	driveID int64,
	healthResult health.Result,
	updatedAt time.Time,
	logger *slog.Logger,
) {
	var wg sync.WaitGroup
	for _, target := range targets {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			dispatchSingleTarget(ctx, db, target, device, driveID, healthResult, updatedAt, logger)
		}()
	}
	wg.Wait()
}

func dispatchSingleTarget(
	ctx context.Context,
	db daemonStorage,
	target notificationTarget,
	device string,
	driveID int64,
	healthResult health.Result,
	updatedAt time.Time,
	logger *slog.Logger,
) {
	var previousStatus *health.Status
	previousState, err := db.GetNotificationState(ctx, driveID, target.name)
	if err != nil {
		logger.Error("failed loading notification dedupe state", "device", device, "notification", target.name, "error", err)
		return
	}
	if previousState != nil {
		previousStatus = parseStoredHealthStatus(previousState.State)
	}

	dispatchResult, err := target.dispatcher.DispatchIfNeeded(ctx, notification.DispatchRequest{
		DriveID:        device,
		PreviousStatus: previousStatus,
		Current:        healthResult,
	})
	if err != nil {
		logger.Error("notification dispatch failed", "device", device, "notification", target.name, "error", err)
	}

	for _, outcome := range dispatchResult.Outcomes {
		if outcome.Attempted && !outcome.Sent {
			continue
		}
		if err := db.UpsertNotificationState(ctx, driveID, outcome.Name, string(healthResult.Status), updatedAt); err != nil {
			logger.Error("failed persisting notification dedupe state", "device", device, "notification", outcome.Name, "error", err)
		}
	}
}

func parseStoredHealthStatus(value string) *health.Status {
	status := health.Status(strings.ToUpper(strings.TrimSpace(value)))
	switch status {
	case health.StatusGreen, health.StatusYellow, health.StatusRed, health.StatusUnknown:
		return &status
	default:
		return nil
	}
}

// isStaleSelfTestResult reports whether the candidate self-test log entry
// matches the baseline captured before the test was started. A match means
// the log has not yet been updated with the new run's result.
//
// When both entries carry a power_on_time.hours value, a strictly greater
// candidate power-on-hours is enough to consider the entry fresh (the drive
// has accumulated hours since the baseline was captured). Otherwise we fall
// back to comparing status and message strings.
func isStaleSelfTestResult(candidateStatus, candidateMsg string, candidatePowerOnHours *int64, baselineStatus, baselineMsg string, baselinePowerOnHours *int64) bool {
	if candidatePowerOnHours != nil && baselinePowerOnHours != nil {
		if *candidatePowerOnHours > *baselinePowerOnHours {
			return false
		}
		if *candidatePowerOnHours < *baselinePowerOnHours {
			return true
		}
	}
	return candidateStatus == baselineStatus && candidateMsg == baselineMsg
}

// collectDevicesWithTestInProgress queries the live self-test log for each
// device and returns the list of devices that currently report a test in
// progress. These devices should be skipped by MarkIncompleteSmartTestRuns
// so a genuinely running test is not falsely marked incomplete on restart.
func collectDevicesWithTestInProgress(ctx context.Context, devices []string, collector *smart.Collector, logger *slog.Logger) []string {
	var inProgress []string
	for _, device := range devices {
		status, _, _ := collector.ReadSelfTestResultWithPowerOnHours(ctx, device, "short")
		if status == "IN_PROGRESS" {
			inProgress = append(inProgress, device)
			continue
		}
		if status, _, _ := collector.ReadSelfTestResultWithPowerOnHours(ctx, device, "long"); status == "IN_PROGRESS" {
			inProgress = append(inProgress, device)
		}
	}
	if len(inProgress) > 0 {
		logger.Info("skipping incomplete-marking for drives with self-test in progress", "devices", inProgress)
	}
	return inProgress
}

func buildNotificationTargets(cfg *config.Config) ([]notificationTarget, error) {
	if len(cfg.Notifications) == 0 {
		return nil, nil
	}

	targets := make([]notificationTarget, 0, len(cfg.Notifications))
	for _, cfgEntry := range cfg.Notifications {
		entry := configNotificationToEntry(cfgEntry)
		dispatcher, err := notification.NewDispatcher([]notification.Entry{entry}, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("build notification dispatcher %q: %w", entry.Name, err)
		}
		targets = append(targets, notificationTarget{
			name:       entry.Name,
			dispatcher: dispatcher,
		})
	}
	return targets, nil
}

func configNotificationToEntry(in config.NotificationConfig) notification.Entry {
	entry := notification.Entry{
		Name:    in.Name,
		Enabled: in.Enabled,
		OnPass:  in.Reasons.Pass,
		OnFail:  in.Reasons.Fail,
	}

	switch {
	case in.HTTP != nil:
		entry.Provider.Type = notification.ProviderHTTP
		entry.Provider.HTTP.URL = in.HTTP.URL
	case in.Slack != nil:
		entry.Provider.Type = notification.ProviderSlack
		if in.Slack.WebhookURL != "" {
			entry.Provider.Slack.Mode = notification.ModeWebhook
			entry.Provider.Slack.WebhookURL = in.Slack.WebhookURL
		} else {
			entry.Provider.Slack.Mode = notification.ModeSDK
			entry.Provider.Slack.APIToken = in.Slack.BotToken
			if in.Slack.ChannelID != "" {
				entry.Provider.Slack.ChannelIDs = []string{in.Slack.ChannelID}
			}
		}
	case in.Discord != nil:
		entry.Provider.Type = notification.ProviderDiscord
		if in.Discord.WebhookURL != "" {
			entry.Provider.Discord.Mode = notification.ModeWebhook
			entry.Provider.Discord.WebhookURL = in.Discord.WebhookURL
		} else {
			entry.Provider.Discord.Mode = notification.ModeSDK
			entry.Provider.Discord.BotToken = in.Discord.BotToken
			if in.Discord.ChannelID != "" {
				entry.Provider.Discord.ChannelIDs = []string{in.Discord.ChannelID}
			}
		}
	}

	return entry
}

func configureSmartTestCron(
	ctx context.Context,
	cfg *config.Config,
	drives []string,
	collector *smart.Collector,
	db *storage.DuckDB,
	logger *slog.Logger,
	events *api.EventBroker,
) (*cron.Cron, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	scheduler := cron.New(cron.WithParser(parser))
	enabled := false
	inFlight := make(map[string]bool)
	var inFlightMu sync.Mutex

	addJob := func(testType string, expr *string) error {
		if expr == nil {
			return nil
		}
		testType = strings.ToLower(strings.TrimSpace(testType))
		spec := strings.TrimSpace(*expr)
		if _, err := scheduler.AddFunc(spec, func() {
			scheduledAt := time.Now().UTC()
			for _, device := range drives {
				select {
				case <-ctx.Done():
					return
				default:
				}
				testKey := device + ":" + testType
				inFlightMu.Lock()
				if inFlight[testKey] {
					inFlightMu.Unlock()
					logger.Warn("skipping scheduled SMART test; previous run still in progress", "device", device, "type", testType)
					continue
				}
				inFlight[testKey] = true
				inFlightMu.Unlock()

				func() {
					defer func() {
						inFlightMu.Lock()
						delete(inFlight, testKey)
						inFlightMu.Unlock()
					}()

					baselineStatus, baselineMsg, baselinePowerOnHours := collector.ReadSelfTestResultWithPowerOnHours(ctx, device, testType)
					if baselineStatus == "IN_PROGRESS" {
						logger.Warn("skipping scheduled SMART test; device reports test already in progress", "device", device, "type", testType)
						return
					}

					startedAt := time.Now().UTC()
					output, runErr := collector.RunSelfTest(ctx, device, testType)

					var finishedAt *time.Time
					status := "STARTED"
					message := output
					if runErr != nil {
						status = "FAILED"
						message = runErr.Error()
						now := time.Now().UTC()
						finishedAt = &now
						logger.Error("scheduled SMART test failed", "device", device, "type", testType, "error", runErr)
					} else {
						logger.Info("scheduled SMART test triggered", "device", device, "type", testType)
					}

					if _, err := db.InsertSmartTestRun(ctx, smart.DriveInfo{Device: device}, storage.SmartTestRun{
						TestType:    testType,
						ScheduledAt: scheduledAt,
						StartedAt:   startedAt,
						FinishedAt:  finishedAt,
						Status:      status,
						Message:     message,
					}); err != nil {
						logger.Error("failed storing SMART test run", "device", device, "type", testType, "error", err)
					} else {
						events.Publish("test.updated", device)
					}

					if runErr != nil {
						return
					}

					waitFor := collector.ParseSelfTestWait(output)
					if waitFor > 0 {
						waitFor += 10 * time.Second
						select {
						case <-ctx.Done():
							return
						case <-time.After(waitFor):
						}
					}

					finalStatus := "UNKNOWN"
					finalMsg := "self-test result unavailable"
					for i := 0; i < 12; i++ {
						candidateStatus, candidateMsg, candidatePowerOnHours := collector.ReadSelfTestResultWithPowerOnHours(ctx, device, testType)
						if candidateStatus == "IN_PROGRESS" {
							// Test is still running, keep polling.
						} else if isStaleSelfTestResult(candidateStatus, candidateMsg, candidatePowerOnHours, baselineStatus, baselineMsg, baselinePowerOnHours) {
							// Result unchanged from before we started the test;
							// likely a stale entry from a previous run.
							logger.Debug("self-test result unchanged from baseline, still waiting", "device", device, "type", testType, "status", candidateStatus)
						} else {
							finalStatus = candidateStatus
							finalMsg = candidateMsg
							break
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(20 * time.Second):
						}
					}

					finalFinishedAt := time.Now().UTC()
					if _, err := db.InsertSmartTestRun(ctx, smart.DriveInfo{Device: device}, storage.SmartTestRun{
						TestType:    testType,
						ScheduledAt: scheduledAt,
						StartedAt:   startedAt,
						FinishedAt:  &finalFinishedAt,
						Status:      finalStatus,
						Message:     finalMsg,
					}); err != nil {
						logger.Error("failed storing SMART final test result", "device", device, "type", testType, "error", err)
						return
					} else {
						events.Publish("test.updated", device)
					}
				}()
			}
		}); err != nil {
			return fmt.Errorf("configure collector.tests.%s: %w", testType, err)
		}
		logger.Info("scheduled SMART test enabled", "type", testType, "cron", spec)
		enabled = true
		return nil
	}

	if err := addJob("short", cfg.Tests.Short); err != nil {
		return nil, err
	}
	if err := addJob("long", cfg.Tests.Long); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	return scheduler, nil
}
