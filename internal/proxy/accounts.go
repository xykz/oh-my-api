package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type AccountStore struct {
	mu       sync.RWMutex
	cfg      config.CredentialConfig
	now      func() time.Time
	accounts []AccountSnapshot
	loaded   bool
}

func NewAccountStore(cfg config.CredentialConfig, now func() time.Time) *AccountStore {
	if now == nil {
		now = time.Now
	}
	if cfg.AuthFile == "" {
		cfg.AuthFile = "./auth/credentials.json"
	}
	return &AccountStore{
		cfg: cfg,
		now: now,
	}
}

func (store *AccountStore) Accounts(ctx context.Context) ([]AccountSnapshot, error) {
	store.mu.RLock()
	accounts := cloneAccountSnapshots(store.accounts)
	loaded := store.loaded
	store.mu.RUnlock()

	if loaded {
		return accounts, nil
	}
	return store.Refresh(ctx)
}

func (store *AccountStore) Refresh(ctx context.Context) ([]AccountSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	accounts, err := store.loadAccounts(ctx)
	if err != nil {
		return nil, err
	}

	store.accounts = accounts
	store.loaded = true
	return cloneAccountSnapshots(accounts), nil
}

func (store *AccountStore) Summaries(ctx context.Context) ([]AccountSummary, error) {
	accounts, err := store.Accounts(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]AccountSummary, len(accounts))
	for i, account := range accounts {
		summaries[i] = account.Summary(5 * time.Minute)
	}
	return summaries, nil
}

func (store *AccountStore) UpsertAccount(ctx context.Context, account StoredCredentialAccount) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	stored := StoredCredentialFile{SchemaVersion: 2}
	data, readErr := os.ReadFile(store.cfg.AuthFile)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(data, &stored); err != nil {
			return fmt.Errorf("%w: parse auth file: %v", ErrCredentialsUnavailable, err)
		}
		if stored.SchemaVersion < 2 {
			legacyAccounts, err := store.loadLegacy(stored)
			if err != nil {
				return err
			}
			stored = StoredCredentialFile{SchemaVersion: 2}
			for _, legacy := range legacyAccounts {
				stored.Accounts = append(stored.Accounts, legacy.ToStoredAccount())
			}
		}
	case os.IsNotExist(readErr):
	default:
		return fmt.Errorf("%w: read auth file: %v", ErrCredentialsUnavailable, readErr)
	}

	account, err := defaultStoredAccount(account)
	if err != nil {
		return err
	}
	candidate := stored
	replaced := false
	for i := range candidate.Accounts {
		if candidate.Accounts[i].ID == account.ID {
			candidate.Accounts[i] = account
			replaced = true
			break
		}
	}
	if !replaced {
		candidate.Accounts = append(candidate.Accounts, account)
	}
	candidate.SchemaVersion = 2

	if _, err := schemaV2AccountSnapshots(candidate, store.now()); err != nil {
		return err
	}
	authDir := filepath.Dir(store.cfg.AuthFile)
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		return err
	}
	data, err = json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic0600(store.cfg.AuthFile, data); err != nil {
		return err
	}

	store.loaded = false
	store.accounts = nil
	return nil
}

