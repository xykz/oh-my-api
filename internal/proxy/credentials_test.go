package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

func TestCredentialManagerReadsProjectCredentialFile(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 1,
		"source":         "project_bootstrap",
		"obtained_at":    "2026-04-27T11:30:00+08:00",
		"updated_at":     "2026-04-27T11:30:00+08:00",
		"auth": map[string]string{
			"cosy_key":          "sentinel-key",
			"encrypt_user_info": "sentinel-info",
			"user_id":           "u-123",
			"machine_id":        "m-123",
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	snapshot, err := manager.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}

	if snapshot.Source != "project_bootstrap" {
		t.Fatalf("expected project source, got %q", snapshot.Source)
	}
	if snapshot.CosyKey != "sentinel-key" {
		t.Fatalf("expected cosy key from project file, got %q", snapshot.CosyKey)
	}
	if snapshot.MachineID != "m-123" {
		t.Fatalf("expected machine id from project file, got %q", snapshot.MachineID)
	}
}

func TestCredentialManagerRejectsMissingProjectCredentialFields(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 1,
		"source":         "project_bootstrap",
		"auth": map[string]string{
			"cosy_key": "sentinel-key",
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	if _, err := manager.Current(context.Background()); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCredentialManagerReadsSchemaV2FirstEnabledChinaAccount(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 2,
		"source":         "schema-v2-file",
		"accounts": []map[string]any{
			{
				"id":      "disabled-china",
				"region":  "china",
				"enabled": false,
				"auth": map[string]string{
					"cosy_key":          "disabled-key",
					"encrypt_user_info": "disabled-info",
					"user_id":           "disabled-user",
					"machine_id":        "disabled-machine",
				},
			},
			{
				"id":      "intl",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
			{
				"id":                "enabled-china",
				"region":            "china",
				"enabled":           true,
				"source":            "china-account",
				"token_expire_time": "1770000000000",
				"auth": map[string]string{
					"cosy_key":          "enabled-key",
					"encrypt_user_info": "enabled-info",
					"user_id":           "enabled-user",
					"machine_id":        "enabled-machine",
				},
			},
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	snapshot, err := manager.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}

	if snapshot.CosyKey != "enabled-key" {
		t.Fatalf("expected first enabled China account cosy key, got %q", snapshot.CosyKey)
	}
	if snapshot.UserID != "enabled-user" {
		t.Fatalf("expected first enabled China account user ID, got %q", snapshot.UserID)
	}
	if snapshot.Source != "china-account" {
		t.Fatalf("expected account source, got %q", snapshot.Source)
	}
	if snapshot.TokenExpireTime != 1770000000000 {
		t.Fatalf("expected account token expiration, got %d", snapshot.TokenExpireTime)
	}
}

func TestCredentialManagerRejectsSchemaV2DuplicateIDs(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "duplicate",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "enabled-key",
					"encrypt_user_info": "enabled-info",
					"user_id":           "enabled-user",
					"machine_id":        "enabled-machine",
				},
			},
			{
				"id":      "duplicate",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	if _, err := manager.Current(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestCredentialManagerRejectsUnselectedInvalidSchemaV2Account(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "disabled-invalid-china",
				"region":  "china",
				"enabled": false,
				"auth": map[string]string{
					"cosy_key":   "cosy-disabled",
					"user_id":    "u-disabled",
					"machine_id": "m-disabled",
				},
			},
			{
				"id":      "enabled-china",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "enabled-key",
					"encrypt_user_info": "enabled-info",
					"user_id":           "enabled-user",
					"machine_id":        "enabled-machine",
				},
			},
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	if _, err := manager.Current(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestCredentialManagerStoredMetaUsesSelectedSchemaV2ChinaAccount(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version":      2,
		"source":              "file-source",
		"lingma_version_hint": "file-version",
		"obtained_at":         "2026-04-27T11:30:00+08:00",
		"updated_at":          "2026-04-27T11:30:00+08:00",
		"token_expire_time":   "1770000000000",
		"accounts": []map[string]any{
			{
				"id":      "disabled-china",
				"region":  "china",
				"enabled": false,
				"auth": map[string]string{
					"cosy_key":          "disabled-key",
					"encrypt_user_info": "disabled-info",
					"user_id":           "disabled-user",
					"machine_id":        "disabled-machine",
				},
			},
			{
				"id":                  "enabled-china",
				"region":              "china",
				"enabled":             true,
				"source":              "account-source",
				"lingma_version_hint": "account-version",
				"obtained_at":         "2026-04-28T11:30:00+08:00",
				"updated_at":          "2026-04-28T12:30:00+08:00",
				"token_expire_time":   "1770003600000",
				"auth": map[string]string{
					"cosy_key":          "enabled-key",
					"encrypt_user_info": "enabled-info",
					"user_id":           "enabled-user",
					"machine_id":        "enabled-machine",
				},
			},
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	meta := manager.StoredMeta()

	if meta.SchemaVersion != 2 {
		t.Fatalf("expected schema version 2, got %d", meta.SchemaVersion)
	}
	if meta.Source != "account-source" {
		t.Fatalf("expected selected account source, got %q", meta.Source)
	}
	if meta.LingmaVersionHint != "account-version" {
		t.Fatalf("expected selected account version hint, got %q", meta.LingmaVersionHint)
	}
	if meta.ObtainedAt != "2026-04-28T11:30:00+08:00" {
		t.Fatalf("expected selected account obtained_at, got %q", meta.ObtainedAt)
	}
	if meta.UpdatedAt != "2026-04-28T12:30:00+08:00" {
		t.Fatalf("expected selected account updated_at, got %q", meta.UpdatedAt)
	}
	if meta.TokenExpireTime != "1770003600000" {
		t.Fatalf("expected selected account token expiration, got %q", meta.TokenExpireTime)
	}
}

func TestCredentialManagerHasOAuthUsesSelectedSchemaV2ChinaAccount(t *testing.T) {
	path := writeCredentialFile(t, map[string]any{
		"schema_version": 2,
		"oauth": map[string]string{
			"access_token": "file-access",
		},
		"accounts": []map[string]any{
			{
				"id":      "disabled-china",
				"region":  "china",
				"enabled": false,
				"oauth": map[string]string{
					"access_token":  "disabled-access",
					"refresh_token": "disabled-refresh",
				},
				"auth": map[string]string{
					"cosy_key":          "disabled-key",
					"encrypt_user_info": "disabled-info",
					"user_id":           "disabled-user",
					"machine_id":        "disabled-machine",
				},
			},
			{
				"id":      "enabled-china",
				"region":  "china",
				"enabled": true,
				"oauth": map[string]string{
					"access_token":  "enabled-access",
					"refresh_token": "enabled-refresh",
				},
				"auth": map[string]string{
					"cosy_key":          "enabled-key",
					"encrypt_user_info": "enabled-info",
					"user_id":           "enabled-user",
					"machine_id":        "enabled-machine",
				},
			},
		},
	})

	manager := NewCredentialManager(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	hasAccessToken, hasRefreshToken := manager.HasOAuth()

	if !hasAccessToken {
		t.Fatal("expected selected account access token presence")
	}
	if !hasRefreshToken {
		t.Fatal("expected selected account refresh token presence")
	}
}

func writeCredentialFile(t *testing.T, payload map[string]any) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
