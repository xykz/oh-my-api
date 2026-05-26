# Lingma Multi-Region Accounts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-account Lingma routing with China and International account regions, account-average round-robin balancing, schema-v2 credential storage, and an account-pool admin UI.

**Architecture:** Introduce account routing as a layer above region-specific transports. Config chooses eligible account regions, an account store loads schema-v1 and schema-v2 credentials, a balancer selects one account per request, and region adapters own endpoint paths, signing, payload mapping, and model parsing. China behavior is preserved through a China adapter; International starts as a configurable adapter that reports protocol-not-configured until real API samples are available.

**Tech Stack:** Go backend (`internal/config`, `internal/proxy`, `internal/api`, `internal/auth`), Vite/React/TypeScript frontend (`frontend/src`), existing Go tests and Playwright tests.

---

## File Structure

- Modify `internal/config/config.go`: add `AccountConfig`, parsing, defaults, validation.
- Modify `internal/config/config_test.go`: cover account config defaults, overrides, invalid values.
- Modify `internal/proxy/types.go`: add account region/routing types, account snapshots, sanitized summaries, and account-aware test result types.
- Create `internal/proxy/accounts.go`: schema-v1/v2 credential loading, account status, selection helpers, schema-v2 write/update helpers.
- Create `internal/proxy/accounts_test.go`: credential compatibility and secret-redaction tests.
- Create `internal/proxy/balancer.go`: routing-mode filtering and account-average round-robin.
- Create `internal/proxy/balancer_test.go`: `china_only`, `international_only`, `mixed`, no-eligible-account, and round-robin tests.
- Create `internal/proxy/adapters.go`: `RegionAdapter` interface, adapter registry, protocol-not-configured error.
- Create `internal/proxy/china_adapter.go`: wrap current signer/body/native transport behavior.
- Create `internal/proxy/international_adapter.go`: configurable base URL with explicit unsupported protocol responses.
- Create `internal/proxy/adapters_test.go`: adapter registry and International unsupported behavior.
- Modify `internal/proxy/models.go`: aggregate model refresh across eligible accounts through adapters.
- Modify `internal/proxy/models_test.go`: model aggregation and per-account error behavior.
- Modify `internal/api/server.go`: add account-aware dependencies and route chat through selected account/adapter.
- Modify `internal/api/anthropic_handler.go`: mirror account-aware routing for `/v1/messages`.
- Modify `internal/api/admin_handlers.go`: return account-pool data, account-aware test/refresh behavior, and overview counts.
- Modify `internal/api/bootstrap_handler.go`: accept `region` in bootstrap body.
- Modify `internal/api/bootstrap_manager.go` and `internal/api/bootstrap_remote_callback.go`: save China bootstrap output into schema-v2 account file without deleting other accounts.
- Modify `internal/api/server_test.go` and create `internal/api/account_routing_test.go`: selected account metadata and admin redaction tests.
- Modify `main.go`: construct account store, balancer, adapter registry, and account-aware services.
- Modify `auth/credentials.example.json`: show schema-v2 accounts with safe redacted sample values.
- Modify `config.yaml`: add commented account defaults with safe sample values.
- Modify `frontend/src/types/index.ts`: add account-region/routing/account-summary types and bootstrap region.
- Modify `frontend/src/api/client.ts`: add account-aware admin calls.
- Replace `frontend/src/pages/Account.tsx`: account-pool management UI with normal Simplified Chinese text.
- Modify `frontend/tests/account-bootstrap.spec.ts`: cover China/International login controls and unsupported International flow.

## Task 1: Account Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config.yaml`

- [ ] **Step 1: Write failing config tests**

Add these tests to `internal/config/config_test.go`:

```go
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
	if cfg.Account.ChinaBaseURL != "https://api.lingma.cn" {
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
```

Also add `strings` to the imports in `internal/config/config_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/config -run Account -count=1
```

Expected: FAIL because `Config.Account` is undefined.

- [ ] **Step 3: Implement account config**

In `internal/config/config.go`, add an `Account` field and type:

```go
type Config struct {
	Server     ServerConfig
	Credential CredentialConfig
	Session    SessionConfig
	Lingma     LingmaConfig
	Account    AccountConfig
}

type AccountConfig struct {
	RoutingMode          string
	LoadBalance          string
	ChinaBaseURL         string
	InternationalBaseURL string
}
```

Set defaults in `Default()`:

```go
Account: AccountConfig{
	RoutingMode:          "mixed",
	LoadBalance:          "round_robin",
	ChinaBaseURL:         "https://lingma.alibabacloud.com",
	InternationalBaseURL: "https://api.lingma.ai",
},
```

After `applyYAML` in `Load`, call:

```go
if cfg.Account.ChinaBaseURL == "" {
	cfg.Account.ChinaBaseURL = cfg.Lingma.BaseURL
}
if err := validateAccountConfig(cfg.Account); err != nil {
	return Config{}, err
}
```

Add `account` to `assignValue`:

```go
case "account":
	return assignAccountValue(&cfg.Account, key, value)
```

Add:

```go
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
```

Update `config.yaml` with:

```yaml
account:
  routing_mode: "mixed"
  load_balance: "round_robin"
  china_base_url: "https://api.lingma.cn"
  international_base_url: "https://api.lingma.ai"
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```powershell
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config -run Account -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/config/config.go internal/config/config_test.go config.yaml
git commit -m "feat: add account routing config"
```

## Task 2: Account Types And Credential Loader

**Files:**
- Modify: `internal/proxy/types.go`
- Create: `internal/proxy/accounts.go`
- Create: `internal/proxy/accounts_test.go`
- Modify: `auth/credentials.example.json`

- [ ] **Step 1: Write failing credential loader tests**

Create `internal/proxy/accounts_test.go`:

```go
package proxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

func TestAccountStoreReadsLegacyCredentialAsChinaAccount(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 1,
		"source":         "project_bootstrap",
		"auth": map[string]string{
			"cosy_key":          "sentinel-key",
			"encrypt_user_info": "sentinel-info",
			"user_id":           "u-123",
			"machine_id":        "m-123",
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, func() time.Time {
		return time.Unix(1, 0)
	})
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("len(accounts) = %d, want 1", len(accounts))
	}
	if accounts[0].Region != AccountRegionChina {
		t.Fatalf("region = %q", accounts[0].Region)
	}
	if accounts[0].CosyKey != "sentinel-key" {
		t.Fatalf("cosy key = %q", accounts[0].CosyKey)
	}
	if !accounts[0].Enabled {
		t.Fatal("legacy account should be enabled")
	}
}

func TestAccountStoreReadsSchemaV2Accounts(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []any{
			map[string]any{
				"id":      "china-1",
				"label":   "China 1",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "k1",
					"encrypt_user_info": "info1",
					"user_id":           "u1",
					"machine_id":        "m1",
				},
			},
			map[string]any{
				"id":      "intl-1",
				"label":   "Intl 1",
				"region":  "international",
				"enabled": true,
				"auth": map[string]string{
					"access_token": "at-intl",
				},
			},
		},
	})

	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, time.Now)
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	if accounts[1].Region != AccountRegionInternational {
		t.Fatalf("region = %q", accounts[1].Region)
	}
	if accounts[1].AccessToken != "at-intl" {
		t.Fatalf("access token = %q", accounts[1].AccessToken)
	}
}

