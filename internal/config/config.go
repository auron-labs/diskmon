package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/robfig/cron/v3"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

type Tests struct {
	Short *string
	Long  *string
}

type Config struct {
	ConfigPath    string
	Database      string
	Interval      time.Duration
	Retention     time.Duration
	Drives        []string
	Tests         Tests
	WebListen     string
	WebAPIKey     string
	LogLevel      string
	Notifications []NotificationConfig
}

type NotificationConfig struct {
	Name    string                     `mapstructure:"name" yaml:"name"`
	Enabled bool                       `mapstructure:"enabled" yaml:"enabled"`
	Reasons NotificationReasonConfig   `mapstructure:"reasons" yaml:"reasons"`
	HTTP    *NotificationHTTPConfig    `mapstructure:"http" yaml:"http"`
	Slack   *NotificationSlackConfig   `mapstructure:"slack" yaml:"slack"`
	Discord *NotificationDiscordConfig `mapstructure:"discord" yaml:"discord"`
}

type NotificationReasonConfig struct {
	Pass bool `mapstructure:"pass" yaml:"pass"`
	Fail bool `mapstructure:"fail" yaml:"fail"`
}

type NotificationHTTPConfig struct {
	URL string `mapstructure:"url" yaml:"url"`
}

type NotificationSlackConfig struct {
	WebhookURL string `mapstructure:"webhook_url" yaml:"webhook_url"`
	BotToken   string `mapstructure:"bot_token" yaml:"bot_token"`
	ChannelID  string `mapstructure:"channel_id" yaml:"channel_id"`
}

type NotificationDiscordConfig struct {
	WebhookURL string `mapstructure:"webhook_url" yaml:"webhook_url"`
	BotToken   string `mapstructure:"bot_token" yaml:"bot_token"`
	ChannelID  string `mapstructure:"channel_id" yaml:"channel_id"`
}

type rawNotificationConfig struct {
	Name    string                      `mapstructure:"name"`
	Enabled *bool                       `mapstructure:"enabled"`
	Reasons rawNotificationReasonConfig `mapstructure:"reasons"`
	HTTP    *NotificationHTTPConfig     `mapstructure:"http"`
	Slack   *NotificationSlackConfig    `mapstructure:"slack"`
	Discord *NotificationDiscordConfig  `mapstructure:"discord"`
}

type rawNotificationReasonConfig struct {
	Pass *bool `mapstructure:"pass"`
	Fail *bool `mapstructure:"fail"`
}

func Default() *Config {
	return &Config{
		ConfigPath:    "",
		Database:      "diskmon.duckdb",
		Interval:      60 * time.Second,
		Retention:     0,
		Drives:        []string{},
		Tests:         Tests{},
		WebListen:     "127.0.0.1:8976",
		LogLevel:      "INFO",
		Notifications: []NotificationConfig{},
	}
}

func Load() (*Config, error) {
	return LoadFromPath("")
}