func (store *AccountStore) loadAccounts(ctx context.Context) ([]AccountSnapshot, error) {
	if store.cfg.AuthFile == "" {
		return nil, fmt.Errorf("%w: missing auth_file", ErrCredentialsUnavailable)
	}

	data, err := os.ReadFile(store.cfg.AuthFile)
	if err != nil {
		return nil, fmt.Errorf("%w: read auth file: %v", ErrCredentialsUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var stored StoredCredentialFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("%w: parse auth file: %v", ErrCredentialsUnavailable, err)
	}

	if stored.SchemaVersion == 2 {
		return store.loadSchemaV2(stored)
	}
	return store.loadLegacy(stored)
}

func (store *AccountStore) loadLegacy(stored StoredCredentialFile) ([]AccountSnapshot, error) {
	if stored.Source == "" {
		stored.Source = "project_auth_file"
	}

	account := AccountSnapshot{
		ID:                generatedAccountID(StoredCredentialAccount{Region: AccountRegionChina, Auth: stored.Auth}),
		Region:            AccountRegionChina,
		Enabled:           true,
		CosyKey:           stored.Auth.CosyKey,
		EncryptUserInfo:   stored.Auth.EncryptUserInfo,
		UserID:            stored.Auth.UserID,
		MachineID:         stored.Auth.MachineID,
		AccessToken:       firstNonEmpty(stored.Auth.AccessToken, stored.OAuth.AccessToken),
		RefreshToken:      stored.OAuth.RefreshToken,
		Source:            stored.Source,
		LingmaVersionHint: stored.LingmaVersionHint,
		ObtainedAt:        stored.ObtainedAt,
		UpdatedAt:         stored.UpdatedAt,
		TokenExpireTime:   parseExpireTime(stored.TokenExpireTime),
		LoadedAt:          store.now(),
	}
	if err := validateAccountSnapshot(account); err != nil {
		return nil, err
	}
	return []AccountSnapshot{account}, nil
}

func (store *AccountStore) loadSchemaV2(stored StoredCredentialFile) ([]AccountSnapshot, error) {
	return schemaV2AccountSnapshots(stored, store.now())
}

func schemaV2AccountSnapshots(stored StoredCredentialFile, loadedAt time.Time) ([]AccountSnapshot, error) {
	if len(stored.Accounts) == 0 {
		return nil, fmt.Errorf("%w: no accounts", ErrCredentialsUnavailable)
	}

	accounts := make([]AccountSnapshot, 0, len(stored.Accounts))
	seenIDs := make(map[string]struct{}, len(stored.Accounts))
	for _, storedAccount := range stored.Accounts {
		source := firstNonEmpty(storedAccount.Source, stored.Source, "project_auth_file")
		tokenExpireTime := firstNonEmpty(storedAccount.TokenExpireTime, stored.TokenExpireTime)
		id, err := accountID(storedAccount)
		if err != nil {
			return nil, err
		}
		if _, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("%w: duplicate account id %q", ErrCredentialsUnavailable, id)
		}
		seenIDs[id] = struct{}{}

		account := AccountSnapshot{
			ID:                id,
			Label:             storedAccount.Label,
			Region:            storedAccount.Region,
			Enabled:           storedAccount.Enabled,
			CosyKey:           storedAccount.Auth.CosyKey,
			EncryptUserInfo:   storedAccount.Auth.EncryptUserInfo,
			UserID:            storedAccount.Auth.UserID,
			MachineID:         storedAccount.Auth.MachineID,
			AccessToken:       firstNonEmpty(storedAccount.Auth.AccessToken, storedAccount.OAuth.AccessToken),
			RefreshToken:      storedAccount.OAuth.RefreshToken,
			Source:            source,
			LingmaVersionHint: firstNonEmpty(storedAccount.LingmaVersionHint, stored.LingmaVersionHint),
			ObtainedAt:        firstNonEmpty(storedAccount.ObtainedAt, stored.ObtainedAt),
			UpdatedAt:         firstNonEmpty(storedAccount.UpdatedAt, stored.UpdatedAt),
			TokenExpireTime:   parseExpireTime(tokenExpireTime),
			LoadedAt:          loadedAt,
		}
		if err := validateAccountSnapshot(account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func validateAccountSnapshot(account AccountSnapshot) error {
	switch account.Region {
	case AccountRegionChina:
		return validateChinaAccount(account)
	case AccountRegionInternational:
		if account.AccessToken == "" && account.CosyKey == "" {
			return fmt.Errorf("%w: account %q missing access_token or cosy_key", ErrCredentialsUnavailable, account.ID)
		}
		return nil
	case AccountRegionCodeBuddy:
		if account.AccessToken == "" {
			return fmt.Errorf("%w: account %q missing access_token", ErrCredentialsUnavailable, account.ID)
		}
		return nil
	default:
		return fmt.Errorf("%w: account %q unknown region %q", ErrCredentialsUnavailable, account.ID, account.Region)
	}
}

func validateChinaAccount(account AccountSnapshot) error {
	var missing []string
	if account.CosyKey == "" {
		missing = append(missing, "cosy_key")
	}
	if account.EncryptUserInfo == "" {
		missing = append(missing, "encrypt_user_info")
	}
	if account.UserID == "" {
		missing = append(missing, "user_id")
	}
	if account.MachineID == "" {
		missing = append(missing, "machine_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: account %q missing %s", ErrCredentialsUnavailable, account.ID, missing[0])
	}
	return nil
}

func accountID(account StoredCredentialAccount) (string, error) {
	if account.ID != "" {
		return account.ID, nil
	}
	if account.Region == AccountRegionCodeBuddy {
		if account.Auth.AccessToken == "" {
			return "", fmt.Errorf("%w: codebuddy account missing access_token for generated id", ErrCredentialsUnavailable)
		}
		return generatedAccountID(account), nil
	}
	if account.Region == "" || account.Auth.UserID == "" || account.Auth.MachineID == "" {
		return "", fmt.Errorf("%w: account missing stable identity for generated id", ErrCredentialsUnavailable)
	}
	return generatedAccountID(account), nil
}

func generatedAccountID(account StoredCredentialAccount) string {
	var parts []string
	if account.Region == AccountRegionCodeBuddy {
		parts = []string{
			string(account.Region),
			account.Label,
			account.Auth.AccessToken,
		}
	} else {
		parts = []string{
			string(account.Region),
			account.Auth.UserID,
			account.Auth.MachineID,
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "acct-" + hex.EncodeToString(sum[:8])
}

func defaultStoredAccount(account StoredCredentialAccount) (StoredCredentialAccount, error) {
	if account.Region == "" {
		account.Region = AccountRegionChina
	}
	if account.ID == "" {
		id, err := accountID(account)
		if err != nil {
			return StoredCredentialAccount{}, err
		}
		account.ID = id
	}
	if account.Label == "" {
		switch account.Region {
		case AccountRegionChina:
			account.Label = "China account"
		case AccountRegionInternational:
			account.Label = "International account"
		default:
			account.Label = string(account.Region) + " account"
		}
	}
	if !account.Enabled {
		account.Enabled = true
	}
	return account, nil
}

func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary auth file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temporary auth file: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temporary auth file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temporary auth file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temporary auth file: %w", err)
	}
	if err := replaceFile(tempPath, path); err != nil {
		return fmt.Errorf("replace auth file: %w", err)
	}
	cleanup = false
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneAccountSnapshots(accounts []AccountSnapshot) []AccountSnapshot {
	if len(accounts) == 0 {
		return nil
	}
	cloned := make([]AccountSnapshot, len(accounts))
	copy(cloned, accounts)
	return cloned
}