func TestAccountStoreSummariesRedactSecrets(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []any{
			map[string]any{
				"id":      "china-1",
				"label":   "China 1",
				"region":  "china",
				"enabled": true,
				"auth": map[string]string{
					"cosy_key":          "secret-cosy",
					"encrypt_user_info": "secret-info",
					"user_id":           "u1",
					"machine_id":        "m1",
				},
			},
		},
	})
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, time.Now)

	summaries, err := store.Summaries(context.Background())
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	data, err := json.Marshal(summaries)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) == "" || containsAny(string(data), "secret-cosy", "secret-info") {
		t.Fatalf("summary leaked secret: %s", data)
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

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
```

Add `strings` to this test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/proxy -run AccountStore -count=1
```

Expected: FAIL because `NewAccountStore` and account types are undefined.

- [ ] **Step 3: Add account types**

In `internal/proxy/types.go`, add:

```go
type AccountRegion string

const (
	AccountRegionChina         AccountRegion = "china"
	AccountRegionInternational AccountRegion = "international"
)

type AccountSnapshot struct {
	ID                string        `json:"id"`
	Label             string        `json:"label"`
	Region            AccountRegion `json:"region"`
	Enabled           bool          `json:"enabled"`
	CosyKey           string        `json:"cosy_key,omitempty"`
	EncryptUserInfo   string        `json:"encrypt_user_info,omitempty"`
	UserID            string        `json:"user_id,omitempty"`
	MachineID         string        `json:"machine_id,omitempty"`
	AccessToken       string        `json:"access_token,omitempty"`
	RefreshToken      string        `json:"refresh_token,omitempty"`
	Source            string        `json:"source,omitempty"`
	LingmaVersionHint string        `json:"lingma_version_hint,omitempty"`
	ObtainedAt        string        `json:"obtained_at,omitempty"`
	UpdatedAt         string        `json:"updated_at,omitempty"`
	TokenExpireTime   int64         `json:"token_expire_time,omitempty"`
	LoadedAt          time.Time     `json:"loaded_at"`
}

type AccountSummary struct {
	ID                string        `json:"id"`
	Label             string        `json:"label"`
	Region            AccountRegion `json:"region"`
	Enabled           bool          `json:"enabled"`
	Source            string        `json:"source,omitempty"`
	LingmaVersionHint string        `json:"lingma_version_hint,omitempty"`
	ObtainedAt        string        `json:"obtained_at,omitempty"`
	UpdatedAt         string        `json:"updated_at,omitempty"`
	TokenExpireTime   int64         `json:"token_expire_time,omitempty"`
	TokenExpired      bool          `json:"token_expired"`
	HasCosyKey        bool          `json:"has_cosy_key"`
	HasEncryptInfo    bool          `json:"has_encrypt_user_info"`
	HasAccessToken    bool          `json:"has_access_token"`
	HasRefreshToken   bool          `json:"has_refresh_token"`
	UserID            string        `json:"user_id,omitempty"`
	MachineID         string        `json:"machine_id,omitempty"`
	LoadedAt          time.Time     `json:"loaded_at"`
}

type StoredCredentialAccount struct {
	ID                string            `json:"id"`
	Label             string            `json:"label"`
	Region            AccountRegion     `json:"region"`
	Enabled           bool              `json:"enabled"`
	Source            string            `json:"source,omitempty"`
	LingmaVersionHint string            `json:"lingma_version_hint,omitempty"`
	ObtainedAt        string            `json:"obtained_at,omitempty"`
	UpdatedAt         string            `json:"updated_at,omitempty"`
	TokenExpireTime   string            `json:"token_expire_time,omitempty"`
	Auth              StoredAuthFields  `json:"auth"`
	OAuth             StoredOAuthFields `json:"oauth,omitempty"`
}
```

Extend `StoredCredentialFile`:

```go
Accounts []StoredCredentialAccount `json:"accounts,omitempty"`
```

Extend `StoredAuthFields`:

```go
AccessToken string `json:"access_token,omitempty"`
```

- [ ] **Step 4: Implement account store**

Create `internal/proxy/accounts.go`:

```go
package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type AccountStore struct {
	mu      sync.RWMutex
	cfg     config.CredentialConfig
	now     func() time.Time
	loaded  bool
	current []AccountSnapshot
}

func NewAccountStore(cfg config.CredentialConfig, now func() time.Time) *AccountStore {
	if now == nil {
		now = time.Now
	}
	if cfg.AuthFile == "" {
		cfg.AuthFile = "./auth/credentials.json"
	}
	return &AccountStore{cfg: cfg, now: now}
}

func (s *AccountStore) Accounts(ctx context.Context) ([]AccountSnapshot, error) {
	s.mu.RLock()
	if s.loaded {
		out := cloneAccounts(s.current)
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()
	return s.Refresh(ctx)
}

func (s *AccountStore) Refresh(_ context.Context) ([]AccountSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.loadAccounts()
	if err != nil {
		return nil, err
	}
	s.current = accounts
	s.loaded = true
	return cloneAccounts(accounts), nil
}

func (s *AccountStore) Summaries(ctx context.Context) ([]AccountSummary, error) {
	accounts, err := s.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]AccountSummary, 0, len(accounts))
	for _, account := range accounts {
		summaries = append(summaries, account.Summary(5*time.Minute))
	}
	return summaries, nil
}

func (s *AccountStore) loadAccounts() ([]AccountSnapshot, error) {
	if s.cfg.AuthFile == "" {
		return nil, fmt.Errorf("%w: missing auth_file", ErrCredentialsUnavailable)
	}
	data, err := os.ReadFile(s.cfg.AuthFile)
	if err != nil {
		return nil, fmt.Errorf("%w: read auth file: %v", ErrCredentialsUnavailable, err)
	}
	var stored StoredCredentialFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("%w: parse auth file: %v", ErrCredentialsUnavailable, err)
	}
	if stored.SchemaVersion >= 2 || len(stored.Accounts) > 0 {
		return s.loadV2(stored)
	}
	account := AccountSnapshot{
		ID:                stableAccountID(stored.Auth.UserID, stored.Auth.MachineID, stored.Source),
		Label:             "China account",
		Region:            AccountRegionChina,
		Enabled:           true,
		CosyKey:           stored.Auth.CosyKey,
		EncryptUserInfo:   stored.Auth.EncryptUserInfo,
		UserID:            stored.Auth.UserID,
		MachineID:         stored.Auth.MachineID,
		AccessToken:       stored.OAuth.AccessToken,
		RefreshToken:      stored.OAuth.RefreshToken,
		Source:            firstNonEmptyString(stored.Source, "project_auth_file"),
		LingmaVersionHint: stored.LingmaVersionHint,
		ObtainedAt:        stored.ObtainedAt,
		UpdatedAt:         stored.UpdatedAt,
		TokenExpireTime:   parseExpireTime(stored.TokenExpireTime),
		LoadedAt:          s.now(),
	}
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	return []AccountSnapshot{account}, nil
}

func (s *AccountStore) loadV2(stored StoredCredentialFile) ([]AccountSnapshot, error) {
	accounts := make([]AccountSnapshot, 0, len(stored.Accounts))
	for _, item := range stored.Accounts {
		account := AccountSnapshot{
			ID:                item.ID,
			Label:             item.Label,
			Region:            item.Region,
			Enabled:           item.Enabled,
			CosyKey:           item.Auth.CosyKey,
			EncryptUserInfo:   item.Auth.EncryptUserInfo,
			UserID:            item.Auth.UserID,
			MachineID:         item.Auth.MachineID,
			AccessToken:       firstNonEmptyString(item.Auth.AccessToken, item.OAuth.AccessToken),
			RefreshToken:      item.OAuth.RefreshToken,
			Source:            item.Source,
			LingmaVersionHint: item.LingmaVersionHint,
			ObtainedAt:        item.ObtainedAt,
			UpdatedAt:         item.UpdatedAt,
			TokenExpireTime:   parseExpireTime(item.TokenExpireTime),
			LoadedAt:          s.now(),
		}
		if account.ID == "" {
			account.ID = stableAccountID(account.UserID, account.MachineID, string(account.Region))
		}
		if account.Label == "" {
			account.Label = string(account.Region) + " account"
		}
		if account.Region == "" {
			account.Region = AccountRegionChina
		}
		if err := validateAccount(account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (a AccountSnapshot) IsTokenExpired(grace time.Duration) bool {
	if a.TokenExpireTime == 0 {
		return false
	}
	return time.Now().Add(grace).UnixMilli() > a.TokenExpireTime
}

func (a AccountSnapshot) Summary(grace time.Duration) AccountSummary {
	return AccountSummary{
		ID:                a.ID,
		Label:             a.Label,
		Region:            a.Region,
		Enabled:           a.Enabled,
		Source:            a.Source,
		LingmaVersionHint: a.LingmaVersionHint,
		ObtainedAt:        a.ObtainedAt,
		UpdatedAt:         a.UpdatedAt,
		TokenExpireTime:   a.TokenExpireTime,
		TokenExpired:      a.IsTokenExpired(grace),
		HasCosyKey:        a.CosyKey != "",
		HasEncryptInfo:    a.EncryptUserInfo != "",
		HasAccessToken:    a.AccessToken != "",
		HasRefreshToken:   a.RefreshToken != "",
		UserID:            a.UserID,
		MachineID:         a.MachineID,
		LoadedAt:          a.LoadedAt,
	}
}

func validateAccount(account AccountSnapshot) error {
	switch account.Region {
	case AccountRegionChina:
		if account.CosyKey == "" || account.EncryptUserInfo == "" || account.UserID == "" || account.MachineID == "" {
			return fmt.Errorf("%w: invalid china account %q", ErrCredentialsUnavailable, account.ID)
		}
	case AccountRegionInternational:
		if account.AccessToken == "" && account.CosyKey == "" {
			return fmt.Errorf("%w: invalid international account %q", ErrCredentialsUnavailable, account.ID)
		}
	default:
		return fmt.Errorf("%w: unknown account region %q", ErrCredentialsUnavailable, account.Region)
	}
	return nil
}

func cloneAccounts(accounts []AccountSnapshot) []AccountSnapshot {
	out := make([]AccountSnapshot, len(accounts))
	copy(out, accounts)
	return out
}

func stableAccountID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return "acct-" + hex.EncodeToString(sum[:6])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 5: Update credential example**

Replace `auth/credentials.example.json` with schema-v2 sample accounts containing no real secrets:

```json
{
  "schema_version": 2,
  "accounts": [
    {
      "id": "china-example",
      "label": "China account",
      "region": "china",
      "enabled": true,
      "source": "example",
      "lingma_version_hint": "2.11.2",
      "token_expire_time": "1770000000000",
      "auth": {
        "cosy_key": "replace-me",
        "encrypt_user_info": "replace-me",
        "user_id": "replace-me",
        "machine_id": "replace-me"
      },
      "oauth": {
        "access_token": "optional",
        "refresh_token": "optional"
      }
    },
    {
      "id": "international-example",
      "label": "International account",
      "region": "international",
      "enabled": false,
      "source": "example",
      "auth": {
        "access_token": "replace-me"
      }
    }
  ]
}
```

- [ ] **Step 6: Run tests**

Run:

```powershell
gofmt -w internal/proxy/types.go internal/proxy/accounts.go internal/proxy/accounts_test.go
go test ./internal/proxy -run AccountStore -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/proxy/types.go internal/proxy/accounts.go internal/proxy/accounts_test.go auth/credentials.example.json
git commit -m "feat: add multi-account credential store"
```

## Task 3: Routing And Round-Robin Balancer

**Files:**
- Create: `internal/proxy/balancer.go`
- Create: `internal/proxy/balancer_test.go`

- [ ] **Step 1: Write failing balancer tests**

Create `internal/proxy/balancer_test.go`:

```go
package proxy

import (
	"testing"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

func TestAccountPoolFiltersByRoutingMode(t *testing.T) {
	accounts := []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
		{ID: "china-disabled", Region: AccountRegionChina, Enabled: false},
	}

	tests := []struct {
		name string
		mode string
		want []string
	}{
		{name: "china only", mode: "china_only", want: []string{"china-1"}},
		{name: "international only", mode: "international_only", want: []string{"intl-1"}},
		{name: "mixed", mode: "mixed", want: []string{"china-1", "intl-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewAccountPool(config.AccountConfig{RoutingMode: tt.mode, LoadBalance: "round_robin"})
			got := pool.Eligible(accounts)
			if idsOf(got) != strings.Join(tt.want, ",") {
				t.Fatalf("ids = %s, want %s", idsOf(got), strings.Join(tt.want, ","))
			}
		})
	}
}

func TestRoundRobinBalancerIsAccountAverage(t *testing.T) {
	balancer := NewRoundRobinBalancer()
	accounts := []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "china-2", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
	}
	var got []string
	for i := 0; i < 5; i++ {
		account, err := balancer.Next(accounts)
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		got = append(got, account.ID)
	}
	want := "china-1,china-2,intl-1,china-1,china-2"
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %s, want %s", strings.Join(got, ","), want)
	}
}

