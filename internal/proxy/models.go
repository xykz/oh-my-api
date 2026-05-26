package proxy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type remoteModelFetcher interface {
	ListModels(context.Context, CredentialSnapshot) ([]RemoteModel, error)
}

type credentialReader interface {
	Current(context.Context) (CredentialSnapshot, error)
}

type accountModelReader interface {
	Accounts(context.Context) ([]AccountSnapshot, error)
}

type ModelService struct {
	mu            sync.RWMutex
	transport     remoteModelFetcher
	credentials   credentialReader
	accounts      accountModelReader
	accountPool   *AccountPool
	adapters      *AdapterRegistry
	aliases       map[string]string
	modelsByKey   map[string]RemoteModel
	regionsByKey  map[string][]AccountRegion
	accountsByKey map[string][]string
	fetchedAt     time.Time
	lastError     string
	now           func() time.Time
}

func NewModelService(transport remoteModelFetcher, credentials credentialReader, aliases map[string]string, now func() time.Time) *ModelService {
	if now == nil {
		now = time.Now
	}
	if aliases == nil {
		aliases = DefaultAliases()
	}
	return &ModelService{
		transport:     transport,
		credentials:   credentials,
		aliases:       aliases,
		modelsByKey:   make(map[string]RemoteModel),
		regionsByKey:  make(map[string][]AccountRegion),
		accountsByKey: make(map[string][]string),
		now:           now,
	}
}

func NewAccountModelService(accounts accountModelReader, pool *AccountPool, adapters *AdapterRegistry, aliases map[string]string, now func() time.Time) *ModelService {
	if now == nil {
		now = time.Now
	}
	if aliases == nil {
		aliases = DefaultAliases()
	}
	if pool == nil {
		pool = NewAccountPool(config.AccountConfig{RoutingMode: "mixed"})
	}
	return &ModelService{
		accounts:      accounts,
		accountPool:   pool,
		adapters:      adapters,
		aliases:       aliases,
		modelsByKey:   make(map[string]RemoteModel),
		regionsByKey:  make(map[string][]AccountRegion),
		accountsByKey: make(map[string][]string),
		now:           now,
	}
}

