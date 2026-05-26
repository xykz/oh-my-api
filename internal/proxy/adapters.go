package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
)

var ErrAdapterProtocolNotConfigured = errors.New("adapter protocol not configured")
var ErrAdapterNil = errors.New("adapter is nil")
var ErrLingmaAdapterNilTransport = errors.New("lingma adapter native transport is nil")
var ErrLingmaAdapterNilBuilder = errors.New("lingma adapter body builder is nil")

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
	return &AdapterRegistry{
		adapters: make(map[AccountRegion]RegionAdapter),
	}
}

func (r *AdapterRegistry) Register(adapter RegionAdapter) error {
	if r == nil {
		return errors.New("adapter registry is nil")
	}
	if isNilRegionAdapter(adapter) {
		return ErrAdapterNil
	}
	if r.adapters == nil {
		r.adapters = make(map[AccountRegion]RegionAdapter)
	}
	r.adapters[adapter.Region()] = adapter
	return nil
}

func (r *AdapterRegistry) ForRegion(region AccountRegion) (RegionAdapter, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: no adapter for region %q", ErrAdapterProtocolNotConfigured, region)
	}
	adapter, ok := r.adapters[region]
	if !ok {
		return nil, fmt.Errorf("%w: no adapter for region %q", ErrAdapterProtocolNotConfigured, region)
	}
	return adapter, nil
}

func isNilRegionAdapter(adapter RegionAdapter) bool {
	if adapter == nil {
		return true
	}

	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