func TestRoundRobinBalancerRejectsEmptyPool(t *testing.T) {
	_, err := NewRoundRobinBalancer().Next(nil)
	if err == nil || !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("expected credentials unavailable, got %v", err)
	}
}

func idsOf(accounts []AccountSnapshot) string {
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return strings.Join(ids, ",")
}
```

Add imports `errors` and `strings`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/proxy -run "AccountPool|RoundRobin" -count=1
```

Expected: FAIL because `NewAccountPool` and `NewRoundRobinBalancer` are undefined.

- [ ] **Step 3: Implement balancer**

Create `internal/proxy/balancer.go`:

```go
package proxy

import (
	"fmt"
	"sync"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type AccountPool struct {
	cfg config.AccountConfig
}

func NewAccountPool(cfg config.AccountConfig) *AccountPool {
	if cfg.RoutingMode == "" {
		cfg.RoutingMode = "mixed"
	}
	return &AccountPool{cfg: cfg}
}

func (p *AccountPool) Eligible(accounts []AccountSnapshot) []AccountSnapshot {
	out := make([]AccountSnapshot, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		switch p.cfg.RoutingMode {
		case "china_only":
			if account.Region == AccountRegionChina {
				out = append(out, account)
			}
		case "international_only":
			if account.Region == AccountRegionInternational {
				out = append(out, account)
			}
		default:
			out = append(out, account)
		}
	}
	return out
}

type RoundRobinBalancer struct {
	mu    sync.Mutex
	index int
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (b *RoundRobinBalancer) Next(accounts []AccountSnapshot) (AccountSnapshot, error) {
	if len(accounts) == 0 {
		return AccountSnapshot{}, fmt.Errorf("%w: no eligible accounts", ErrCredentialsUnavailable)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	account := accounts[b.index%len(accounts)]
	b.index++
	return account, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```powershell
gofmt -w internal/proxy/balancer.go internal/proxy/balancer_test.go
go test ./internal/proxy -run "AccountPool|RoundRobin" -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/proxy/balancer.go internal/proxy/balancer_test.go
git commit -m "feat: add account routing balancer"
```

## Task 4: Region Adapter Interfaces

**Files:**
- Create: `internal/proxy/adapters.go`
- Create: `internal/proxy/china_adapter.go`
- Create: `internal/proxy/international_adapter.go`
- Create: `internal/proxy/adapters_test.go`

- [ ] **Step 1: Write failing adapter tests**

Create `internal/proxy/adapters_test.go`:

```go
package proxy

import (
	"context"
	"errors"
	"testing"
)

func TestAdapterRegistryReturnsAdapterByRegion(t *testing.T) {
	registry := NewAdapterRegistry()
	intl := NewInternationalAdapter("https://api.lingma.ai")
	registry.Register(intl)

	got, err := registry.ForRegion(AccountRegionInternational)
	if err != nil {
		t.Fatalf("ForRegion() error = %v", err)
	}
	if got.Region() != AccountRegionInternational {
		t.Fatalf("region = %q", got.Region())
	}
}

func TestInternationalAdapterReportsProtocolNotConfigured(t *testing.T) {
	adapter := NewInternationalAdapter("https://api.lingma.ai")
	_, err := adapter.ListModels(context.Background(), AccountSnapshot{ID: "intl-1", Region: AccountRegionInternational, AccessToken: "at"})
	if !errors.Is(err, ErrAdapterProtocolNotConfigured) {
		t.Fatalf("expected protocol-not-configured, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/proxy -run "Adapter|International" -count=1
```

Expected: FAIL because adapter types are undefined.

- [ ] **Step 3: Implement adapter interface and registry**

Create `internal/proxy/adapters.go`:

```go
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var ErrAdapterProtocolNotConfigured = errors.New("adapter protocol not configured")

type AccountTestResult struct {
	AccountID       string        `json:"account_id,omitempty"`
	AccountLabel    string        `json:"account_label,omitempty"`
	Region          AccountRegion `json:"region,omitempty"`
	Success         bool          `json:"success"`
	StatusCode      int           `json:"status_code"`
	ResponsePreview string        `json:"response_preview"`
	Error           string        `json:"error"`
	Timestamp       string        `json:"timestamp"`
}

type RegionAdapter interface {
	Region() AccountRegion
	ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error)
	BuildChatRequest(ctx context.Context, canonical CanonicalRequest, modelKey string, account AccountSnapshot) (RemoteChatRequest, error)
	StreamChat(ctx context.Context, request RemoteChatRequest, account AccountSnapshot) (io.ReadCloser, error)
	UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error)
	TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult
}

type AdapterRegistry struct {
	adapters map[AccountRegion]RegionAdapter
}

func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: map[AccountRegion]RegionAdapter{}}
}