func LoadFromPath(path string) (*Config, error) {
	cfg := Default()

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("DISKMON")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	v.SetDefault("database", cfg.Database)
	v.SetDefault("collector.interval", cfg.Interval)
	v.SetDefault("storage.retention", cfg.Retention)
	v.SetDefault("collector.drives", cfg.Drives)
	v.SetDefault("collector.tests.short", "")
	v.SetDefault("collector.tests.long", "")
	v.SetDefault("web.listen", cfg.WebListen)
	v.SetDefault("web.api_key", "")
	v.SetDefault("log.level", cfg.LogLevel)
	v.SetDefault("notifications", []map[string]any{})

	_ = v.BindEnv("database", "DISKMON_DATABASE")
	_ = v.BindEnv("collector.interval", "DISKMON_INTERVAL")
	_ = v.BindEnv("storage.retention", "DISKMON_RETENTION")
	_ = v.BindEnv("collector.drives", "DISKMON_DRIVES")
	_ = v.BindEnv("collector.tests.short", "DISKMON_TEST_SHORT")
	_ = v.BindEnv("collector.tests.long", "DISKMON_TEST_LONG")
	_ = v.BindEnv("web.listen", "DISKMON_WEB_LISTEN")
	_ = v.BindEnv("web.api_key", "DISKMON_WEB_API_KEY")
	_ = v.BindEnv("log.level", "DISKMON_LOG_LEVEL")
	_ = v.BindEnv("notifications", "DISKMON_NOTIFICATIONS")

	if path != "" {
		cfg.ConfigPath = path
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("diskmon")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	cfg.Database = v.GetString("database")
	cfg.Interval = v.GetDuration("collector.interval")
	cfg.Retention = v.GetDuration("storage.retention")
	cfg.Drives = v.GetStringSlice("collector.drives")
	cfg.Tests = Tests{
		Short: optionalString(v.GetString("collector.tests.short")),
		Long:  optionalString(v.GetString("collector.tests.long")),
	}
	cfg.WebListen = v.GetString("web.listen")
	cfg.WebAPIKey = strings.TrimSpace(v.GetString("web.api_key"))
	cfg.LogLevel = strings.ToUpper(v.GetString("log.level"))
	notifications, err := loadNotifications(v)
	if err != nil {
		return nil, err
	}
	cfg.Notifications = notifications

	return cfg, nil
}

func ApplyFlagOverrides(cfg *Config, flags *pflag.FlagSet) {
	if flags == nil {
		return
	}

	if flags.Changed("config") {
		cfg.ConfigPath, _ = flags.GetString("config")
	}
	if flags.Changed("db") {
		cfg.Database, _ = flags.GetString("db")
	}
	if flags.Changed("interval") {
		cfg.Interval, _ = flags.GetDuration("interval")
	}
	if flags.Changed("retention") {
		cfg.Retention, _ = flags.GetDuration("retention")
	}
	if flags.Changed("web-listen") {
		cfg.WebListen, _ = flags.GetString("web-listen")
	}
	if flags.Changed("web-api-key") {
		cfg.WebAPIKey, _ = flags.GetString("web-api-key")
	}
	if flags.Changed("drives") {
		cfg.Drives, _ = flags.GetStringSlice("drives")
	}
	if flags.Changed("log-level") {
		cfg.LogLevel, _ = flags.GetString("log-level")
	}
}

func (c *Config) Validate() error {
	if c.Database == "" {
		return fmt.Errorf("database path is required")
	}
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	if c.Retention < 0 {
		return fmt.Errorf("storage.retention must not be negative")
	}
	if c.WebListen == "" {
		return fmt.Errorf("web listen address is required")
	}
	if c.Tests.Short != nil {
		if _, err := cron.ParseStandard(*c.Tests.Short); err != nil {
			return fmt.Errorf("collector.tests.short must be a valid cron expression: %w", err)
		}
	}
	if c.Tests.Long != nil {
		if _, err := cron.ParseStandard(*c.Tests.Long); err != nil {
			return fmt.Errorf("collector.tests.long must be a valid cron expression: %w", err)
		}
	}
	if err := c.validateNotifications(); err != nil {
		return err
	}
	return nil
}

func loadNotifications(v *viper.Viper) ([]NotificationConfig, error) {
	raw := v.Get("notifications")
	if raw == nil {
		return []NotificationConfig{}, nil
	}

	if s, ok := raw.(string); ok {
		parsed, err := parseNotificationsString(s)
		if err != nil {
			return nil, err
		}
		raw = parsed
	}

	var parsed []rawNotificationConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &parsed,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return nil, fmt.Errorf("notifications decode setup failed: %w", err)
	}
	if err := decoder.Decode(raw); err != nil {
		return nil, fmt.Errorf("notifications decode failed: %w", err)
	}

	out := make([]NotificationConfig, 0, len(parsed))
	for _, item := range parsed {
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		reasonPass := true
		if item.Reasons.Pass != nil {
			reasonPass = *item.Reasons.Pass
		}
		reasonFail := true
		if item.Reasons.Fail != nil {
			reasonFail = *item.Reasons.Fail
		}

		cfg := NotificationConfig{
			Name:    strings.TrimSpace(item.Name),
			Enabled: enabled,
			Reasons: NotificationReasonConfig{
				Pass: reasonPass,
				Fail: reasonFail,
			},
			HTTP:    sanitizeHTTP(item.HTTP),
			Slack:   sanitizeSlack(item.Slack),
			Discord: sanitizeDiscord(item.Discord),
		}
		out = append(out, cfg)
	}

	return out, nil
}

