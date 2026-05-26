package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server     ServerConfig
	Credential CredentialConfig
	Session    SessionConfig
	Lingma     LingmaConfig
	Account    AccountConfig
	Logging    LoggingConfig
	Database   DatabaseConfig
	Redis      RedisConfig
}

type ServerConfig struct {
	Host       string
	Port       int
	AdminToken string
}

type CredentialConfig struct {
	AuthFile string
}

type SessionConfig struct {
	TTLMinutes  int
	MaxSessions int
}

type LingmaConfig struct {
	BaseURL           string
	CosyVersion       string
	Transport         string
	OAuthListenAddr   string
	OAuthCallbackAddr string
}

type AccountConfig struct {
	RoutingMode          string
	LoadBalance          string
	ChinaBaseURL         string
	InternationalBaseURL string
}

type LoggingConfig struct {
	StoreExecutionLogs bool
}

type DatabaseConfig struct {
	Driver string
	DSN    string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Credential: CredentialConfig{
			AuthFile: "./auth/credentials.json",
		},
		Session: SessionConfig{
			TTLMinutes:  30,
			MaxSessions: 100,
		},
		Lingma: LingmaConfig{
			BaseURL:           "https://lingma.alibabacloud.com",
			CosyVersion:       "2.11.2",
			Transport:         "curl",
			OAuthListenAddr:   "127.0.0.1:37510",
			OAuthCallbackAddr: "127.0.0.1:37510",
		},
		Account: AccountConfig{
			RoutingMode:          "mixed",
			LoadBalance:          "round_robin",
			ChinaBaseURL:         "https://lingma.alibabacloud.com",
			InternationalBaseURL: "https://api.lingma.ai",
		},
		Logging: LoggingConfig{
			StoreExecutionLogs: true,
		},
		Database: DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgres://lingma:lingma@localhost:5432/lingma2api?sslmode=disable",
		},
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	raw := string(data)
	hasAccountChinaBaseURL, err := hasYAMLKey(raw, "account", "china_base_url")
	if err != nil {
		return Config{}, err
	}
	if err := applyYAML(&cfg, raw); err != nil {
		return Config{}, err
	}
	if !hasAccountChinaBaseURL {
		cfg.Account.ChinaBaseURL = cfg.Lingma.BaseURL
	}
	if err := validateAccountConfig(cfg.Account); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyYAML(cfg *Config, raw string) error {
	section := ""
	for index, rawLine := range strings.Split(raw, "\n") {
		line := stripComment(strings.TrimRight(rawLine, "\r"))
		if strings.TrimSpace(line) == "" {
			continue
		}

		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if !strings.HasSuffix(trimmed, ":") {
				return fmt.Errorf("line %d: invalid section header", index+1)
			}
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if section == "" || indent < 2 {
			return fmt.Errorf("line %d: nested value without section", index+1)
		}

		key, value, err := splitKeyValue(trimmed)
		if err != nil {
			return fmt.Errorf("line %d: %w", index+1, err)
		}
		if err := assignValue(cfg, section, key, value); err != nil {
			return fmt.Errorf("line %d: %w", index+1, err)
		}
	}

	return nil
}