func (r *AdapterRegistry) Register(adapter RegionAdapter) {
	r.adapters[adapter.Region()] = adapter
}

func (r *AdapterRegistry) ForRegion(region AccountRegion) (RegionAdapter, error) {
	adapter, ok := r.adapters[region]
	if !ok {
		return nil, fmt.Errorf("%w: no adapter for region %q", ErrAdapterProtocolNotConfigured, region)
	}
	return adapter, nil
}
```

- [ ] **Step 4: Implement China adapter**

Create `internal/proxy/china_adapter.go`:

```go
package proxy

import (
	"context"
	"io"
	"time"
)

type ChinaAdapter struct {
	transport *NativeTransport
	builder   *BodyBuilder
	now       func() time.Time
}

func NewChinaAdapter(transport *NativeTransport, builder *BodyBuilder, now func() time.Time) *ChinaAdapter {
	if now == nil {
		now = time.Now
	}
	return &ChinaAdapter{transport: transport, builder: builder, now: now}
}

func (a *ChinaAdapter) Region() AccountRegion { return AccountRegionChina }

func (a *ChinaAdapter) ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error) {
	return a.transport.ListModels(ctx, account.ToCredentialSnapshot())
}

func (a *ChinaAdapter) BuildChatRequest(ctx context.Context, canonical CanonicalRequest, modelKey string, account AccountSnapshot) (RemoteChatRequest, error) {
	_ = ctx
	_ = account
	return a.builder.BuildCanonical(canonical, modelKey)
}

func (a *ChinaAdapter) StreamChat(ctx context.Context, request RemoteChatRequest, account AccountSnapshot) (io.ReadCloser, error) {
	return a.transport.StreamChat(ctx, request, account.ToCredentialSnapshot())
}

func (a *ChinaAdapter) UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error) {
	return a.transport.UploadImage(ctx, account.ToCredentialSnapshot(), imageURI)
}

func (a *ChinaAdapter) TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult {
	models, err := a.ListModels(ctx, account)
	if err != nil {
		return AccountTestResult{AccountID: account.ID, AccountLabel: account.Label, Region: account.Region, Success: false, Error: err.Error(), Timestamp: a.now().Format(time.RFC3339)}
	}
	return AccountTestResult{AccountID: account.ID, AccountLabel: account.Label, Region: account.Region, Success: true, StatusCode: 200, ResponsePreview: fmt.Sprintf("ListModels returned %d models", len(models)), Timestamp: a.now().Format(time.RFC3339)}
}
```

Add `fmt` to imports. Add this method to `internal/proxy/accounts.go`:

```go
func (a AccountSnapshot) ToCredentialSnapshot() CredentialSnapshot {
	return CredentialSnapshot{
		CosyKey:         a.CosyKey,
		EncryptUserInfo: a.EncryptUserInfo,
		UserID:          a.UserID,
		MachineID:       a.MachineID,
		Source:          a.Source,
		LoadedAt:        a.LoadedAt,
		TokenExpireTime: a.TokenExpireTime,
	}
}
```

- [ ] **Step 5: Implement International unsupported-protocol adapter**

Create `internal/proxy/international_adapter.go`:

```go
package proxy

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type InternationalAdapter struct {
	baseURL string
	now     func() time.Time
}

func NewInternationalAdapter(baseURL string) *InternationalAdapter {
	if baseURL == "" {
		baseURL = "https://api.lingma.ai"
	}
	return &InternationalAdapter{baseURL: strings.TrimRight(baseURL, "/"), now: time.Now}
}

func (a *InternationalAdapter) Region() AccountRegion { return AccountRegionInternational }

func (a *InternationalAdapter) ListModels(context.Context, AccountSnapshot) ([]RemoteModel, error) {
	return nil, fmt.Errorf("%w: international adapter protocol not configured", ErrAdapterProtocolNotConfigured)
}

func (a *InternationalAdapter) BuildChatRequest(context.Context, CanonicalRequest, string, AccountSnapshot) (RemoteChatRequest, error) {
	return RemoteChatRequest{}, fmt.Errorf("%w: international adapter protocol not configured", ErrAdapterProtocolNotConfigured)
}

func (a *InternationalAdapter) StreamChat(context.Context, RemoteChatRequest, AccountSnapshot) (io.ReadCloser, error) {
	return nil, fmt.Errorf("%w: international adapter protocol not configured", ErrAdapterProtocolNotConfigured)
}

func (a *InternationalAdapter) UploadImage(context.Context, AccountSnapshot, string) (string, error) {
	return "", fmt.Errorf("%w: international adapter protocol not configured", ErrAdapterProtocolNotConfigured)
}

func (a *InternationalAdapter) TestConnection(_ context.Context, account AccountSnapshot) AccountTestResult {
	err := fmt.Errorf("%w: international adapter protocol not configured", ErrAdapterProtocolNotConfigured)
	return AccountTestResult{AccountID: account.ID, AccountLabel: account.Label, Region: account.Region, Success: false, Error: err.Error(), Timestamp: a.now().Format(time.RFC3339)}
}
```

- [ ] **Step 6: Run tests**

Run:

```powershell
gofmt -w internal/proxy/adapters.go internal/proxy/china_adapter.go internal/proxy/international_adapter.go internal/proxy/adapters_test.go internal/proxy/accounts.go
go test ./internal/proxy -run "Adapter|International" -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/proxy/adapters.go internal/proxy/china_adapter.go internal/proxy/international_adapter.go internal/proxy/adapters_test.go internal/proxy/accounts.go
git commit -m "feat: add lingma region adapters"
```

## Task 5: Account-Aware Model Service

**Files:**
- Modify: `internal/proxy/models.go`
- Modify: `internal/proxy/models_test.go`

- [ ] **Step 1: Write failing aggregation test**

Add to `internal/proxy/models_test.go`:

```go
type fakeAccountReader struct {
	accounts []AccountSnapshot
}

func (f fakeAccountReader) Accounts(context.Context) ([]AccountSnapshot, error) {
	return f.accounts, nil
}

type fakeRegionAdapter struct {
	region AccountRegion
	models []RemoteModel
	err    error
}

func (f fakeRegionAdapter) Region() AccountRegion { return f.region }
func (f fakeRegionAdapter) ListModels(context.Context, AccountSnapshot) ([]RemoteModel, error) { return f.models, f.err }
func (f fakeRegionAdapter) BuildChatRequest(context.Context, CanonicalRequest, string, AccountSnapshot) (RemoteChatRequest, error) {
	return RemoteChatRequest{}, nil
}
func (f fakeRegionAdapter) StreamChat(context.Context, RemoteChatRequest, AccountSnapshot) (io.ReadCloser, error) {
	return nil, nil
}
func (f fakeRegionAdapter) UploadImage(context.Context, AccountSnapshot, string) (string, error) { return "", nil }
func (f fakeRegionAdapter) TestConnection(context.Context, AccountSnapshot) AccountTestResult { return AccountTestResult{} }

