package cli

import (
	"context"
	"fmt"
	"log/slog"

	"diskmon/internal/config"
	"diskmon/internal/health"
	"diskmon/internal/smart"
	"diskmon/internal/storage"

	"github.com/spf13/cobra"
)

func newScanCmd(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Collect one SMART sample for all configured drives",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.Validate(); err != nil {
				return err
			}
			ctx := context.Background()
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

			results, err := collector.CollectAll(ctx, drives)
			if err != nil {
				return err
			}

			stored := 0
			for _, res := range results {
				healthResult := evaluator.Evaluate(res.Sample)
				if _, _, err := db.InsertSample(ctx, res.Info, res.Sample, healthResult); err != nil {
					logger.Error("failed to insert sample", "device", res.Info.Device, "error", err)
					continue
				}
				stored++
			}

			fmt.Printf("stored %d sample(s)\n", stored)
			return nil
		},
	}
}
