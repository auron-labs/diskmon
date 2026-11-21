package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"diskmon/internal/config"
	"diskmon/internal/storage"

	"github.com/spf13/cobra"
)

func newDatabaseCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "database", Short: "Offline database maintenance"}
	var dryRun, compact bool
	prune := &cobra.Command{
		Use:   "prune",
		Short: "Apply configured tier retention to the database",
		Long:  "Zero disables an individual retention tier. Prune requires at least one enabled tier; --compact may still be used when all tiers are disabled.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if dryRun && compact {
				return fmt.Errorf("--dry-run cannot be combined with --compact")
			}
			if err := validatePruneRetention(cfg, dryRun, compact); err != nil {
				return err
			}
			db, err := storage.OpenDuckDB(cfg.Database)
			if err != nil {
				return err
			}
			defer db.Close()
			policy := storage.TierPolicy{RawRetention: cfg.RawRetention, HourlyRetention: cfg.HourlyRetention, DailyRetention: cfg.DailyRetention, DryRun: dryRun}
			if dryRun {
				result, err := db.TierMaintenance(ctx, time.Now().UTC(), policy)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Eligible (up to 1000 per tier/bucket): raw=%d hourly=%d daily=%d pending_remaining=%d; counts may be capped.\n", result.RawSamplesRemaining, result.HourlyRowsRemaining, result.DailyRowsRemaining, result.PendingRowsRemaining)
				return nil
			}
			for {
				result, err := db.TierMaintenance(ctx, time.Now().UTC(), policy)
				if err != nil {
					return err
				}
				work := result.RawSamplesRolled + result.HourlyRowsRolled + result.DailyRowsDeleted + result.PendingSamplesDeleted
				remaining := result.RawSamplesRemaining + result.HourlyRowsRemaining + result.DailyRowsRemaining + result.PendingRowsRemaining
				fmt.Fprintf(cmd.OutOrStdout(), "Pruned raw=%d hourly=%d daily=%d pending_deleted=%d remaining=%d\n", result.RawSamplesRolled, result.HourlyRowsRolled, result.DailyRowsDeleted, result.PendingSamplesDeleted, remaining)
				if remaining == 0 {
					break
				}
				if work == 0 {
					return fmt.Errorf("pruning made no progress with %d eligible rows remaining", remaining)
				}
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				return err
			}
			_, checkpointErr := conn.ExecContext(ctx, "CHECKPOINT")
			closeErr := conn.Close()
			if checkpointErr != nil {
				return fmt.Errorf("checkpoint database: %w", checkpointErr)
			}
			if closeErr != nil {
				return closeErr
			}
			if err := db.Close(); err != nil {
				return err
			}
			if compact {
				before, err := os.Stat(cfg.Database)
				if err != nil {
					return fmt.Errorf("stat database before compaction: %w", err)
				}
				if err := compactDatabase(ctx, cfg.Database); err != nil {
					return err
				}
				after, err := os.Stat(cfg.Database)
				if err != nil {
					return fmt.Errorf("stat database after compaction: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Compacted database size: %d -> %d bytes (reclaimed %d bytes)\n", before.Size(), after.Size(), before.Size()-after.Size())
			}
			return nil
		},
	}
	prune.Flags().BoolVar(&dryRun, "dry-run", false, "report eligible rows without pruning")
	prune.Flags().BoolVar(&compact, "compact", false, "rewrite the database to reclaim space; allowed alone when all tiers are disabled, not with --dry-run")
	cmd.AddCommand(prune)
	return cmd
}

func validatePruneRetention(cfg *config.Config, dryRun, compact bool) error {
	if cfg.RawRetention < 0 || cfg.HourlyRetention < 0 || cfg.DailyRetention < 0 {
		return fmt.Errorf("database tier retentions cannot be negative")
	}
	if cfg.RawRetention == 0 && cfg.HourlyRetention == 0 && cfg.DailyRetention == 0 && !dryRun && !compact {
		return fmt.Errorf("all database retention tiers are disabled; nothing to prune (use --compact to compact without pruning)")
	}
	return nil
}

