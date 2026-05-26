package proxy

import (
	"context"
	"fmt"
	"io"
	"time"
)

// LingmaAdapter implements the RegionAdapter interface for the Lingma protocol.
// Both China and International regions use the same protocol, differing only in
// baseURL and region identifier.
type LingmaAdapter struct {
	region    AccountRegion
	transport *NativeTransport
	builder   *BodyBuilder
	now       func() time.Time
}

// NewLingmaAdapter creates a LingmaAdapter for the given region with the
// provided transport and body builder.
func NewLingmaAdapter(region AccountRegion, transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*LingmaAdapter, error) {
	if transport == nil {
		return nil, ErrLingmaAdapterNilTransport
	}
	if builder == nil {
		return nil, ErrLingmaAdapterNilBuilder
	}
	if now == nil {
		now = time.Now
	}
	return &LingmaAdapter{
		region:    region,
		transport: transport,
		builder:   builder,
		now:       now,
	}, nil
}

func (a *LingmaAdapter) Region() AccountRegion {
	return a.region
}

func (a *LingmaAdapter) ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error) {
	return a.transport.ListModels(ctx, account.ToCredentialSnapshot())
}

func (a *LingmaAdapter) BuildChatRequest(_ context.Context, canonical CanonicalRequest, modelKey string, _ AccountSnapshot) (RemoteChatRequest, error) {
	return a.builder.BuildCanonical(canonical, modelKey)
}

func (a *LingmaAdapter) StreamChat(ctx context.Context, request RemoteChatRequest, account AccountSnapshot) (io.ReadCloser, error) {
	return a.transport.StreamChat(ctx, request, account.ToCredentialSnapshot())
}

func (a *LingmaAdapter) UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error) {
	return a.transport.UploadImage(ctx, account.ToCredentialSnapshot(), imageURI)
}

func (a *LingmaAdapter) TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult {
	models, err := a.ListModels(ctx, account)
	if err != nil {
		return AccountTestResult{
			AccountID:    account.ID,
			AccountLabel: account.Label,
			Region:       account.Region,
			Success:      false,
			Error:        err.Error(),
			Timestamp:    a.now().Format(time.RFC3339),
		}
	}

	return AccountTestResult{
		AccountID:       account.ID,
		AccountLabel:    account.Label,
		Region:          account.Region,
		Success:         true,
		StatusCode:      200,
		ResponsePreview: fmt.Sprintf("ListModels returned %d models", len(models)),
		Timestamp:       a.now().Format(time.RFC3339),
	}
}
