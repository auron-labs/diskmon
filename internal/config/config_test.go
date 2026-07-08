package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestDefaultWebListenUsesLoopback(t *testing.T) {
	if got := Default().WebListen; got != "127.0.0.1:8976" {
		t.Fatalf("expected loopback default, got %q", got)
	}
}

func TestOptionalString(t *testing.T) {
	if got := optionalString("   "); got != nil {
		t.Fatalf("expected nil for whitespace, got %v", *got)
	}
	got := optionalString("  */5 * * * *  ")
	if got == nil || *got != "*/5 * * * *" {
		t.Fatalf("expected trimmed cron string, got %v", got)
	}
}

func TestValidate(t *testing.T) {
	validShort := "*/5 * * * *"
	validLong := "0 2 * * *"

	cfg := Default()
	cfg.Tests.Short = &validShort
	cfg.Tests.Long = &validLong
	cfg.Notifications = []NotificationConfig{
		{
			Name:    "http-main",
			Enabled: true,
			Reasons: NotificationReasonConfig{Pass: true, Fail: true},
			HTTP:    &NotificationHTTPConfig{URL: "https://example.com"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	cfg2 := Default()
	cfg2.Database = ""
	if err := cfg2.Validate(); err == nil {
		t.Fatal("expected error for empty database")
	}

	cfg3 := Default()
	cfg3.Interval = 0
	if err := cfg3.Validate(); err == nil {
		t.Fatal("expected error for non-positive interval")
	}

	cfg4 := Default()
	cfg4.WebListen = ""
	if err := cfg4.Validate(); err == nil {
		t.Fatal("expected error for empty web listen")
	}

	cfg5 := Default()
	bad := "not-a-cron"
	cfg5.Tests.Short = &bad
	if err := cfg5.Validate(); err == nil {
		t.Fatal("expected error for invalid short cron")
	}
}

func TestValidateNotifications(t *testing.T) {
	t.Run("duplicate names", func(t *testing.T) {
		cfg := Default()
		cfg.Notifications = []NotificationConfig{
			{
				Name:    "dup",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
				HTTP:    &NotificationHTTPConfig{URL: "https://one.example.com"},
			},
			{
				Name:    "dup",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
				HTTP:    &NotificationHTTPConfig{URL: "https://two.example.com"},
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must be unique") {
			t.Fatalf("expected duplicate name error, got %v", err)
		}
	})

	t.Run("missing provider", func(t *testing.T) {
		cfg := Default()
		cfg.Notifications = []NotificationConfig{
			{
				Name:    "missing-provider",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must configure one provider") {
			t.Fatalf("expected missing provider error, got %v", err)
		}
	})

	t.Run("multiple providers", func(t *testing.T) {
		cfg := Default()
		cfg.Notifications = []NotificationConfig{
			{
				Name:    "multi",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
				HTTP:    &NotificationHTTPConfig{URL: "https://example.com"},
				Slack: &NotificationSlackConfig{
					WebhookURL: "https://hooks.slack.com/services/T000/B000/XXX",
				},
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one provider") {
			t.Fatalf("expected multiple provider error, got %v", err)
		}
	})

	t.Run("slack sdk webhook mutual exclusivity", func(t *testing.T) {
		cfg := Default()
		cfg.Notifications = []NotificationConfig{
			{
				Name:    "slack-both",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
				Slack: &NotificationSlackConfig{
					WebhookURL: "https://hooks.slack.com/services/T000/B000/XXX",
					BotToken:   "xoxb-token",
					ChannelID:  "C123",
				},
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "either webhook_url or sdk mode") {
			t.Fatalf("expected slack mode exclusivity error, got %v", err)
		}
	})

	t.Run("discord sdk requires both fields", func(t *testing.T) {
		cfg := Default()
		cfg.Notifications = []NotificationConfig{
			{
				Name:    "discord-missing",
				Enabled: true,
				Reasons: NotificationReasonConfig{Pass: true, Fail: true},
				Discord: &NotificationDiscordConfig{
					BotToken: "discord-token",
				},
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "requires webhook_url or sdk mode") {
			t.Fatalf("expected discord required fields error, got %v", err)
		}
	})
}

func TestLoadFromPathNotificationsYAMLAndDefaults(t *testing.T) {
	t.Setenv("DISKMON_NOTIFICATIONS", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "diskmon.yaml")
	content := `database: /tmp/diskmon.duckdb
notifications:
  - name: http-defaults
    http:
      url: https://example.com/notify
  - name: slack-webhook
    enabled: false
    reasons:
      pass: false
    slack:
      webhook_url: https://hooks.slack.com/services/T000/B000/XXX
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath failed: %v", err)
	}

	if len(cfg.Notifications) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(cfg.Notifications))
	}

	first := cfg.Notifications[0]
	if !first.Enabled {
		t.Fatal("expected enabled default to true")
	}
	if !first.Reasons.Pass || !first.Reasons.Fail {
		t.Fatalf("expected reasons defaults true,true got %+v", first.Reasons)
	}
	if first.HTTP == nil || first.HTTP.URL != "https://example.com/notify" {
		t.Fatalf("expected http url parsed, got %+v", first.HTTP)
	}

	second := cfg.Notifications[1]
	if second.Enabled {
		t.Fatal("expected explicit enabled=false")
	}
	if second.Reasons.Pass {
		t.Fatal("expected pass=false from yaml")
	}
	if !second.Reasons.Fail {
		t.Fatal("expected fail default true when omitted")
	}
}

func TestLoadFromPathWebListenDefaultsAndOverrides(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("DISKMON_WEB_LISTEN", "")
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd failed: %v", err)
		}
		t.Cleanup(func() {
			if chdirErr := os.Chdir(cwd); chdirErr != nil {
				t.Fatalf("restore cwd: %v", chdirErr)
			}
		})
		if err := os.Chdir(t.TempDir()); err != nil {
			t.Fatalf("Chdir failed: %v", err)
		}

		cfg, err := LoadFromPath("")
		if err != nil {
			t.Fatalf("LoadFromPath failed: %v", err)
		}
		if cfg.WebListen != "127.0.0.1:8976" {
			t.Fatalf("expected loopback default, got %q", cfg.WebListen)
		}
	})

	t.Run("yaml override", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "diskmon.yaml")
		content := []byte("web:\n  listen: 0.0.0.0:8976\n")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		cfg, err := LoadFromPath(path)
		if err != nil {
			t.Fatalf("LoadFromPath failed: %v", err)
		}
		if cfg.WebListen != "0.0.0.0:8976" {
			t.Fatalf("expected yaml override, got %q", cfg.WebListen)
		}
	})

	t.Run("env override", func(t *testing.T) {
		t.Setenv("DISKMON_WEB_LISTEN", "0.0.0.0:8976")
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd failed: %v", err)
		}
		t.Cleanup(func() {
			if chdirErr := os.Chdir(cwd); chdirErr != nil {
				t.Fatalf("restore cwd: %v", chdirErr)
			}
		})
		if err := os.Chdir(t.TempDir()); err != nil {
			t.Fatalf("Chdir failed: %v", err)
		}

		cfg, err := LoadFromPath("")
		if err != nil {
			t.Fatalf("LoadFromPath failed: %v", err)
		}
		if cfg.WebListen != "0.0.0.0:8976" {
			t.Fatalf("expected env override, got %q", cfg.WebListen)
		}
	})
}

func TestLoadFromPathNotificationsFromEnvJSON(t *testing.T) {
	t.Setenv("DISKMON_NOTIFICATIONS", `[{"name":"discord-env","discord":{"bot_token":"token","channel_id":"123"}}]`)

	cfg, err := LoadFromPath("")
	if err != nil {
		t.Fatalf("LoadFromPath failed: %v", err)
	}

	if len(cfg.Notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(cfg.Notifications))
	}
	n := cfg.Notifications[0]
	if n.Name != "discord-env" {
		t.Fatalf("expected env name, got %q", n.Name)
	}
	if !n.Enabled || !n.Reasons.Pass || !n.Reasons.Fail {
		t.Fatalf("expected defaults for enabled and reasons, got enabled=%v reasons=%+v", n.Enabled, n.Reasons)
	}
	if n.Discord == nil || n.Discord.BotToken != "token" || n.Discord.ChannelID != "123" {
		t.Fatalf("expected discord sdk fields parsed, got %+v", n.Discord)
	}
}

func TestApplyFlagOverrides(t *testing.T) {
	cfg := Default()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("config", cfg.ConfigPath, "")
	flags.String("db", cfg.Database, "")
	flags.Duration("interval", cfg.Interval, "")
	flags.String("web-listen", cfg.WebListen, "")
	flags.StringSlice("drives", cfg.Drives, "")
	flags.String("log-level", cfg.LogLevel, "")

	if err := flags.Set("db", "/tmp/test.duckdb"); err != nil {
		t.Fatalf("set db flag: %v", err)
	}
	if err := flags.Set("interval", "30s"); err != nil {
		t.Fatalf("set interval flag: %v", err)
	}
	if err := flags.Set("web-listen", "0.0.0.0:8976"); err != nil {
		t.Fatalf("set web-listen flag: %v", err)
	}
	if err := flags.Set("drives", "/dev/sda,/dev/sdb"); err != nil {
		t.Fatalf("set drives flag: %v", err)
	}

	ApplyFlagOverrides(cfg, flags)

	if cfg.Database != "/tmp/test.duckdb" {
		t.Fatalf("database override not applied: %q", cfg.Database)
	}
	if cfg.Interval != 30*time.Second {
		t.Fatalf("interval override not applied: %v", cfg.Interval)
	}
	if cfg.WebListen != "0.0.0.0:8976" {
		t.Fatalf("web listen override not applied: %q", cfg.WebListen)
	}
	if len(cfg.Drives) != 2 {
		t.Fatalf("drives override not applied: %v", cfg.Drives)
	}
}