// compactDatabase builds and validates a sibling copy before atomically
// replacing the original. The original remains at path until the rename.
func compactDatabase(ctx context.Context, path string) (retErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".diskmon-compact-*.duckdb")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("prepare compact destination: %w", err)
	}
	if err := copyDuckDB(ctx, path, tmpPath); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve database permissions: %w", err)
	}
	compactedFile, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open compacted database for sync: %w", err)
	}
	syncErr := compactedFile.Sync()
	closeErr := compactedFile.Close()
	if syncErr != nil {
		return fmt.Errorf("sync compacted database: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close compacted database after sync: %w", closeErr)
	}
	// On the supported POSIX platforms, rename atomically replaces path. The
	// verified original therefore remains available until this single swap.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("atomically replace database with compacted copy: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("compacted database installed at %q, but failed syncing containing directory %q: %w", path, dir, err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func copyDuckDB(ctx context.Context, source, target string) error {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	quotedSource := strings.ReplaceAll(source, "'", "''")
	if _, err := db.ExecContext(ctx, "ATTACH '"+quotedSource+"' AS compact_source"); err != nil {
		return fmt.Errorf("attach compact source: %w", err)
	}
	quotedTarget := strings.ReplaceAll(target, "'", "''")
	if _, err := db.ExecContext(ctx, "ATTACH '"+quotedTarget+"' AS compact_target"); err != nil {
		return fmt.Errorf("attach compact destination: %w", err)
	}
	if _, err := db.ExecContext(ctx, "COPY FROM DATABASE compact_source TO compact_target"); err != nil {
		if _, attachErr := db.ExecContext(ctx, "DETACH compact_target"); attachErr != nil {
			return fmt.Errorf("copy database: %w (detach: %v)", err, attachErr)
		}
		return fmt.Errorf("copy database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "DETACH compact_target"); err != nil {
		return fmt.Errorf("detach compact destination: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close compact copy connection: %w", err)
	}
	return verifyDatabaseCopy(ctx, source, target)
}

func verifyDatabaseCopy(ctx context.Context, source, target string) error {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	for alias, path := range map[string]string{"verify_source": source, "verify_target": target} {
		quoted := strings.ReplaceAll(path, "'", "''")
		if _, err := db.ExecContext(ctx, "ATTACH '"+quoted+"' AS "+alias); err != nil {
			return fmt.Errorf("attach database for verification: %w", err)
		}
	}
	for _, item := range []struct{ catalog, nameColumn string }{{"duckdb_tables", "table_name"}, {"duckdb_indexes", "index_name"}, {"duckdb_sequences", "sequence_name"}} {
		catalog, nameColumn := item.catalog, item.nameColumn
		query := "SELECT schema_name, " + nameColumn + ", sql FROM " + catalog + "() WHERE database_name=? ORDER BY schema_name, " + nameColumn
		sourceObjects, err := catalogObjects(ctx, db, query, "verify_source")
		if err != nil {
			return fmt.Errorf("inspect source %s: %w", catalog, err)
		}
		targetObjects, err := catalogObjects(ctx, db, query, "verify_target")
		if err != nil {
			return fmt.Errorf("inspect compacted %s: %w", catalog, err)
		}
		if strings.Join(sourceObjects, "\n") != strings.Join(targetObjects, "\n") {
			return fmt.Errorf("compacted database %s differ from source", catalog)
		}
		if catalog == "duckdb_tables" {
			for _, object := range sourceObjects {
				parts := strings.SplitN(object, "\x00", 3)
				if len(parts) < 2 {
					continue
				}
				var sourceCount int64
				for _, alias := range []string{"verify_source", "verify_target"} {
					var count int64
					qualified := alias + "." + quoteIdentifier(parts[0]) + "." + quoteIdentifier(parts[1])
					if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil {
						return fmt.Errorf("count %s table %s: %w", alias, parts[1], err)
					}
					if alias == "verify_source" {
						sourceCount = count
					} else if count != sourceCount {
						return fmt.Errorf("row count mismatch for %s: source=%d compacted=%d", parts[1], sourceCount, count)
					}
				}
			}
		}
	}
	return nil
}

func catalogObjects(ctx context.Context, db *sql.DB, query, database string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []string
	for rows.Next() {
		var schema, name string
		var definition sql.NullString
		if err := rows.Scan(&schema, &name, &definition); err != nil {
			return nil, err
		}
		objects = append(objects, schema+"\x00"+name+"\x00"+definition.String)
	}
	return objects, rows.Err()
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