func (service *ModelService) ResolveChatModel(ctx context.Context, requested string) (string, error) {
	if requested == "" || requested == "auto" {
		return "", nil
	}
	if mapped, ok := service.aliases[requested]; ok {
		return mapped, nil
	}

	service.mu.RLock()
	_, cached := service.modelsByKey[requested]
	cachedCount := len(service.modelsByKey)
	service.mu.RUnlock()
	if cached {
		return requested, nil
	}

	if cachedCount == 0 {
		_ = service.Refresh(ctx)
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	if _, ok := service.modelsByKey[requested]; ok {
		return requested, nil
	}
	if len(service.modelsByKey) == 0 {
		return requested, nil
	}
	return "", ErrUnknownModel
}

func (service *ModelService) AvailableRegions(ctx context.Context, modelKey string) ([]AccountRegion, bool, error) {
	if modelKey == "" || service.accounts == nil {
		return nil, false, nil
	}

	service.mu.RLock()
	hasCache := len(service.regionsByKey) > 0
	service.mu.RUnlock()
	if !hasCache {
		if err := service.Refresh(ctx); err != nil {
			return nil, true, err
		}
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	regions, ok := service.regionsByKey[modelKey]
	if !ok {
		return []AccountRegion{}, true, nil
	}
	out := make([]AccountRegion, len(regions))
	copy(out, regions)
	return out, true, nil
}

func (service *ModelService) AvailableAccounts(ctx context.Context, modelKey string) ([]string, bool, error) {
	if modelKey == "" || service.accounts == nil {
		return nil, false, nil
	}

	service.mu.RLock()
	hasCache := len(service.accountsByKey) > 0
	service.mu.RUnlock()
	if !hasCache {
		if err := service.Refresh(ctx); err != nil {
			return nil, true, err
		}
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	accountIDs, ok := service.accountsByKey[modelKey]
	if !ok {
		return []string{}, true, nil
	}
	out := make([]string, len(accountIDs))
	copy(out, accountIDs)
	return out, true, nil
}

func (service *ModelService) ListModels(ctx context.Context) ([]OpenAIModel, error) {
	service.mu.RLock()
	hasCache := len(service.modelsByKey) > 0
	service.mu.RUnlock()
	if !hasCache {
		if err := service.Refresh(ctx); err != nil {
			return nil, err
		}
	}

	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.buildOpenAIModelsLocked(), nil
}

func (service *ModelService) Refresh(ctx context.Context) error {
	if service.accounts != nil {
		return service.refreshAccounts(ctx)
	}
	if service.transport == nil || service.credentials == nil {
		return nil
	}

	credential, err := service.credentials.Current(ctx)
	if err != nil {
		service.recordError(err.Error())
		return err
	}
	models, err := service.transport.ListModels(ctx, credential)
	if err != nil {
		service.recordError(err.Error())
		return err
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	service.modelsByKey = make(map[string]RemoteModel, len(models))
	service.regionsByKey = make(map[string][]AccountRegion)
	service.accountsByKey = make(map[string][]string)
	for _, model := range models {
		if model.Key == "" {
			continue
		}
		service.modelsByKey[model.Key] = model
	}
	service.fetchedAt = service.now()
	service.lastError = ""
	return nil
}

func (service *ModelService) refreshAccounts(ctx context.Context) error {
	accounts, err := service.accounts.Accounts(ctx)
	if err != nil {
		service.recordError(err.Error())
		return err
	}

	pool := service.accountPool
	if pool == nil {
		pool = NewAccountPool(config.AccountConfig{RoutingMode: "mixed"})
	}
	eligible := pool.Eligible(accounts)
	if len(eligible) == 0 {
		err := fmt.Errorf("%w: no eligible accounts", ErrCredentialsUnavailable)
		service.recordError(err.Error())
		return err
	}

	modelsByKey := make(map[string]RemoteModel)
	regionsByKey := make(map[string]map[AccountRegion]struct{})
	accountsByKey := make(map[string]map[string]struct{})
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
			if model.Key == "" {
				continue
			}
			modelsByKey[model.Key] = model
			if regionsByKey[model.Key] == nil {
				regionsByKey[model.Key] = make(map[AccountRegion]struct{})
			}
			regionsByKey[model.Key][account.Region] = struct{}{}
			if accountsByKey[model.Key] == nil {
				accountsByKey[model.Key] = make(map[string]struct{})
			}
			accountsByKey[model.Key][account.ID] = struct{}{}
		}
	}

	if len(modelsByKey) == 0 && lastErr != nil {
		service.recordError(lastErr.Error())
		return lastErr
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	service.modelsByKey = modelsByKey
	service.regionsByKey = flattenModelRegions(regionsByKey)
	service.accountsByKey = flattenModelAccounts(accountsByKey)
	service.fetchedAt = service.now()
	service.lastError = ""
	return nil
}

func flattenModelRegions(regionsByKey map[string]map[AccountRegion]struct{}) map[string][]AccountRegion {
	out := make(map[string][]AccountRegion, len(regionsByKey))
	for key, regionSet := range regionsByKey {
		regions := make([]AccountRegion, 0, len(regionSet))
		for region := range regionSet {
			regions = append(regions, region)
		}
		sort.Slice(regions, func(i, j int) bool {
			return regions[i] < regions[j]
		})
		out[key] = regions
	}
	return out
}

func flattenModelAccounts(accountsByKey map[string]map[string]struct{}) map[string][]string {
	out := make(map[string][]string, len(accountsByKey))
	for key, accountSet := range accountsByKey {
		accountIDs := make([]string, 0, len(accountSet))
		for accountID := range accountSet {
			accountIDs = append(accountIDs, accountID)
		}
		sort.Strings(accountIDs)
		out[key] = accountIDs
	}
	return out
}

func (service *ModelService) Status() ModelStatus {
	service.mu.RLock()
	defer service.mu.RUnlock()

	return ModelStatus{
		FetchedAt: service.fetchedAt,
		Cached:    len(service.modelsByKey) > 0,
		Count:     len(service.modelsByKey),
		LastError: service.lastError,
	}
}

func (service *ModelService) recordError(message string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.lastError = message
}

func (service *ModelService) buildOpenAIModelsLocked() []OpenAIModel {
	keys := make([]string, 0, len(service.modelsByKey))
	for key := range service.modelsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	models := make([]OpenAIModel, 0, len(keys)+len(service.aliases))
	for _, key := range keys {
		models = append(models, OpenAIModel{
			ID:      key,
			Object:  "model",
			OwnedBy: "lingma",
		})
	}

	aliasKeys := make([]string, 0, len(service.aliases))
	for alias := range service.aliases {
		aliasKeys = append(aliasKeys, alias)
	}
	sort.Strings(aliasKeys)
	for _, alias := range aliasKeys {
		models = append(models, OpenAIModel{
			ID:      alias,
			Object:  "model",
			OwnedBy: "lingma-alias",
		})
	}
	return models
}
