package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

func TestAccountStoreReadsLegacyCredentialAsChinaAccount(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 1,
		"source":         "project_bootstrap",
		"auth": map[string]string{
			"cosy_key":          "cosy-legacy",
			"encrypt_user_info": "info-legacy",
			"user_id":           "u-legacy",
			"machine_id":        "m-legacy",
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].Region != AccountRegionChina {
		t.Fatalf("expected china region, got %q", accounts[0].Region)
	}
	if accounts[0].CosyKey != "cosy-legacy" {
		t.Fatalf("expected cosy key from legacy credential, got %q", accounts[0].CosyKey)
	}
	if !accounts[0].Enabled {
		t.Fatal("expected legacy account to be enabled")
	}
}

func TestAccountStoreReadsSchemaV2Accounts(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "china",
				"label":   "China",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "cosy-china",
					"encrypt_user_info": "info-china",
					"user_id":           "u-china",
					"machine_id":        "m-china",
				},
			},
			{
				"id":      "intl",
				"label":   "International",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if accounts[1].Region != AccountRegionInternational {
		t.Fatalf("expected international region, got %q", accounts[1].Region)
	}
	if accounts[1].AccessToken != "at-intl" {
		t.Fatalf("expected international access token, got %q", accounts[1].AccessToken)
	}
}

func TestAccountStoreSummariesRedactSecrets(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "china",
				"label":   "China",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "secret-cosy",
					"encrypt_user_info": "secret-info",
					"user_id":           "u-china",
					"machine_id":        "m-china",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	summaries, err := store.Summaries(context.Background())
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	assertNotContains(t, string(data), "secret-cosy")
	assertNotContains(t, string(data), "secret-info")
}

func TestAccountStoreGeneratedIDIgnoresLabelAndRefreshMetadata(t *testing.T) {
	firstPath := writeAccountCredentialFile(t, accountCredentialWithoutID("China", "2026-04-27T11:30:00+08:00", "1770000000000"))
	secondPath := writeAccountCredentialFile(t, accountCredentialWithoutID("Renamed China", "2026-04-28T11:30:00+08:00", "1770003600000"))

	firstID := loadSingleAccountID(t, firstPath)
	secondID := loadSingleAccountID(t, secondPath)

	if firstID != secondID {
		t.Fatalf("expected generated IDs to match, got %q and %q", firstID, secondID)
	}
}

func TestAccountStoreRejectsTokenOnlyInternationalAccountWithoutID(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"label":   "International",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	if _, err := store.Accounts(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestAccountStoreRejectsDuplicateExplicitIDs(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "duplicate",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "cosy-one",
					"encrypt_user_info": "info-one",
					"user_id":           "u-one",
					"machine_id":        "m-one",
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

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	if _, err := store.Accounts(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestAccountStoreRejectsDuplicateGeneratedIDs(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"label":   "China One",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "cosy-one",
					"encrypt_user_info": "info-one",
					"user_id":           "u-shared",
					"machine_id":        "m-shared",
				},
			},
			{
				"label":   "China Two",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "cosy-two",
					"encrypt_user_info": "info-two",
					"user_id":           "u-shared",
					"machine_id":        "m-shared",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	if _, err := store.Accounts(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestAccountStoreRejectsDisabledInvalidSchemaV2Account(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
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
					"cosy_key":          "cosy-enabled",
					"encrypt_user_info": "info-enabled",
					"user_id":           "u-enabled",
					"machine_id":        "m-enabled",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	if _, err := store.Accounts(context.Background()); !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
}

func TestAccountStoreUpsertAccountKeepsInternationalAccount(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "intl-1",
				"label":   "International",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
		},
	})
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)

	if err := store.UpsertAccount(context.Background(), StoredCredentialAccount{
		ID:      "china-1",
		Label:   "China",
		Region:  AccountRegionChina,
		Enabled: true,
		Auth: StoredAuthFields{
			CosyKey:         "cosy-china",
			EncryptUserInfo: "info-china",
			UserID:          "u-china",
			MachineID:       "m-china",
		},
	}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}

	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	assertAccountPresent(t, accounts, "intl-1", AccountRegionInternational, "at-intl")
	assertAccountPresent(t, accounts, "china-1", AccountRegionChina, "cosy-china")
	assertFileMode(t, path, 0o600)
}

func TestAccountStoreUpsertAccountReplacesExistingAccountByID(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"id":      "china-1",
				"label":   "China",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "old-cosy",
					"encrypt_user_info": "old-info",
					"user_id":           "old-user",
					"machine_id":        "old-machine",
				},
			},
		},
	})
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)

	if err := store.UpsertAccount(context.Background(), StoredCredentialAccount{
		ID:      "china-1",
		Label:   "China Updated",
		Region:  AccountRegionChina,
		Enabled: true,
		Auth: StoredAuthFields{
			CosyKey:         "new-cosy",
			EncryptUserInfo: "new-info",
			UserID:          "new-user",
			MachineID:       "new-machine",
		},
	}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}

	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].CosyKey != "new-cosy" || accounts[0].Label != "China Updated" {
		t.Fatalf("account not replaced: %+v", accounts[0])
	}
}