func TestModelServiceAggregatesAcrossEligibleAccounts(t *testing.T) {
	registry := NewAdapterRegistry()
	registry.Register(fakeRegionAdapter{region: AccountRegionChina, models: []RemoteModel{{Key: "china-model"}}})
	registry.Register(fakeRegionAdapter{region: AccountRegionInternational, models: []RemoteModel{{Key: "intl-model"}}})

	service := NewAccountModelService(
		fakeAccountReader{accounts: []AccountSnapshot{
			{ID: "china-1", Region: AccountRegionChina, Enabled: true},
			{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
		}},
		NewAccountPool(config.AccountConfig{RoutingMode: "mixed", LoadBalance: "round_robin"}),
		registry,
		DefaultAliases(),
		time.Now,
	)

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	ids := modelIDs(models)
	if !strings.Contains(ids, "china-model") || !strings.Contains(ids, "intl-model") {
		t.Fatalf("ids = %s", ids)
	}
}
```

Add imports `io`, `lingma2api/internal/config`, and `strings` as needed.

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/proxy -run TestModelServiceAggregatesAcrossEligibleAccounts -count=1
```

Expected: FAIL because `NewAccountModelService` is undefined.

- [ ] **Step 3: Implement account-aware model service**

In `internal/proxy/models.go`, keep the existing `NewModelService` for compatibility and add a second constructor and fields:

```go
type accountReader interface {
	Accounts(context.Context) ([]AccountSnapshot, error)
}

type ModelService struct {
	mu             sync.RWMutex
	transport      remoteModelFetcher
	credentials    credentialReader
	accountReader  accountReader
	accountPool    *AccountPool
	adapters       *AdapterRegistry
	aliases        map[string]string
	modelsByKey    map[string]RemoteModel
	fetchedAt      time.Time
	lastError      string
	now            func() time.Time
}

func NewAccountModelService(accounts accountReader, pool *AccountPool, adapters *AdapterRegistry, aliases map[string]string, now func() time.Time) *ModelService {
	service := NewModelService(nil, nil, aliases, now)
	service.accountReader = accounts
	service.accountPool = pool
	service.adapters = adapters
	return service
}
```

In `Refresh`, branch when account-aware fields are present:

```go
if service.accountReader != nil && service.accountPool != nil && service.adapters != nil {
	return service.refreshFromAccounts(ctx)
}
```

Add:

```go
func (service *ModelService) refreshFromAccounts(ctx context.Context) error {
	accounts, err := service.accountReader.Accounts(ctx)
	if err != nil {
		service.recordError(err.Error())
		return err
	}
	eligible := service.accountPool.Eligible(accounts)
	if len(eligible) == 0 {
		err := fmt.Errorf("%w: no eligible accounts", ErrCredentialsUnavailable)
		service.recordError(err.Error())
		return err
	}
	modelsByKey := map[string]RemoteModel{}
	var lastErr error
	for _, account := range eligible {
		adapter, err := service.adapters.ForRegion(account.Region)
		if err != nil {
			lastErr = err
			continue
		}
		models, err := adapter.ListModels(ctx, account)
		if err != nil {
			lastErr = err
			continue
		}
		for _, model := range models {
			if model.Key != "" {
				modelsByKey[model.Key] = model
			}
		}
	}
	if len(modelsByKey) == 0 && lastErr != nil {
		service.recordError(lastErr.Error())
		return lastErr
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.modelsByKey = modelsByKey
	service.fetchedAt = service.now()
	service.lastError = ""
	return nil
}
```

Add `fmt` import.

- [ ] **Step 4: Run tests**

Run:

```powershell
gofmt -w internal/proxy/models.go internal/proxy/models_test.go
go test ./internal/proxy -run "ModelService|AccountModelService" -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/proxy/models.go internal/proxy/models_test.go
git commit -m "feat: aggregate models across account pool"
```

## Task 6: Account-Aware API Dependencies And Chat Routing

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/anthropic_handler.go`
- Create: `internal/api/account_routing_test.go`
- Modify: `internal/api/server_test.go`

- [ ] **Step 1: Write failing chat routing test**

Create `internal/api/account_routing_test.go`:

```go
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

type fakeAccountProvider struct {
	accounts []proxy.AccountSnapshot
}

func (f fakeAccountProvider) Accounts(context.Context) ([]proxy.AccountSnapshot, error) { return f.accounts, nil }
func (f fakeAccountProvider) Summaries(context.Context) ([]proxy.AccountSummary, error) { return nil, nil }

type recordingAdapter struct {
	selected []string
}

func (a *recordingAdapter) Region() proxy.AccountRegion { return proxy.AccountRegionChina }
func (a *recordingAdapter) ListModels(context.Context, proxy.AccountSnapshot) ([]proxy.RemoteModel, error) { return nil, nil }
func (a *recordingAdapter) BuildChatRequest(_ context.Context, req proxy.CanonicalRequest, modelKey string, account proxy.AccountSnapshot) (proxy.RemoteChatRequest, error) {
	a.selected = append(a.selected, account.ID)
	return proxy.RemoteChatRequest{Path: proxy.ChatPath, Query: proxy.ChatQuery, RequestID: "req-1", ModelKey: modelKey, Stream: req.Stream}, nil
}
func (a *recordingAdapter) StreamChat(context.Context, proxy.RemoteChatRequest, proxy.AccountSnapshot) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(`data:{"body":"{\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}","statusCodeValue":200}` + "\n" + `data:[DONE]`)), nil
}
func (a *recordingAdapter) UploadImage(context.Context, proxy.AccountSnapshot, string) (string, error) { return "", nil }
func (a *recordingAdapter) TestConnection(context.Context, proxy.AccountSnapshot) proxy.AccountTestResult { return proxy.AccountTestResult{} }

