package model

import (
	"context"
	"embed"
	"io"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/config"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
	"github.com/rizxfrog/oh-my-api/internal/redis"
)

// ── Provider interfaces ──────────────────────────────────────────

type CredentialProvider interface {
	Current(context.Context) (proxy.CredentialSnapshot, error)
	Refresh(context.Context) (proxy.CredentialSnapshot, error)
	Status() proxy.CredentialStatus
	StoredMeta() proxy.StoredMetaInfo
	HasOAuth() (bool, bool)
}

type ModelService interface {
	ResolveChatModel(context.Context, string) (string, error)
	ListModels(context.Context) ([]proxy.OpenAIModel, error)
	Refresh(context.Context) error
	Status() proxy.ModelStatus
}

type ModelRegionProvider interface {
	AvailableRegions(context.Context, string) ([]proxy.AccountRegion, bool, error)
}

type ModelAccountProvider interface {
	AvailableAccounts(context.Context, string) ([]string, bool, error)
}

type SessionStore interface {
	BuildCanonicalRequest(context.Context, string, proxy.CanonicalRequest) (proxy.CanonicalRequest, error)
	SaveCanonicalResponse(context.Context, string, proxy.CanonicalRequest, proxy.Message) error
	Delete(context.Context, string) error
	List(context.Context) ([]proxy.SessionState, error)
	SweepExpired(context.Context) error
}

type ChatTransport interface {
	StreamChat(context.Context, proxy.RemoteChatRequest, proxy.CredentialSnapshot) (io.ReadCloser, error)
}

type AccountProvider interface {
	Accounts(context.Context) ([]proxy.AccountSnapshot, error)
	Summaries(context.Context) ([]proxy.AccountSummary, error)
}

type AccountBalancer interface {
	Next([]proxy.AccountSnapshot) (proxy.AccountSnapshot, error)
}

type ImageUploader interface {
	UploadImage(ctx context.Context, credential proxy.CredentialSnapshot, imageURI string) (cdnURL string, err error)
}

type RequestBuilder interface {
	BuildCanonical(proxy.CanonicalRequest, string) (proxy.RemoteChatRequest, error)
}

// BootstrapAccountStore is the subset of account store needed by bootstrap.
type BootstrapAccountStore interface {
	UpsertAccount(context.Context, proxy.StoredCredentialAccount) error
}

// SettingsStore is the minimal subset of *db.Store required by vision gate.
type SettingsStore interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

// ── Dependencies ─────────────────────────────────────────────────

type Dependencies struct {
	Credentials        CredentialProvider
	Accounts           AccountProvider
	Models             ModelService
	Sessions           SessionStore
	AccountPool        *proxy.AccountPool
	AccountConfig      config.AccountConfig
	Balancer           AccountBalancer
	Adapters           *proxy.AdapterRegistry
	Transport          ChatTransport
	Uploader           ImageUploader
	Builder            RequestBuilder
	AdminToken         string
	StoreExecutionLogs bool
	Now                func() time.Time
	FrontendFS         embed.FS
	TokenStats         *redis.TokenStats
}

// ── OpenAI response types ────────────────────────────────────────

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *OpenAIUsage           `json:"usage,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionChoice struct {
	Index        int              `json:"index"`
	Message      *proxy.Message   `json:"message,omitempty"`
	Delta        *DeltaPayload    `json:"delta,omitempty"`
	FinishReason *string          `json:"finish_reason"`
	ToolCalls    []proxy.ToolCall `json:"tool_calls,omitempty"`
}

type DeltaPayload struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []proxy.ToolCall `json:"tool_calls,omitempty"`
}

type AdminStatusResponse struct {
	Credential   proxy.CredentialStatus `json:"credential"`
	Models       proxy.ModelStatus      `json:"models"`
	SessionCount int                    `json:"session_count"`
}

// ── Error sentinels ──────────────────────────────────────────────

var ErrVisionNotImplemented = errVisionNotImplemented{}

type errVisionNotImplemented struct{}

func (errVisionNotImplemented) Error() string { return "vision_not_implemented" }
