package proxy

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type stubTransport struct {
	models []RemoteModel
}

func (transport stubTransport) ListModels(_ context.Context, _ CredentialSnapshot) ([]RemoteModel, error) {
	return transport.models, nil
}

type stubCredentialReader struct{}

func (stubCredentialReader) Current(_ context.Context) (CredentialSnapshot, error) {
	return CredentialSnapshot{
		CosyKey:         "k",
		EncryptUserInfo: "info",
		UserID:          "u",
		MachineID:       "m",
	}, nil
}

func TestResolveChatModelMapsAutoToEmptyKey(t *testing.T) {
	service := NewModelService(stubTransport{
		models: []RemoteModel{{Key: "dashscope_qwen3_coder"}},
	}, stubCredentialReader{}, DefaultAliases(), nil)

	got, err := service.ResolveChatModel(context.Background(), "auto")
	if err != nil {
		t.Fatalf("ResolveChatModel() error = %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestAccountModelServiceAggregatesModelsAcrossEligibleAccounts(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
		{ID: "china-disabled", Region: AccountRegionChina, Enabled: false},
	}}
	adapters := NewAdapterRegistry()
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionChina,
		models: []RemoteModel{
			{Key: "china-coder"},
			{Key: ""},
			{Key: "shared-model", DisplayName: "china shared"},
		},
	})
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionInternational,
		models: []RemoteModel{
			{Key: "intl-coder"},
			{Key: "shared-model", DisplayName: "intl shared"},
		},
	})
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), adapters, map[string]string{"alias-coder": "china-coder"}, func() time.Time {
		return time.Unix(10, 0)
	})

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}

	got := modelIDs(models)
	want := []string{"china-coder", "intl-coder", "shared-model", "alias-coder"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model ids = %v, want %v", got, want)
	}
	status := service.Status()
	if !status.FetchedAt.Equal(time.Unix(10, 0)) {
		t.Fatalf("FetchedAt = %v, want %v", status.FetchedAt, time.Unix(10, 0))
	}
	if status.Count != 3 {
		t.Fatalf("status count = %d, want 3", status.Count)
	}
}

func TestAccountModelServiceAvailableRegionsReturnsRegionsForModel(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
	}}
	adapters := NewAdapterRegistry()
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionChina,
		models: []RemoteModel{{Key: "china-coder"}, {Key: "shared-model"}},
	})
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionInternational,
		models: []RemoteModel{{Key: "intl-coder"}, {Key: "shared-model"}},
	})
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), adapters, nil, nil)

	regions, ok, err := service.AvailableRegions(context.Background(), "intl-coder")
	if err != nil {
		t.Fatalf("AvailableRegions() error = %v", err)
	}
	if !ok {
		t.Fatal("AvailableRegions() ok = false, want true")
	}
	if want := []AccountRegion{AccountRegionInternational}; !reflect.DeepEqual(regions, want) {
		t.Fatalf("AvailableRegions() = %v, want %v", regions, want)
	}

	regions, ok, err = service.AvailableRegions(context.Background(), "shared-model")
	if err != nil {
		t.Fatalf("AvailableRegions(shared-model) error = %v", err)
	}
	if !ok {
		t.Fatal("AvailableRegions(shared-model) ok = false, want true")
	}
	if want := []AccountRegion{AccountRegionChina, AccountRegionInternational}; !reflect.DeepEqual(regions, want) {
		t.Fatalf("AvailableRegions(shared-model) = %v, want %v", regions, want)
	}
}

func TestAccountModelServiceAvailableAccountsReturnsAccountsForModel(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "china-2", Region: AccountRegionChina, Enabled: true},
		{ID: "china-disabled", Region: AccountRegionChina, Enabled: false},
	}}
	adapters := NewAdapterRegistry()
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionChina,
		modelsByAccount: map[string][]RemoteModel{
			"china-1":        {{Key: "shared-model"}},
			"china-2":        {{Key: "target-model"}, {Key: "shared-model"}},
			"china-disabled": {{Key: "target-model"}},
		},
	})
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), adapters, nil, nil)

	accountIDs, ok, err := service.AvailableAccounts(context.Background(), "target-model")
	if err != nil {
		t.Fatalf("AvailableAccounts() error = %v", err)
	}
	if !ok {
		t.Fatal("AvailableAccounts() ok = false, want true")
	}
	if want := []string{"china-2"}; !reflect.DeepEqual(accountIDs, want) {
		t.Fatalf("AvailableAccounts() = %v, want %v", accountIDs, want)
	}

	accountIDs, ok, err = service.AvailableAccounts(context.Background(), "shared-model")
	if err != nil {
		t.Fatalf("AvailableAccounts(shared-model) error = %v", err)
	}
	if !ok {
		t.Fatal("AvailableAccounts(shared-model) ok = false, want true")
	}
	if want := []string{"china-1", "china-2"}; !reflect.DeepEqual(accountIDs, want) {
		t.Fatalf("AvailableAccounts(shared-model) = %v, want %v", accountIDs, want)
	}
}