func TestChatCompletionsSelectsAccountAndAdapter(t *testing.T) {
	adapter := &recordingAdapter{}
	registry := proxy.NewAdapterRegistry()
	registry.Register(adapter)

	handler := NewServer(Dependencies{
		Accounts: fakeAccountProvider{accounts: []proxy.AccountSnapshot{{ID: "china-1", Region: proxy.AccountRegionChina, Enabled: true}}},
		AccountPool: proxy.NewAccountPool(config.AccountConfig{RoutingMode: "mixed", LoadBalance: "round_robin"}),
		Balancer: proxy.NewRoundRobinBalancer(),
		Adapters: registry,
		Models: fakeModels{},
		Sessions: fakeSessions{},
		Now: func() time.Time { return time.Unix(1, 0) },
	}, nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(adapter.selected) != 1 || adapter.selected[0] != "china-1" {
		t.Fatalf("selected accounts = %#v", adapter.selected)
	}
}
```

Add import `lingma2api/internal/config`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/api -run TestChatCompletionsSelectsAccountAndAdapter -count=1
```

Expected: FAIL because `Dependencies` lacks account-aware fields.

- [ ] **Step 3: Add account-aware dependency interfaces**

In `internal/api/server.go`, add:

```go
type AccountProvider interface {
	Accounts(context.Context) ([]proxy.AccountSnapshot, error)
	Summaries(context.Context) ([]proxy.AccountSummary, error)
}

type AccountBalancer interface {
	Next([]proxy.AccountSnapshot) (proxy.AccountSnapshot, error)
}
```

Extend `Dependencies`:

```go
Accounts    AccountProvider
AccountPool *proxy.AccountPool
Balancer    AccountBalancer
Adapters    *proxy.AdapterRegistry
```

- [ ] **Step 4: Route OpenAI chat through selected account when configured**

In `handleChatCompletions`, replace the single-credential transport path with a branch after model resolution:

```go
if server.deps.Accounts != nil && server.deps.AccountPool != nil && server.deps.Balancer != nil && server.deps.Adapters != nil {
	server.handleAccountRoutedChat(writer, request, projectedRequest, sessionCanonicalRequest, canonicalRequest, policyResult.PostPolicyRequest, sessionID, messages, modelKey)
	return
}
```

Add helper in `internal/api/server.go`:

```go
func (server *Server) selectAccountAndAdapter(ctx context.Context) (proxy.AccountSnapshot, proxy.RegionAdapter, error) {
	accounts, err := server.deps.Accounts.Accounts(ctx)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	eligible := server.deps.AccountPool.Eligible(accounts)
	account, err := server.deps.Balancer.Next(eligible)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	adapter, err := server.deps.Adapters.ForRegion(account.Region)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	return account, adapter, nil
}
```

Add helper that mirrors current non-stream/stream behavior:

```go
func (server *Server) handleAccountRoutedChat(
	writer http.ResponseWriter,
	request *http.Request,
	projectedRequest proxy.OpenAIChatRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionID string,
	messages []proxy.Message,
	modelKey string,
) {
	account, adapter, err := server.selectAccountAndAdapter(request.Context())
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	if server.deps.Uploader != nil {
		imageURLs, err := server.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
		if err != nil {
			writeMappedError(writer, err)
			return
		}
		if len(imageURLs) > 0 {
			sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
			sessionCanonicalRequest.Metadata["is_vl"] = true
		}
	}
	remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	defer stream.Close()
	traceID := proxy.NewUUID()
	if projectedRequest.Stream {
		server.streamChatResponse(writer, request, projectedRequest, remoteRequest, sessionID, messages, stream, prePolicyRequest, postPolicyRequest, sessionCanonicalRequest, traceID)
		return
	}
	server.writeNonStreamResponse(request.Context(), writer, projectedRequest, remoteRequest, sessionID, messages, stream, prePolicyRequest, postPolicyRequest, sessionCanonicalRequest, traceID)
}
```

Add:

```go
func (server *Server) uploadImagesWithAdapter(ctx context.Context, adapter proxy.RegionAdapter, account proxy.AccountSnapshot, req proxy.CanonicalRequest) ([]string, error) {
	var imageURLs []string
	for _, turn := range req.Turns {
		for _, block := range turn.Blocks {
			if block.Type != proxy.CanonicalBlockImage && block.Type != proxy.CanonicalBlockDocument {
				continue
			}
			var src proxy.ImageSource
			if err := json.Unmarshal(block.Data, &src); err != nil {
				return nil, fmt.Errorf("invalid image source: %w", err)
			}
			imageURI := src.Data
			if src.Type == "base64" {
				imageURI = "data:" + src.MediaType + ";base64," + src.Data
			}
			cdnURL, err := adapter.UploadImage(ctx, account, imageURI)
			if err != nil {
				return nil, fmt.Errorf("uploading image: %w", err)
			}
			imageURLs = append(imageURLs, cdnURL)
		}
	}
	return imageURLs, nil
}
```

- [ ] **Step 5: Keep legacy tests passing**

Existing tests use `Credentials`, `Transport`, and `Builder`, so the new branch must activate only when account-aware dependencies are non-nil.

Run:

```powershell
gofmt -w internal/api/server.go internal/api/account_routing_test.go
go test ./internal/api -run "TestChatCompletions" -count=1
```

Expected: PASS.

- [ ] **Step 6: Repeat account routing for Anthropic**

In `internal/api/anthropic_handler.go`, find the current credential/transport/build path. Add the same account branch:

```go
if server.deps.Accounts != nil && server.deps.AccountPool != nil && server.deps.Balancer != nil && server.deps.Adapters != nil {
	account, adapter, err := server.selectAccountAndAdapter(request.Context())
	if err != nil {
		writeAnthropicError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	remoteRequest, err := adapter.BuildChatRequest(request.Context(), canonicalRequest, modelKey, account)
	if err != nil {
		writeAnthropicError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		writeAnthropicError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	defer stream.Close()
	// Continue through existing Anthropic stream conversion using this stream.
}
```

Use the exact local variable names in `anthropic_handler.go`; do not duplicate response conversion logic.

- [ ] **Step 7: Commit**

```powershell
git add internal/api/server.go internal/api/anthropic_handler.go internal/api/account_routing_test.go internal/api/server_test.go
git commit -m "feat: route requests through account adapters"
```

## Task 7: Admin Account API

**Files:**
- Modify: `internal/api/admin_handlers.go`
- Modify: `internal/api/server.go`
- Create: `internal/api/admin_accounts_test.go`

- [ ] **Step 1: Write failing admin redaction test**

Create `internal/api/admin_accounts_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

type fakeAccountAdminProvider struct{}

func (fakeAccountAdminProvider) Accounts(context.Context) ([]proxy.AccountSnapshot, error) {
	return []proxy.AccountSnapshot{{ID: "china-1", Label: "China 1", Region: proxy.AccountRegionChina, Enabled: true, CosyKey: "secret-cosy", EncryptUserInfo: "secret-info", UserID: "u1", MachineID: "m1"}}, nil
}

func (fakeAccountAdminProvider) Summaries(context.Context) ([]proxy.AccountSummary, error) {
	return []proxy.AccountSummary{{ID: "china-1", Label: "China 1", Region: proxy.AccountRegionChina, Enabled: true, HasCosyKey: true, HasEncryptInfo: true, UserID: "u1", MachineID: "m1"}}, nil
}

func TestAdminAccountReturnsPoolWithoutSecrets(t *testing.T) {
	handler := NewServer(Dependencies{
		Accounts: fakeAccountAdminProvider{},
		AccountConfig: config.AccountConfig{RoutingMode: "mixed", LoadBalance: "round_robin"},
		Models: fakeModels{},
		Sessions: fakeSessions{},
		Now: func() time.Time { return time.Unix(1, 0) },
	}, newVisionTestStore(t))

	req := httptest.NewRequest(http.MethodGet, "/admin/account", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"accounts"`) || !strings.Contains(body, `"routing_mode":"mixed"`) {
		t.Fatalf("missing account pool fields: %s", body)
	}
	if strings.Contains(body, "secret-cosy") || strings.Contains(body, "secret-info") {
		t.Fatalf("leaked secret: %s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/api -run TestAdminAccountReturnsPoolWithoutSecrets -count=1
```

Expected: FAIL because `Dependencies.AccountConfig` does not exist or handler does not return account pool fields.

- [ ] **Step 3: Extend dependencies and admin response**

In `internal/api/server.go`, add to `Dependencies`:

```go
AccountConfig config.AccountConfig
```

Import `lingma2api/internal/config`.

In `internal/api/admin_handlers.go`, add response types:

```go
type adminAccountPoolResponse struct {
	RoutingMode string                 `json:"routing_mode"`
	LoadBalance string                 `json:"load_balance"`
	Counts      map[string]int         `json:"counts"`
	Accounts    []proxy.AccountSummary `json:"accounts"`
	TokenStats  map[string]int         `json:"token_stats"`
}
```

At the top of `handleAdminAccount`, branch:

```go
if server.deps.Accounts != nil {
	summaries, err := server.deps.Accounts.Summaries(r.Context())
	if err != nil {
		writeMappedError(w, err)
		return
	}
	today, week, total := 0, 0, 0
	if server.db != nil {
		today, week, total, _ = server.db.GetTokenStats(r.Context())
	}
	writeJSON(w, http.StatusOK, adminAccountPoolResponse{
		RoutingMode: server.deps.AccountConfig.RoutingMode,
		LoadBalance: server.deps.AccountConfig.LoadBalance,
		Counts: accountSummaryCounts(summaries),
		Accounts: summaries,
		TokenStats: map[string]int{"today": today, "week": week, "total": total},
	})
	return
}
```

Add helper:

```go
func accountSummaryCounts(accounts []proxy.AccountSummary) map[string]int {
	counts := map[string]int{"total": len(accounts), "enabled": 0, "china": 0, "international": 0}
	for _, account := range accounts {
		if account.Enabled {
			counts["enabled"]++
		}
		switch account.Region {
		case proxy.AccountRegionChina:
			counts["china"]++
		case proxy.AccountRegionInternational:
			counts["international"]++
		}
	}
	return counts
}
```

- [ ] **Step 4: Add account-aware test connection**

In `handleAdminAccountTest`, branch when account dependencies exist:

```go
if server.deps.Accounts != nil && server.deps.Adapters != nil {
	accounts, err := server.deps.Accounts.Accounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, proxy.AccountTestResult{Success: false, Error: err.Error(), Timestamp: server.deps.Now().Format(time.RFC3339)})
		return
	}
	eligible := accounts
	if server.deps.AccountPool != nil {
		eligible = server.deps.AccountPool.Eligible(accounts)
	}
	if len(eligible) == 0 {
		writeJSON(w, http.StatusOK, proxy.AccountTestResult{Success: false, Error: "no eligible accounts", Timestamp: server.deps.Now().Format(time.RFC3339)})
		return
	}
	account := eligible[0]
	if id := r.URL.Query().Get("id"); id != "" {
		for _, candidate := range eligible {
			if candidate.ID == id {
				account = candidate
				break
			}
		}
	}
	adapter, err := server.deps.Adapters.ForRegion(account.Region)
	if err != nil {
		writeJSON(w, http.StatusOK, proxy.AccountTestResult{AccountID: account.ID, AccountLabel: account.Label, Region: account.Region, Success: false, Error: err.Error(), Timestamp: server.deps.Now().Format(time.RFC3339)})
		return
	}
	writeJSON(w, http.StatusOK, adapter.TestConnection(r.Context(), account))
	return
}
```

- [ ] **Step 5: Run admin tests**

Run:

```powershell
gofmt -w internal/api/server.go internal/api/admin_handlers.go internal/api/admin_accounts_test.go
go test ./internal/api -run "AdminAccount|AccountReturnsPool" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/api/server.go internal/api/admin_handlers.go internal/api/admin_accounts_test.go
git commit -m "feat: expose account pool admin status"
```

## Task 8: Bootstrap Writes Schema-V2 China Accounts

**Files:**
- Modify: `internal/proxy/accounts.go`
- Modify: `internal/proxy/accounts_test.go`
- Modify: `internal/api/bootstrap_handler.go`
- Modify: `internal/api/bootstrap_manager.go`
- Modify: `internal/api/bootstrap_remote_callback.go`
- Modify: `internal/api/bootstrap_remote_callback_test.go`

- [ ] **Step 1: Write failing account upsert test**

Add to `internal/proxy/accounts_test.go`:

```go
func TestAccountStoreUpsertsChinaAccountWithoutDeletingInternational(t *testing.T) {
	path := writeAccountCredentialFile(t, map[string]any{
		"schema_version": 2,
		"accounts": []any{
			map[string]any{"id": "intl-1", "label": "Intl", "region": "international", "enabled": true, "auth": map[string]string{"access_token": "at"}},
		},
	})
	store := NewAccountStore(config.CredentialConfig{AuthFile: path}, time.Now)

	err := store.UpsertAccount(context.Background(), StoredCredentialAccount{
		ID:      "china-1",
		Label:   "China",
		Region:  AccountRegionChina,
		Enabled: true,
		Auth: StoredAuthFields{
			CosyKey: "k", EncryptUserInfo: "info", UserID: "u", MachineID: "m",
		},
	})
	if err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	accounts, err := store.Accounts(context.Background())
	if err != nil {
		t.Fatalf("Accounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
go test ./internal/proxy -run TestAccountStoreUpsertsChinaAccountWithoutDeletingInternational -count=1
```

Expected: FAIL because `UpsertAccount` is undefined.

- [ ] **Step 3: Implement schema-v2 upsert**

In `internal/proxy/accounts.go`, add:

```go
func (s *AccountStore) UpsertAccount(ctx context.Context, account StoredCredentialAccount) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := StoredCredentialFile{SchemaVersion: 2}
	if data, err := os.ReadFile(s.cfg.AuthFile); err == nil {
		_ = json.Unmarshal(data, &stored)
		if stored.SchemaVersion < 2 {
			legacyAccounts, legacyErr := s.loadAccounts()
			if legacyErr == nil {
				for _, legacy := range legacyAccounts {
					stored.Accounts = append(stored.Accounts, legacy.ToStoredAccount())
				}
			}
			stored.SchemaVersion = 2
		}
	}
	if account.ID == "" {
		account.ID = stableAccountID(account.Auth.UserID, account.Auth.MachineID, string(account.Region))
	}
	if account.Label == "" {
		account.Label = string(account.Region) + " account"
	}
	replaced := false
	for i := range stored.Accounts {
		if stored.Accounts[i].ID == account.ID {
			stored.Accounts[i] = account
			replaced = true
			break
		}
	}
	if !replaced {
		stored.Accounts = append(stored.Accounts, account)
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.cfg.AuthFile, data, 0o600); err != nil {
		return err
	}
	s.loaded = false
	return nil
}

func (a AccountSnapshot) ToStoredAccount() StoredCredentialAccount {
	return StoredCredentialAccount{
		ID: a.ID, Label: a.Label, Region: a.Region, Enabled: a.Enabled,
		Source: a.Source, LingmaVersionHint: a.LingmaVersionHint, ObtainedAt: a.ObtainedAt, UpdatedAt: a.UpdatedAt,
		TokenExpireTime: fmt.Sprintf("%d", a.TokenExpireTime),
		Auth: StoredAuthFields{CosyKey: a.CosyKey, EncryptUserInfo: a.EncryptUserInfo, UserID: a.UserID, MachineID: a.MachineID, AccessToken: a.AccessToken},
		OAuth: StoredOAuthFields{AccessToken: a.AccessToken, RefreshToken: a.RefreshToken},
	}
}
```

- [ ] **Step 4: Thread region through bootstrap API**

In `internal/api/bootstrap_handler.go`, extend request body:

```go
var body struct {
	Method string `json:"method"`
	Region string `json:"region"`
}
```

For now, reject non-China bootstrap clearly:

```go
if body.Region == "international" {
	writeOpenAIError(w, http.StatusBadRequest, "international adapter protocol not configured")
	return
}
```

In `BootstrapManager`, add optional account store:

```go
AccountStore interface {
	UpsertAccount(context.Context, proxy.StoredCredentialAccount) error
}
```

Add field:

```go
Accounts AccountStore
```

In `buildStoredCredentialFromCallback`, after `proxy.StoredCredentialFile` is created, either keep returning the existing file or add a helper to convert the first account. In `SubmitCallbackURL` and `runRemoteCallbackFlow`, replace `auth.SaveCredentialFile(m.authFile, stored)` with:

```go
if m.Accounts != nil {
	if err := m.Accounts.UpsertAccount(context.Background(), storedFileToChinaAccount(stored)); err != nil {
		m.updateSession(id, "error", fmt.Sprintf("save account: %v", err))
		return m.GetStatus(id), err
	}
} else if err := auth.SaveCredentialFile(m.authFile, stored); err != nil {
	...
}
```

Add helper in `bootstrap_remote_callback.go`:

```go
func storedFileToChinaAccount(stored proxy.StoredCredentialFile) proxy.StoredCredentialAccount {
	return proxy.StoredCredentialAccount{
		ID: stableBootstrapAccountID(stored.Auth.UserID, stored.Auth.MachineID),
		Label: "China account",
		Region: proxy.AccountRegionChina,
		Enabled: true,
		Source: stored.Source,
		LingmaVersionHint: stored.LingmaVersionHint,
		ObtainedAt: stored.ObtainedAt,
		UpdatedAt: stored.UpdatedAt,
		TokenExpireTime: stored.TokenExpireTime,
		Auth: stored.Auth,
		OAuth: stored.OAuth,
	}
}
```

Use an existing hash helper or `proxy` helper for stable ID.

- [ ] **Step 5: Run tests**

Run:

```powershell
gofmt -w internal/proxy/accounts.go internal/proxy/accounts_test.go internal/api/bootstrap_handler.go internal/api/bootstrap_manager.go internal/api/bootstrap_remote_callback.go internal/api/bootstrap_remote_callback_test.go
go test ./internal/proxy -run Upserts -count=1
go test ./internal/api -run Bootstrap -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/proxy/accounts.go internal/proxy/accounts_test.go internal/api/bootstrap_handler.go internal/api/bootstrap_manager.go internal/api/bootstrap_remote_callback.go internal/api/bootstrap_remote_callback_test.go
git commit -m "feat: save bootstrap credentials as accounts"
```

## Task 9: Wire Main Runtime

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update runtime construction**

In `main.go`, create account-aware components after config is loaded:

```go
accountStore := proxy.NewAccountStore(cfg.Credential, time.Now)
accountPool := proxy.NewAccountPool(cfg.Account)
balancer := proxy.NewRoundRobinBalancer()
chinaSigner := proxy.NewSignatureEngine(proxy.SignatureOptions{CosyVersion: cfg.Lingma.CosyVersion})
chinaTransport := proxy.NewNativeTransport(firstNonEmpty(cfg.Account.ChinaBaseURL, cfg.Lingma.BaseURL), chinaSigner, 90*time.Second)
builder := proxy.NewBodyBuilder(cfg.Lingma.CosyVersion, time.Now, proxy.NewUUID, proxy.NewHexID)
adapters := proxy.NewAdapterRegistry()
adapters.Register(proxy.NewChinaAdapter(chinaTransport, builder, time.Now))
adapters.Register(proxy.NewInternationalAdapter(cfg.Account.InternationalBaseURL))
models := proxy.NewAccountModelService(accountStore, accountPool, adapters, proxy.DefaultAliases(), time.Now)
```

Keep legacy `credentials := proxy.NewCredentialManager(...)` available only for backward-compatible admin fields if needed during transition; prefer `accountStore` for new request routing.

Set bootstrap account store:

```go
bootstrapMgr.Accounts = accountStore
```

Pass new dependencies:

```go
Accounts: accountStore,
AccountPool: accountPool,
Balancer: balancer,
Adapters: adapters,
AccountConfig: cfg.Account,
```

Use `chinaTransport` for cleanup timeout updates and legacy uploader dependency if still needed.

- [ ] **Step 2: Run compile tests**

Run:

```powershell
gofmt -w main.go
go test ./...
```

Expected: PASS or only existing unrelated failures. Any compile error from changed interfaces must be fixed here.

- [ ] **Step 3: Commit**

```powershell
git add main.go
git commit -m "feat: wire account routing runtime"
```

## Task 10: Frontend Types And API Client

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add frontend types**

In `frontend/src/types/index.ts`, add:

```ts
export type AccountRegion = 'china' | 'international';
export type AccountRoutingMode = 'china_only' | 'international_only' | 'mixed';
export type AccountLoadBalance = 'round_robin';

export interface AccountSummary {
  id: string;
  label: string;
  region: AccountRegion;
  enabled: boolean;
  source?: string;
  lingma_version_hint?: string;
  obtained_at?: string;
  updated_at?: string;
  token_expire_time?: number;
  token_expired?: boolean;
  has_cosy_key: boolean;
  has_encrypt_user_info: boolean;
  has_access_token: boolean;
  has_refresh_token: boolean;
  user_id?: string;
  machine_id?: string;
  loaded_at?: string;
}

export interface AccountPoolData {
  routing_mode: AccountRoutingMode;
  load_balance: AccountLoadBalance;
  counts: {
    total: number;
    enabled: number;
    china: number;
    international: number;
  };
  accounts: AccountSummary[];
  token_stats: {
    today: number;
    week: number;
    total: number;
  };
}
```

Change `BootstrapMethod` to:

```ts
export type BootstrapMethod = 'remote_callback';
export type BootstrapRegion = 'china' | 'international';
```

- [ ] **Step 2: Update client helpers**

In `frontend/src/api/client.ts`, import `AccountPoolData` and `BootstrapRegion`, then change:

```ts
export const getAccount = () => request<AccountPoolData>('/admin/account');
export const refreshAccount = (id?: string) =>
  request<{ credential?: unknown; account?: unknown }>(`/admin/account/refresh${id ? `?id=${encodeURIComponent(id)}` : ''}`, { method: 'POST' });
export const startBootstrap = (method: BootstrapMethod = 'remote_callback', region: BootstrapRegion = 'china') =>
  request<BootstrapResponse>('/admin/account/bootstrap', {
    method: 'POST',
    body: JSON.stringify({ method, region }),
  });
export const testAccountConnection = (id?: string) =>
  request<AccountTestResult>(`/admin/account/test${id ? `?id=${encodeURIComponent(id)}` : ''}`, { method: 'POST' });
```

- [ ] **Step 3: Run type check through build**

Run:

```powershell
cd frontend
npm run build
```

Expected: FAIL because `Account.tsx` still expects old `AccountData`.

- [ ] **Step 4: Commit after Task 11, not now**

Do not commit this task alone if frontend build is failing. Carry these changes into Task 11 commit.

## Task 11: Account Pool UI

**Files:**
- Replace: `frontend/src/pages/Account.tsx`
- Modify: `frontend/tests/account-bootstrap.spec.ts`

- [ ] **Step 1: Write/update Playwright test expectations**

In `frontend/tests/account-bootstrap.spec.ts`, add or update a test that mocks `/admin/account`:

```ts
await page.route('**/admin/account', async route => {
  await route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      routing_mode: 'mixed',
      load_balance: 'round_robin',
      counts: { total: 2, enabled: 2, china: 1, international: 1 },
      accounts: [
        { id: 'china-1', label: '国内账号', region: 'china', enabled: true, has_cosy_key: true, has_encrypt_user_info: true, has_access_token: true, has_refresh_token: true, user_id: 'u1', machine_id: 'm1' },
        { id: 'intl-1', label: '国际账号', region: 'international', enabled: true, has_cosy_key: false, has_encrypt_user_info: false, has_access_token: true, has_refresh_token: false },
      ],
      token_stats: { today: 0, week: 0, total: 0 },
    }),
  });
});
await expect(page.getByRole('button', { name: /登录国内版 Lingma/ })).toBeVisible();
await expect(page.getByRole('button', { name: /登录国际版 Lingma/ })).toBeVisible();
await expect(page.getByText('混合使用')).toBeVisible();
await expect(page.getByText('账号平均轮询')).toBeVisible();
```

- [ ] **Step 2: Run Playwright test to verify it fails**

Run:

```powershell
cd frontend
npx playwright test tests/account-bootstrap.spec.ts
```

Expected: FAIL because current UI does not render the new account-pool controls.

- [ ] **Step 3: Replace Account page**

Replace `frontend/src/pages/Account.tsx` with a component that:

- calls `getAccount()` and stores `AccountPoolData`
- displays summary chips for routing mode and load-balance strategy
- renders two top buttons:
  - `登录国内版 Lingma`, calls `startBootstrap('remote_callback', 'china')`
  - `登录国际版 Lingma`, sets an inline warning panel saying international login protocol is not configured
- renders filters `全部`, `国内版`, `国际版`
- renders account rows with label, region, enabled status, user ID, machine ID, token state, credential booleans
- keeps existing bootstrap polling helpers for China login
- keeps callback URL submit UI for China login

Use this label map:

```ts
const routingLabels: Record<AccountRoutingMode, string> = {
  china_only: '只用国内版',
  international_only: '只用国际版',
  mixed: '混合使用',
};

