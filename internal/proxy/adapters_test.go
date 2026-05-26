package proxy

import (
	"errors"
	"testing"
	"time"
)

func TestAdapterRegistryReturnsAdapterByRegion(t *testing.T) {
	registry := NewAdapterRegistry()
	transport := NewNativeTransport("https://api.lingma.ai", nil, 0)
	builder := NewBodyBuilder("", nil, nil, nil)
	adapter, err := NewInternationalAdapter(transport, builder, nil)
	if err != nil {
		t.Fatalf("NewInternationalAdapter() error = %v", err)
	}
	if err := registry.Register(adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	found, err := registry.ForRegion(AccountRegionInternational)
	if err != nil {
		t.Fatalf("ForRegion() error = %v", err)
	}
	if found.Region() != AccountRegionInternational {
		t.Fatalf("Region() = %q, want %q", found.Region(), AccountRegionInternational)
	}
}

func TestAdapterRegistryRegisterRejectsNilAdapter(t *testing.T) {
	registry := NewAdapterRegistry()

	err := registry.Register(nil)
	if !errors.Is(err, ErrAdapterNil) {
		t.Fatalf("Register() error = %v, want ErrAdapterNil", err)
	}

	var adapter *LingmaAdapter
	err = registry.Register(adapter)
	if !errors.Is(err, ErrAdapterNil) {
		t.Fatalf("Register() typed nil error = %v, want ErrAdapterNil", err)
	}
}

func TestAdapterRegistryZeroValueIsSafe(t *testing.T) {
	var registry AdapterRegistry

	_, err := registry.ForRegion(AccountRegionChina)
	if !errors.Is(err, ErrAdapterProtocolNotConfigured) {
		t.Fatalf("ForRegion() error = %v, want ErrAdapterProtocolNotConfigured", err)
	}

	transport := NewNativeTransport("https://api.lingma.ai", nil, 0)
	builder := NewBodyBuilder("", nil, nil, nil)
	adapter, err := NewInternationalAdapter(transport, builder, nil)
	if err != nil {
		t.Fatalf("NewInternationalAdapter() error = %v", err)
	}
	if err := registry.Register(adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	found, err := registry.ForRegion(AccountRegionInternational)
	if err != nil {
		t.Fatalf("ForRegion() error = %v", err)
	}
	if found.Region() != AccountRegionInternational {
		t.Fatalf("Region() = %q, want %q", found.Region(), AccountRegionInternational)
	}
}

func TestNewChinaAdapterRequiresTransport(t *testing.T) {
	adapter, err := NewChinaAdapter(nil, NewBodyBuilder("", nil, nil, nil), nil)
	if !errors.Is(err, ErrChinaAdapterNilTransport) {
		t.Fatalf("NewChinaAdapter() error = %v, want ErrChinaAdapterNilTransport", err)
	}
	if adapter != nil {
		t.Fatalf("NewChinaAdapter() adapter = %#v, want nil", adapter)
	}
}

func TestNewChinaAdapterRequiresBodyBuilder(t *testing.T) {
	transport := NewNativeTransport("https://example.com", nil, 0)

	adapter, err := NewChinaAdapter(transport, nil, nil)
	if !errors.Is(err, ErrChinaAdapterNilBuilder) {
		t.Fatalf("NewChinaAdapter() error = %v, want ErrChinaAdapterNilBuilder", err)
	}
	if adapter != nil {
		t.Fatalf("NewChinaAdapter() adapter = %#v, want nil", adapter)
	}
}

func TestNewLingmaAdapterRequiresTransport(t *testing.T) {
	_, err := NewLingmaAdapter(AccountRegionChina, nil, NewBodyBuilder("", nil, nil, nil), nil)
	if !errors.Is(err, ErrLingmaAdapterNilTransport) {
		t.Fatalf("error = %v, want ErrLingmaAdapterNilTransport", err)
	}
}

func TestNewLingmaAdapterRequiresBuilder(t *testing.T) {
	transport := NewNativeTransport("https://example.com", nil, 0)
	_, err := NewLingmaAdapter(AccountRegionInternational, transport, nil, nil)
	if !errors.Is(err, ErrLingmaAdapterNilBuilder) {
		t.Fatalf("error = %v, want ErrLingmaAdapterNilBuilder", err)
	}
}

func TestLingmaAdapterRegion(t *testing.T) {
	transport := NewNativeTransport("https://example.com", nil, 0)
	builder := NewBodyBuilder("", nil, nil, nil)

	chinaAdapter, err := NewLingmaAdapter(AccountRegionChina, transport, builder, nil)
	if err != nil {
		t.Fatalf("NewLingmaAdapter() error = %v", err)
	}
	if chinaAdapter.Region() != AccountRegionChina {
		t.Fatalf("Region() = %q, want %q", chinaAdapter.Region(), AccountRegionChina)
	}

	intlAdapter, err := NewLingmaAdapter(AccountRegionInternational, transport, builder, nil)
	if err != nil {
		t.Fatalf("NewLingmaAdapter() error = %v", err)
	}
	if intlAdapter.Region() != AccountRegionInternational {
		t.Fatalf("Region() = %q, want %q", intlAdapter.Region(), AccountRegionInternational)
	}
}

func TestAccountSnapshotToCredentialSnapshotPreservesNativeCredentialFields(t *testing.T) {
	loadedAt := time.Unix(100, 0)
	account := AccountSnapshot{
		CosyKey:         "cosy-key",
		EncryptUserInfo: "encrypt-user-info",
		UserID:          "user-id",
		MachineID:       "machine-id",
		Source:          "test-source",
		LoadedAt:        loadedAt,
		TokenExpireTime: 12345,
	}

	credential := account.ToCredentialSnapshot()

	if credential.CosyKey != account.CosyKey {
		t.Fatalf("CosyKey = %q, want %q", credential.CosyKey, account.CosyKey)
	}
	if credential.EncryptUserInfo != account.EncryptUserInfo {
		t.Fatalf("EncryptUserInfo = %q, want %q", credential.EncryptUserInfo, account.EncryptUserInfo)
	}
	if credential.UserID != account.UserID {
		t.Fatalf("UserID = %q, want %q", credential.UserID, account.UserID)
	}
	if credential.MachineID != account.MachineID {
		t.Fatalf("MachineID = %q, want %q", credential.MachineID, account.MachineID)
	}
	if credential.Source != account.Source {
		t.Fatalf("Source = %q, want %q", credential.Source, account.Source)
	}
	if !credential.LoadedAt.Equal(account.LoadedAt) {
		t.Fatalf("LoadedAt = %v, want %v", credential.LoadedAt, account.LoadedAt)
	}
	if credential.TokenExpireTime != account.TokenExpireTime {
		t.Fatalf("TokenExpireTime = %d, want %d", credential.TokenExpireTime, account.TokenExpireTime)
	}
}
