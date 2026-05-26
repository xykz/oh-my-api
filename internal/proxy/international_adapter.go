package proxy

import "time"

// InternationalAdapter is a LingmaAdapter pre-configured for the International region.
type InternationalAdapter = LingmaAdapter

// NewInternationalAdapter creates a LingmaAdapter for the International region.
// The caller must provide a NativeTransport configured with the correct international baseURL.
func NewInternationalAdapter(transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*InternationalAdapter, error) {
	return NewLingmaAdapter(AccountRegionInternational, transport, builder, now)
}