const balanceLabels: Record<AccountLoadBalance, string> = {
  round_robin: '账号平均轮询',
};
```

Use lucide icons already imported in the project; good choices are `Globe2`, `ShieldCheck`, `RefreshCw`, `Zap`, `AlertTriangle`, `CheckCircle`, `X`, and `ClipboardPaste`.

- [ ] **Step 4: Run frontend build**

Run:

```powershell
cd frontend
npm run build
```

Expected: PASS.

- [ ] **Step 5: Run focused Playwright test**

Run:

```powershell
cd frontend
npx playwright test tests/account-bootstrap.spec.ts
```

Expected: PASS.

- [ ] **Step 6: Commit frontend work**

```powershell
git add frontend/src/types/index.ts frontend/src/api/client.ts frontend/src/pages/Account.tsx frontend/tests/account-bootstrap.spec.ts
git commit -m "feat: add account pool admin UI"
```

## Task 12: Full Verification And Documentation Review

**Files:**
- Review only unless failures require focused fixes.

- [ ] **Step 1: Run backend tests**

Run:

```powershell
go test ./internal/config ./internal/proxy ./internal/api
```

Expected: PASS.

- [ ] **Step 2: Run full Go suite**

Run:

```powershell
go test ./...
```

Expected: PASS. If this fails in unrelated packages, record the failing package and exact error before deciding whether to fix.

- [ ] **Step 3: Run frontend build**

Run:

```powershell
cd frontend
npm run build
```

Expected: PASS.

- [ ] **Step 4: Run account Playwright coverage**

Run:

```powershell
cd frontend
npx playwright test tests/account-bootstrap.spec.ts
```

Expected: PASS.

- [ ] **Step 5: Manual smoke test**

Run the server:

```powershell
go run . -config ./config.yaml
```

Open the admin console and verify:

- Account page shows routing mode and account counts.
- China login button starts the existing bootstrap flow.
- International login button shows the honest protocol-not-configured message.
- `/v1/models` works with a valid China account.
- `/v1/chat/completions` works with `routing_mode: china_only` and a valid China account.

## Self-Review Notes

- Spec coverage: config, schema-v2 credentials, schema-v1 compatibility, account-average balancing, adapter boundary, China adapter preservation, International unsupported-protocol behavior, admin API, UI, tests, and migration are covered.
- Intentional constraint: International protocol remains unsupported until real request samples exist; the adapter and UI report this explicitly.
- Type consistency: plan uses `AccountRegion`, `AccountSnapshot`, `AccountSummary`, `AccountStore`, `AccountPool`, `RoundRobinBalancer`, `RegionAdapter`, and `AdapterRegistry` consistently across backend tasks.
- Verification path: each backend layer has a red-green test, followed by focused package tests and final full-suite verification.
