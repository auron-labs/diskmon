package cli

import (
	"log/slog"
	"strings"

	"diskmon/internal/config"
	"diskmon/internal/util"

	"github.com/spf13/cobra"
)

func NewRootCmd(cfg *config.Config, logger *slog.Logger, levelVar *slog.LevelVar) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diskmon",
		Short: "Disk health monitoring daemon and CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			loaded, err := config.LoadFromPath(configPath)
			if err != nil {
				return err
			}
			config.ApplyFlagOverrides(loaded, cmd.Flags())
			loaded.Drives = normalizeDrives(loaded.Drives)
			level, err := util.ParseLogLevel(loaded.LogLevel)
			if err != nil {
				return err
			}
			loaded.LogLevel = strings.ToUpper(loaded.LogLevel)
			if err := loaded.Validate(); err != nil {
				return err
			}
			if levelVar != nil {
				levelVar.Set(level)
			}
			*cfg = *loaded
			logger.Debug("config loaded", "database", cfg.Database, "listen", cfg.WebListen, "drives", len(cfg.Drives))
			return nil
		},
	}

	cmd.PersistentFlags().String("config", cfg.ConfigPath, "path to config yaml")
	cmd.PersistentFlags().String("db", cfg.Database, "duckdb database path")
	cmd.PersistentFlags().Duration("interval", cfg.Interval, "collection interval")
	cmd.PersistentFlags().Duration("retention", cfg.Retention, "sample retention duration (0 disables pruning)")
	cmd.PersistentFlags().String("web-listen", cfg.WebListen, "web listen address")
	cmd.PersistentFlags().String("web-api-key", cfg.WebAPIKey, "API key required for /api/v1 requests (empty disables auth)")
	cmd.PersistentFlags().StringSlice("drives", cfg.Drives, "comma separated device list")
	cmd.PersistentFlags().String("log-level", cfg.LogLevel, "log level")

	cmd.AddCommand(newDaemonCmd(cfg, logger))
	cmd.AddCommand(newScanCmd(cfg, logger))
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newConfigCmd(cfg))

	return cmd
}

func normalizeDrives(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return values
	}
	return out
}