func hasYAMLKey(raw, targetSection, targetKey string) (bool, error) {
	section := ""
	for index, rawLine := range strings.Split(raw, "\n") {
		line := stripComment(strings.TrimRight(rawLine, "\r"))
		if strings.TrimSpace(line) == "" {
			continue
		}

		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if !strings.HasSuffix(trimmed, ":") {
				return false, fmt.Errorf("line %d: invalid section header", index+1)
			}
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if section == "" || indent < 2 {
			return false, fmt.Errorf("line %d: nested value without section", index+1)
		}

		key, _, err := splitKeyValue(trimmed)
		if err != nil {
			return false, fmt.Errorf("line %d: %w", index+1, err)
		}
		if section == targetSection && key == targetKey {
			return true, nil
		}
	}

	return false, nil
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func splitKeyValue(line string) (string, string, error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key/value pair")
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"'`)
	return key, value, nil
}

func assignValue(cfg *Config, section, key, value string) error {
	switch section {
	case "server":
		return assignServerValue(&cfg.Server, key, value)
	case "credential":
		return assignCredentialValue(&cfg.Credential, key, value)
	case "session":
		return assignSessionValue(&cfg.Session, key, value)
	case "lingma":
		return assignLingmaValue(&cfg.Lingma, key, value)
	case "account":
		return assignAccountValue(&cfg.Account, key, value)
	case "logging":
		return assignLoggingValue(&cfg.Logging, key, value)
	case "database":
		return assignDatabaseValue(&cfg.Database, key, value)
	case "redis":
		return assignRedisValue(&cfg.Redis, key, value)
	default:
		return fmt.Errorf("unknown section %q", section)
	}
}

func assignServerValue(cfg *ServerConfig, key, value string) error {
	switch key {
	case "host":
		cfg.Host = value
	case "port":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Port = parsed
	case "admin_token":
		cfg.AdminToken = value
	default:
		return fmt.Errorf("unknown server key %q", key)
	}
	return nil
}

func assignCredentialValue(cfg *CredentialConfig, key, value string) error {
	switch key {
	case "auth_file":
		cfg.AuthFile = value
	default:
		return fmt.Errorf("unknown credential key %q", key)
	}
	return nil
}

func assignSessionValue(cfg *SessionConfig, key, value string) error {
	switch key {
	case "ttl_minutes":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.TTLMinutes = parsed
	case "max_sessions":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxSessions = parsed
	default:
		return fmt.Errorf("unknown session key %q", key)
	}
	return nil
}

func assignLingmaValue(cfg *LingmaConfig, key, value string) error {
	switch key {
	case "base_url":
		cfg.BaseURL = value
	case "cosy_version":
		cfg.CosyVersion = value
	case "transport":
		cfg.Transport = value
	case "client_id":
		// Deprecated: Lingma's real auth flow does not use a standard OAuth
		// client_id (the server injects it in the 302 chain). Silently
		// ignored for backward compatibility with existing config.yaml files.
		_ = value
	case "oauth_listen_addr":
		cfg.OAuthListenAddr = value
		if cfg.OAuthCallbackAddr == "" {
			cfg.OAuthCallbackAddr = value
		}
	case "oauth_callback_addr":
		cfg.OAuthCallbackAddr = value
	default:
		return fmt.Errorf("unknown lingma key %q", key)
	}
	return nil
}

func assignAccountValue(cfg *AccountConfig, key, value string) error {
	switch key {
	case "routing_mode":
		cfg.RoutingMode = value
	case "load_balance":
		cfg.LoadBalance = value
	case "china_base_url":
		cfg.ChinaBaseURL = value
	case "international_base_url":
		cfg.InternationalBaseURL = value
	default:
		return fmt.Errorf("unknown account key %q", key)
	}
	return nil
}

func assignLoggingValue(cfg *LoggingConfig, key, value string) error {
	switch key {
	case "store_execution_logs":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.StoreExecutionLogs = parsed
	default:
		return fmt.Errorf("unknown logging key %q", key)
	}
	return nil
}

func assignDatabaseValue(cfg *DatabaseConfig, key, value string) error {
	switch key {
	case "driver":
		cfg.Driver = value
	case "dsn":
		cfg.DSN = value
	default:
		return fmt.Errorf("unknown database key %q", key)
	}
	return nil
}

func assignRedisValue(cfg *RedisConfig, key, value string) error {
	switch key {
	case "addr":
		cfg.Addr = value
	case "password":
		cfg.Password = value
	case "db":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.DB = parsed
	default:
		return fmt.Errorf("unknown redis key %q", key)
	}
	return nil
}

func validateAccountConfig(cfg AccountConfig) error {
	switch cfg.RoutingMode {
	case "china_only", "international_only", "mixed":
	default:
		return fmt.Errorf("unknown account routing_mode %q", cfg.RoutingMode)
	}
	switch cfg.LoadBalance {
	case "round_robin":
	default:
		return fmt.Errorf("unknown account load_balance %q", cfg.LoadBalance)
	}
	return nil
}