func parseNotificationsString(value string) (any, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return []any{}, nil
	}

	var raw any
	if err := json.Unmarshal([]byte(s), &raw); err == nil {
		return raw, nil
	}
	if err := yaml.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("notifications must be valid JSON or YAML: %w", err)
	}
	return raw, nil
}

func sanitizeHTTP(in *NotificationHTTPConfig) *NotificationHTTPConfig {
	if in == nil {
		return nil
	}
	return &NotificationHTTPConfig{
		URL: strings.TrimSpace(in.URL),
	}
}

func sanitizeSlack(in *NotificationSlackConfig) *NotificationSlackConfig {
	if in == nil {
		return nil
	}
	return &NotificationSlackConfig{
		WebhookURL: strings.TrimSpace(in.WebhookURL),
		BotToken:   strings.TrimSpace(in.BotToken),
		ChannelID:  strings.TrimSpace(in.ChannelID),
	}
}

func sanitizeDiscord(in *NotificationDiscordConfig) *NotificationDiscordConfig {
	if in == nil {
		return nil
	}
	return &NotificationDiscordConfig{
		WebhookURL: strings.TrimSpace(in.WebhookURL),
		BotToken:   strings.TrimSpace(in.BotToken),
		ChannelID:  strings.TrimSpace(in.ChannelID),
	}
}

func (c *Config) validateNotifications() error {
	seen := make(map[string]struct{}, len(c.Notifications))

	for idx, n := range c.Notifications {
		if n.Name == "" {
			return fmt.Errorf("notifications[%d].name is required", idx)
		}
		if _, exists := seen[n.Name]; exists {
			return fmt.Errorf("notifications[%d].name %q must be unique", idx, n.Name)
		}
		seen[n.Name] = struct{}{}

		providers := 0
		if n.HTTP != nil {
			providers++
			if n.HTTP.URL == "" {
				return fmt.Errorf("notifications[%d].http.url is required", idx)
			}
		}
		if n.Slack != nil {
			providers++
			if err := validateSDKOrWebhook("notifications", idx, "slack", n.Slack.WebhookURL, n.Slack.BotToken, n.Slack.ChannelID); err != nil {
				return err
			}
		}
		if n.Discord != nil {
			providers++
			if err := validateSDKOrWebhook("notifications", idx, "discord", n.Discord.WebhookURL, n.Discord.BotToken, n.Discord.ChannelID); err != nil {
				return err
			}
		}

		if providers == 0 {
			return fmt.Errorf("notifications[%d] must configure one provider: http, slack, or discord", idx)
		}
		if providers > 1 {
			return fmt.Errorf("notifications[%d] must configure exactly one provider", idx)
		}
	}

	return nil
}

func validateSDKOrWebhook(fieldPrefix string, idx int, provider, webhookURL, botToken, channelID string) error {
	hasWebhook := webhookURL != ""
	hasSDKField := botToken != "" || channelID != ""

	if hasWebhook && hasSDKField {
		return fmt.Errorf("%s[%d].%s must use either webhook_url or sdk mode (bot_token + channel_id), not both", fieldPrefix, idx, provider)
	}
	if hasWebhook {
		return nil
	}
	if botToken == "" || channelID == "" {
		return fmt.Errorf("%s[%d].%s requires webhook_url or sdk mode with bot_token and channel_id", fieldPrefix, idx, provider)
	}
	return nil
}

func optionalString(v string) *string {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	return &s
}