func TestModelServiceAvailableRegionsUnavailableForLegacyService(t *testing.T) {
	service := NewModelService(stubTransport{
		models: []RemoteModel{{Key: "dashscope_qwen3_coder"}},
	}, stubCredentialReader{}, DefaultAliases(), nil)

	regions, ok, err := service.AvailableRegions(context.Background(), "dashscope_qwen3_coder")
	if err != nil {
		t.Fatalf("AvailableRegions() error = %v", err)
	}
	if ok {
		t.Fatalf("AvailableRegions() ok = true, want false")
	}
	if regions != nil {
		t.Fatalf("AvailableRegions() regions = %v, want nil", regions)
	}
}

func TestAccountModelServiceRefreshReturnsCredentialsUnavailableWhenNoEligibleAccounts(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-disabled", Region: AccountRegionChina, Enabled: false},
	}}
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), NewAdapterRegistry(), nil, nil)

	err := service.Refresh(context.Background())

	if !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("Refresh() error = %v, want ErrCredentialsUnavailable", err)
	}
	if service.Status().LastError == "" {
		t.Fatal("LastError should be recorded")
	}
}

func TestAccountModelServiceRefreshSucceedsWithPartialAdapterErrors(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
	}}
	adapters := NewAdapterRegistry()
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionChina,
		err:    errors.New("china failed"),
	})
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionInternational,
		models: []RemoteModel{{Key: "intl-coder"}},
	})
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), adapters, nil, nil)

	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	status := service.Status()
	if status.LastError != "" {
		t.Fatalf("LastError = %q, want empty", status.LastError)
	}
	if status.Count != 1 {
		t.Fatalf("status count = %d, want 1", status.Count)
	}
}

func TestAccountModelServiceRefreshReturnsLastErrorWhenNoModelsCollected(t *testing.T) {
	accounts := fakeAccountReader{accounts: []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
	}}
	adapters := NewAdapterRegistry()
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionChina,
		err:    errors.New("china failed"),
	})
	mustRegisterAdapter(t, adapters, fakeRegionAdapter{
		region: AccountRegionInternational,
		err:    errors.New("intl failed"),
	})
	service := NewAccountModelService(accounts, NewAccountPool(config.AccountConfig{
		RoutingMode: "mixed",
	}), adapters, nil, nil)

	err := service.Refresh(context.Background())

	if err == nil || err.Error() != "intl failed" {
		t.Fatalf("Refresh() error = %v, want last adapter error", err)
	}
	if service.Status().LastError != "intl failed" {
		t.Fatalf("LastError = %q, want last adapter error", service.Status().LastError)
	}
}

type fakeAccountReader struct {
	accounts []AccountSnapshot
	err      error
}

func (reader fakeAccountReader) Accounts(context.Context) ([]AccountSnapshot, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	out := make([]AccountSnapshot, len(reader.accounts))
	copy(out, reader.accounts)
	return out, nil
}

type fakeRegionAdapter struct {
	region          AccountRegion
	models          []RemoteModel
	modelsByAccount map[string][]RemoteModel
	err             error
}

func (adapter fakeRegionAdapter) Region() AccountRegion {
	return adapter.region
}

func (adapter fakeRegionAdapter) ListModels(_ context.Context, account AccountSnapshot) ([]RemoteModel, error) {
	if adapter.err != nil {
		return nil, adapter.err
	}
	if adapter.modelsByAccount != nil {
		return adapter.modelsByAccount[account.ID], nil
	}
	return adapter.models, nil
}

func (fakeRegionAdapter) BuildChatRequest(context.Context, CanonicalRequest, string, AccountSnapshot) (RemoteChatRequest, error) {
	return RemoteChatRequest{}, nil
}

func (fakeRegionAdapter) StreamChat(context.Context, RemoteChatRequest, AccountSnapshot) (io.ReadCloser, error) {
	return nil, nil
}

func (fakeRegionAdapter) UploadImage(context.Context, AccountSnapshot, string) (string, error) {
	return "", nil
}

func (fakeRegionAdapter) TestConnection(context.Context, AccountSnapshot) AccountTestResult {
	return AccountTestResult{Success: true}
}

func mustRegisterAdapter(t *testing.T, registry *AdapterRegistry, adapter RegionAdapter) {
	t.Helper()
	if err := registry.Register(adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

func modelIDs(models []OpenAIModel) []string {
	ids := make([]string, len(models))
	for i, model := range models {
		ids[i] = model.ID
	}
	return ids
}
