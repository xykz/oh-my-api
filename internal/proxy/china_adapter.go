package proxy

import "time"

// ErrChinaAdapterNilTransport is an alias for ErrLingmaAdapterNilTransport.
//
// Deprecated: Use ErrLingmaAdapterNilTransport instead.
var ErrChinaAdapterNilTransport = ErrLingmaAdapterNilTransport

// ErrChinaAdapterNilBuilder is an alias for ErrLingmaAdapterNilBuilder.
//
// Deprecated: Use ErrLingmaAdapterNilBuilder instead.
var ErrChinaAdapterNilBuilder = ErrLingmaAdapterNilBuilder

// ChinaAdapter is a LingmaAdapter pre-configured for the China region.
type ChinaAdapter = LingmaAdapter

// NewChinaAdapter creates a LingmaAdapter for the China region.
func NewChinaAdapter(transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*ChinaAdapter, error) {
	return NewLingmaAdapter(AccountRegionChina, transport, builder, now)
}
