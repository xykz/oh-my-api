# Protocol Translator Module Design

> **Status:** Draft | **Date:** 2026-05-25

## Goal

Add OpenAI Responses API (`/v1/responses`) as a first-class protocol, and refactor the adapter layer from region-scoped (`RegionAdapter`) to backend-scoped (`BackendAdapter`), making it easy to add new input protocols and new output backends in the future.

## Architecture Overview

```
Inbound Protocols                    Intermediate Representation          Backend Adapters
─────────────────────────────────   ────────────────────────────────   ──────────────────

OpenAI Chat Completions ────→ CanonicalizeOpenAIRequest() ──────┐
                                                                  │
OpenAI Responses API ──────→ CanonicalizeResponsesRequest() ─────┤──→ CanonicalRequest ──→ BackendAdapter.TranslateRequest()
                                                                  │                            │
Anthropic Messages ────────→ CanonicalizeAnthropicRequest() ─────┘                      LingmaAdapter (current)
                                                                                         (future) OpenAIAdapter
                                                                                         (future) ClaudeAdapter
```

Three input protocols converge into one `CanonicalRequest` IR, then dispatch to any backend via `BackendAdapter`.

---

## Section 1: IR Extension

`CanonicalRequest` gains fields to represent Responses API semantics without affecting existing protocols.

### New types in `proxy/types/types.go`

```go
const (
    CanonicalProtocolOpenAI           CanonicalProtocol = "openai"
    CanonicalProtocolOpenAIResponses  CanonicalProtocol = "openai-responses"  // NEW
    CanonicalProtocolAnthropic        CanonicalProtocol = "anthropic"
)

type TextOutputFormat string

const (
    TextOutputFormatText       TextOutputFormat = "text"
    TextOutputFormatJSONObject TextOutputFormat = "json_object"
    TextOutputFormatJSONSchema TextOutputFormat = "json_schema"
)

type TextOutputConfig struct {
    Format TextOutputFormat `json:"format,omitempty"`
    Schema json.RawMessage  `json:"schema,omitempty"` // JSON Schema
}
```

### New fields on `CanonicalRequest`

```go
type CanonicalRequest struct {
    // ... existing fields unchanged ...

    // Responsives API specific (empty/zero for non-Responses protocols)
    Instructions       *string          `json:"instructions,omitempty"`
    Input              string           `json:"input,omitempty"`               // single-turn text; mutually exclusive with Turns
    PreviousResponseID *string          `json:"previous_response_id,omitempty"` // links to prior response
    ReasoningEffort    *string          `json:"reasoning_effort,omitempty"`    // "low" | "medium" | "high"
    TextOutput         *TextOutputConfig `json:"text_output,omitempty"`        // structured output config
}
```

- `Input` and `Turns` are mutually exclusive: when `Input != ""`, no `Turns`; when `Turns` is non-empty, `Input` is empty.
- `PreviousResponseID` triggers session merging (reuses existing `SessionStore`).

---

## Section 2: BackendAdapter Interface

`RegionAdapter` is deprecated in favor of `BackendAdapter`, which is protocol-agnostic and backend-agnostic.

### Interface definition in `proxy/adapters.go`

```go
type BackendID string

const (
    BackendLingma BackendID = "lingma"
)

type BackendAdapter interface {
    Backend() BackendID

    ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error)

    // TranslateRequest converts a CanonicalRequest into a backend-specific HTTP request.
    TranslateRequest(ctx context.Context, canonical CanonicalRequest, account AccountSnapshot) (RemoteRequest, error)

    // StreamCall sends the request and returns the raw response body.
    StreamCall(ctx context.Context, req RemoteRequest, account AccountSnapshot) (io.ReadCloser, error)

    // TranslateStreamEvent parses a raw SSE/JSON line from the backend into a common SSEEvent.
    TranslateStreamEvent(rawLine string) (SSEEvent, error)

    UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error)

    TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult
}
```

### Key changes from `RegionAdapter`

