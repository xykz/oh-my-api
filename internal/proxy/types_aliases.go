package proxy

import "github.com/rizxfrog/oh-my-api/internal/proxy/types"

// ── API constants ────────────────────────────────────────────────

const (
	ChatPath        = types.ChatPath
	ChatQuery       = types.ChatQuery
	ModelListPath   = types.ModelListPath
	ImageUploadPath = types.ImageUploadPath
)

// ── Error variables ──────────────────────────────────────────────

var (
	ErrUnknownModel           = types.ErrUnknownModel
	ErrCredentialsUnavailable = types.ErrCredentialsUnavailable
)

// ── Core request/response types ──────────────────────────────────

type (
	ToolCall              = types.ToolCall
	FunctionCall          = types.FunctionCall
	Tool                  = types.Tool
	ToolFunction          = types.ToolFunction
	OpenAIContentPart     = types.OpenAIContentPart
	OpenAIContentImageURL = types.OpenAIContentImageURL
	Message               = types.Message
)

// ── Canonical IR types ───────────────────────────────────────────

type (
	CanonicalProtocol         = types.CanonicalProtocol
	CanonicalBlockType        = types.CanonicalBlockType
	CanonicalToolCall         = types.CanonicalToolCall
	CanonicalToolDefinition   = types.CanonicalToolDefinition
	CanonicalToolResult       = types.CanonicalToolResult
	CanonicalContentBlock     = types.CanonicalContentBlock
	CanonicalTurn             = types.CanonicalTurn
	CanonicalRequest          = types.CanonicalRequest
	CanonicalSessionSnapshot  = types.CanonicalSessionSnapshot
	CanonicalExecutionSidecar = types.CanonicalExecutionSidecar
	CanonicalExecutionRecord  = types.CanonicalExecutionRecord
)

const (
	CanonicalProtocolOpenAI    = types.CanonicalProtocolOpenAI
	CanonicalProtocolAnthropic = types.CanonicalProtocolAnthropic
	CanonicalProtocolResponse  = types.CanonicalProtocolResponse
	CanonicalBlockText         = types.CanonicalBlockText
	CanonicalBlockReasoning    = types.CanonicalBlockReasoning
	CanonicalBlockToolCall     = types.CanonicalBlockToolCall
	CanonicalBlockToolResult   = types.CanonicalBlockToolResult
	CanonicalBlockImage        = types.CanonicalBlockImage
	CanonicalBlockDocument     = types.CanonicalBlockDocument
)

// ── OpenAI-compatible types ─────────────────────────────────────

type (
	ExtraBody            = types.ExtraBody
	OpenAIChatRequest    = types.OpenAIChatRequest
	OpenAIModel          = types.OpenAIModel
	OpenAIModelsResponse = types.OpenAIModelsResponse
)

// ── Credential & account types ──────────────────────────────────

type (
	CredentialSnapshot      = types.CredentialSnapshot
	AccountRegion           = types.AccountRegion
	AccountSnapshot         = types.AccountSnapshot
	AccountSummary          = types.AccountSummary
	StoredCredentialFile    = types.StoredCredentialFile
	StoredCredentialAccount = types.StoredCredentialAccount
	StoredAuthFields        = types.StoredAuthFields
	StoredOAuthFields       = types.StoredOAuthFields
	CredentialStatus        = types.CredentialStatus
	StoredMetaInfo          = types.StoredMetaInfo
)

const (
	AccountRegionChina         = types.AccountRegionChina
	AccountRegionInternational = types.AccountRegionInternational
)

// ── Session types ───────────────────────────────────────────────

type (
	SessionState = types.SessionState
)

// ── Model types ─────────────────────────────────────────────────

type (
	ModelStatus = types.ModelStatus
	RemoteModel = types.RemoteModel
)

// ── Transport types ─────────────────────────────────────────────

type (
	RemoteChatRequest = types.RemoteChatRequest
	SSEUsage          = types.SSEUsage
	SSEEvent          = types.SSEEvent
	UpstreamHTTPError = types.UpstreamHTTPError
)

// ── Image types ─────────────────────────────────────────────────

type (
	ImageUploader       = types.ImageUploader
	ImageUploadResponse = types.ImageUploadResponse
	ImageUploadRequest  = types.ImageUploadRequest
)

// ── OpenAI Response API types ────────────────────────────────────

type (
	OpenAIResponseRequest    = types.OpenAIResponseRequest
	OpenAIResponse           = types.OpenAIResponse
	ResponseOutputItem       = types.ResponseOutputItem
	ResponseOutputContent    = types.ResponseOutputContent
	OpenAIResponseUsage      = types.OpenAIResponseUsage
	ResponseStatusDetails    = types.ResponseStatusDetails
	ResponseStreamEvent      = types.ResponseStreamEvent
	ResponseInputItem        = types.ResponseInputItem
	ResponseInputContentPart = types.ResponseInputContentPart
	OpenAIResponseTool       = types.OpenAIResponseTool
	ImageURLPart             = types.ImageURLPart
)

// ── Anthropic types ─────────────────────────────────────────────

type (
	AnthropicTool             = types.AnthropicTool
	AnthropicMessagesRequest  = types.AnthropicMessagesRequest
	ThinkingConfig            = types.ThinkingConfig
	AnthropicMessage          = types.AnthropicMessage
	ContentBlock              = types.ContentBlock
	ImageSource               = types.ImageSource
	SystemBlock               = types.SystemBlock
	AnthropicMessagesResponse = types.AnthropicMessagesResponse
	Usage                     = types.Usage
	AnthropicStreamEvent      = types.AnthropicStreamEvent
	StreamMessage             = types.StreamMessage
	StreamDelta               = types.StreamDelta
)

// ── Re-exported functions ───────────────────────────────────────

func DefaultAliases() map[string]string { return types.DefaultAliases() }
func NewUUID() string                   { return types.NewUUID() }
func NewHexID() string                  { return types.NewHexID() }
func CloneMessages(messages []Message) []Message {
	return types.CloneMessages(messages)
}

// cloneMessages is the unexported version used internally by sessions.go and message_ir.go.
func cloneMessages(messages []Message) []Message {
	return types.CloneMessages(messages)
}