func TestAccountStoreUpsertAccountMigratesLegacyCredential(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 1,
		"source":         "legacy",
		"auth": map[string]string{
			"cosy_key":          "legacy-cosy",
			"encrypt_user_info": "legacy-info",
			"user_id":           "legacy-user",
			"machine_id":        "legacy-machine",
		},
	})
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)

	if err := store.UpsertAccount(context.Background(), StoredCredentialAccount{
		ID:      "china-new",
		Region:  AccountRegionChina,
		Enabled: true,
		Auth: StoredAuthFields{
			CosyKey:         "new-cosy",
			EncryptUserInfo: "new-info",
			UserID:          "new-user",
			MachineID:       "new-machine",
		},
	}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}

	var stored StoredCredentialFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if stored.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", stored.SchemaVersion)
	}
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected migrated legacy plus new account, got %d", len(accounts))
	}
	assertAccountPresent(t, accounts, "", AccountRegionChina, "legacy-cosy")
	assertAccountPresent(t, accounts, "china-new", AccountRegionChina, "new-cosy")
}

func TestAccountStoreUpsertAccountRejectsGeneratedIDWithoutStableIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)

	err := store.UpsertAccount(context.Background(), StoredCredentialAccount{
		Region:  AccountRegionInternational,
		Enabled: true,
		Auth: StoredAuthFields{
			AccessToken: "at-intl",
		},
	})
	if !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected ErrCredentialsUnavailable, got %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("auth file should not be written, stat err=%v", statErr)
	}
}

func writeAccountCredentialFile(t *testing.T, payload map[string]any) string {
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

func accountCredentialWithoutID(label, updatedAt, tokenExpireTime string) map[string]any {
	return map[string]any{
		"schema_version": 2,
		"accounts": []map[string]any{
			{
				"label":             label,
				"region":            "china",
				"enabled":           true,
				"updated_at":        updatedAt,
				"token_expire_time": tokenExpireTime,
				"auth": map[string]string{
					"cosy_key":          "secret-cosy",
					"encrypt_user_info": "secret-info",
					"user_id":           "u-stable",
					"machine_id":        "m-stable",
				},
			},
		},
	}
}

func loadSingleAccountID(t *testing.T, path string) string {
	t.Helper()

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, fixedAccountNow)
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	return accounts[0].ID
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()

	if strings.Contains(haystack, needle) {
		t.Fatalf("expected %q not to contain %q", haystack, needle)
	}
}

func assertAccountPresent(t *testing.T, accounts []AccountSnapshot, id string, region AccountRegion, secret string) {
	t.Helper()

	for _, account := range accounts {
		if id != "" && account.ID != id {
			continue
		}
		if account.Region != region {
			continue
		}
		if account.CosyKey == secret || account.AccessToken == secret {
			return
		}
	}
	t.Fatalf("account id=%q region=%q secret=%q not found in %+v", id, region, secret, accounts)
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("file mode = %v, want %v", got, want)
	}
}

func fixedAccountNow() time.Time {
	return time.Unix(1, 0)
}