| Old (`RegionAdapter`) | New (`BackendAdapter`) | Reason |
|---|---|---|
| `Region() AccountRegion` | `Backend() BackendID` | Backend-scoped, not region-scoped |
| `BuildChatRequest(canonical, modelKey, account)` | `TranslateRequest(canonical, account)` | `modelKey` already in `CanonicalRequest.Model` |
| `StreamChat(ctx, req, account)` | `StreamCall(ctx, req, account)` | Name is backend-agnostic |
| (none) | `TranslateStreamEvent(rawLine)` | Each backend parses its own SSE format |

### AdapterRegistry changes

```go
// Old
type AdapterRegistry struct {
    adapters map[AccountRegion]RegionAdapter
}

// New
type AdapterRegistry struct {
    adapters map[BackendID]BackendAdapter
}
```

### LingmaAdapter implementation

`LingmaAdapter` (from the existing `2026-05-23-unify-adapter-protocol.md` plan) implements `BackendAdapter`. Its `TranslateStreamEvent` encapsulates the Lingma-specific two-layer JSON SSE parsing logic that currently lives in handlers.

---

## Section 3: Responses API Translation Flow

### Inbound: `CanonicalizeResponsesRequest()`

```go
func CanonicalizeResponsesRequest(req OpenaiResponsesRequest) (CanonicalRequest, error)
```

Logic:
1. Map `model`, `stream`, `temperature` → `CanonicalRequest` fields
2. If `input` is a string → set `Input`; if array of messages → expand into `Turns`
3. Copy `instructions` → `Instructions`
4. Map `tools` array → `CanonicalToolDefinition` array; set `HasTools`
5. Map `reasoning.effort` → `ReasoningEffort`
6. Map `text.format` → `TextOutput`
7. Copy `previous_response_id` → `PreviousResponseID`
8. Set `Protocol = CanonicalProtocolOpenAIResponses`
9. If `temperature` is nil, default to 1.0 (Responses API default)

### Outbound: `ProjectCanonicalToResponsesEvent()`

```go
func ProjectCanonicalToResponsesEvent(event SSEEvent, responseID string, itemID *string) []ResponsesSSEEvent
```

Event mapping:

| `SSEEvent` field | Responses API event(s) emitted |
|---|---|
| First event of stream | `response.created` + `response.in_progress` |
| `Delta != ""` | `response.output_text.delta` |
| `Reasoning != ""` | `response.reasoning_text.delta` |
| `ToolCalls` added | `response.output_item.added` + `response.tool_call.arguments.delta` |
| `Done == true` | `response.output_text.done` + `response.completed` (with `usage`) |

Each output event is an SSE `data:` line with structure:
```json
{
  "type": "response.output_text.delta",
  "delta": "Hello",
  "item_id": "item_abc",
  "output_index": 0,
  "content_index": 0
}
```

---

## Section 4: Handler, Router, and Response Writers

### New handler: `api/handler/responses.go`

```go
func (s *Server) HandleResponses(w http.ResponseWriter, r *http.Request) {
    // 1. Decode & validate OpenaiResponsesRequest (max 2MB)
    // 2. CanonicalizeResponsesRequest()
    // 3. Evaluate vision gate (if images present)
    // 4. Evaluate policies via service.EvaluateCanonicalRequest()
    // 5. If PreviousResponseID set → SessionStore.BuildCanonicalRequest() merge
    // 6. selectBackendAdapter() → BackendAdapter
    // 7. adapter.TranslateRequest(canonical, account) → RemoteRequest
    // 8. adapter.StreamCall(ctx, remoteReq, account) → io.ReadCloser
    // 9. SSE loop:
    //      rawLine ← scanner.Scan()
    //      event ← adapter.TranslateStreamEvent(rawLine)
    //      responsesEvents ← ProjectCanonicalToResponsesEvent(event, responseID, &itemID)
    //      for each: WriteResponsesSSEEvent(w, evt) or collect for non-stream
    // 10. SessionStore.SaveCanonicalResponse()
    // 11. PersistCanonicalExecutionRecord()
}
```

