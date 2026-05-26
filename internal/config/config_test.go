package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigOverridesDefaults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  host: "0.0.0.0"
  port: 9090
credential:
  auth_file: "./auth/credentials.json"
session:
  ttl_minutes: 5
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("expected host override, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Session.TTLMinutes != 5 {
		t.Fatalf("expected ttl 5, got %d", cfg.Session.TTLMinutes)
	}
	if cfg.Credential.AuthFile != "./auth/credentials.json" {
		t.Fatalf("expected auth file override, got %q", cfg.Credential.AuthFile)
	}
}

func TestLoadConfigAccountDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Account.RoutingMode != "mixed" {
		t.Fatalf("routing mode = %q, want mixed", cfg.Account.RoutingMode)
	}
	if cfg.Account.LoadBalance != "round_robin" {
		t.Fatalf("load balance = %q, want round_robin", cfg.Account.LoadBalance)
	}
	if cfg.Account.ChinaBaseURL != cfg.Lingma.BaseURL {
		t.Fatalf("china base url = %q, want lingma base url %q", cfg.Account.ChinaBaseURL, cfg.Lingma.BaseURL)
	}
	if cfg.Account.InternationalBaseURL != "https://api.lingma.ai" {
		t.Fatalf("international base url = %q", cfg.Account.InternationalBaseURL)
	}
}

func TestLoadConfigAccountOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
account:
  routing_mode: "china_only"
  load_balance: "round_robin"
  china_base_url: "https://api.lingma.cn"
  international_base_url: "https://api.lingma.ai"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Account.RoutingMode != "china_only" {
		t.Fatalf("routing mode = %q", cfg.Account.RoutingMode)
	}
	if cfg.Account.LoadBalance != "round_robin" {
		t.Fatalf("load balance = %q", cfg.Account.LoadBalance)
	}
	if cfg.Account.ChinaBaseURL != "https://api.lingma.cn" {
		t.Fatalf("china base url = %q", cfg.Account.ChinaBaseURL)
	}
	if cfg.Account.InternationalBaseURL != "https://api.lingma.ai" {
		t.Fatalf("international base url = %q", cfg.Account.InternationalBaseURL)
	}
}

func TestLoadConfigAccountChinaBaseURLFollowsLingmaBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
lingma:
  base_url: "https://custom.example"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Account.ChinaBaseURL != "https://custom.example" {
		t.Fatalf("china base url = %q", cfg.Account.ChinaBaseURL)
	}
}

func TestLoadConfigRejectsInvalidAccountRoutingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
account:
  routing_mode: "region_magic"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown account routing_mode") {
		t.Fatalf("expected routing mode error, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidAccountLoadBalance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
account:
  load_balance: "least_busy"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown account load_balance") {
		t.Fatalf("expected load balance error, got %v", err)
	}
}

func TestLoadConfigRejectsUnknownAccountKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
account:
  region_magic: "enabled"
`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `unknown account key "region_magic"`) {
		t.Fatalf("expected unknown account key error, got %v", err)
	}
}