### Router addition: `api/router/router.go`

```go
mux.HandleFunc("/v1/responses", s.HandleResponses)
```

### New protocol constant

```go
CanonicalProtocolOpenAIResponses CanonicalProtocol = "openai-responses"
```

### New types: `proxy/types/responses.go`

```go
type OpenaiResponsesRequest struct {
    Model              string              `json:"model"`
    Input              ResponsesInput      `json:"input"`
    Instructions       *string             `json:"instructions,omitempty"`
    Tools              []ResponsesTool     `json:"tools,omitempty"`
    ToolChoice         any                 `json:"tool_choice,omitempty"`
    Stream             bool                `json:"stream,omitempty"`
    Temperature        *float64            `json:"temperature,omitempty"`
    MaxOutputTokens    *int                `json:"max_output_tokens,omitempty"`
    Reasoning          *ReasoningConfig    `json:"reasoning,omitempty"`
    Text               *ResponsesTextConfig `json:"text,omitempty"`
    PreviousResponseID *string             `json:"previous_response_id,omitempty"`
    Metadata           map[string]any      `json:"metadata,omitempty"`
}
```

`ResponsesInput` is a custom JSON unmarshaler that handles both `string` and `[{role, content}...]`.

### New response writers: `api/response/responses.go`

- `WriteResponsesSSEEvent(w, evt)` — SSE streaming output
- `BuildResponsesNonStreamingResponse(collectedEvents)` — non-stream JSON response

---

## Section 5: File Impact Summary

| Layer | File | Action | Description |
|---|---|---|---|
| IR | `proxy/types/types.go` | Modify | New constants, `TextOutputConfig`, `CanonicalRequest` fields |
| IR | `proxy/types/responses.go` | **New** | `OpenaiResponsesRequest`, `ResponsesInput`, `ResponsesTool` etc. |
| Translation | `proxy/message_ir.go` | Modify | Add `CanonicalizeResponsesRequest()` |
| Translation | `proxy/message_ir_response.go` | **New** | Add `ProjectCanonicalToResponsesEvent()` |
| Adapter | `proxy/adapters.go` | Modify | `BackendAdapter` interface, `BackendID`, deprecate `RegionAdapter` |
| Adapter | `proxy/lingma_adapter.go` | Follow plan | Implement `BackendAdapter` per existing plan |
| Handler | `api/handler/responses.go` | **New** | `HandleResponses` |
| Router | `api/router/router.go` | Modify | Register `/v1/responses` |
| Response | `api/response/responses.go` | **New** | `WriteResponsesSSEEvent()`, non-stream builder |
| Entry | `main.go` | Modify | Register `BackendAdapter` instead of `RegionAdapter` |

**Total: 10 files (4 new + 6 modified).** No changes to policy engine, session management, config, or database layers.

---

## Section 6: Backward Compatibility

- `RegionAdapter` interface and types remain (marked `// Deprecated`) until all callers migrate
- Existing `/v1/chat/completions` and `/v1/messages` endpoints unchanged
- Existing tests continue to pass if `RegionAdapter` adapter methods are preserved as wrappers

---

## Section 7: Testing Strategy

| Scope | What to test |
|---|---|
| Unit | `CanonicalizeResponsesRequest()` — string input, array input, all optional fields, error cases |
| Unit | `ProjectCanonicalToResponsesEvent()` — delta, reasoning, tool calls, completion events |
| Unit | `BackendAdapter` interface satisfaction for `LingmaAdapter` |
| Integration | `/v1/responses` end-to-end: request → Lingma → SSE → Responses format |
| Integration | `previous_response_id` session continuity |
| Regression | Existing `/v1/chat/completions` and `/v1/messages` still work |

---

## Dependencies

This design depends on the `2026-05-23-unify-adapter-protocol.md` plan (unifying `ChinaAdapter`/`InternationalAdapter` into `LingmaAdapter`) being implemented first, as `BackendAdapter` builds on that refactored `LingmaAdapter`.
