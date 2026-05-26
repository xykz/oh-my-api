# OpenAI Responses API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `/v1/responses` endpoint supporting OpenAI Responses API format (core subset), translated through the existing Canonical IR pipeline to the Lingma upstream.

**Architecture:** Follow the existing pattern: define types, add `CanonicalProtocolResponses` to the IR, write `CanonicalizeResponsesRequest()` that converts `input`/`instructions` → canonical turns, reuse `ProjectCanonicalToOpenAIRequest()` for upstream projection, then format responses back in Responses API shape (non-streaming JSON and streaming SSE). No `previous_response_id`, no built-in tools.

**Tech Stack:** Go, standard library `net/http`, existing `internal/proxy`, `internal/api/handler`, `internal/api/response`

---

## Scope (Core Subset)

**Included:**
- `input` — string or content-block array
- `instructions` — system-level instructions string
- `model`, `stream`, `temperature`, `max_output_tokens`
- `tools` / `tool_choice` — function calling (same Canonical IR tool support)
- `reasoning.effort` — maps to `canonical_request.has_reasoning`
- Non-streaming JSON response
- Streaming SSE response

**Excluded:**
- `previous_response_id` / `store` (stateful multi-turn)
- Built-in tools (`web_search`, `file_search`, `code_interpreter`, MCP)
- `top_p`, `top_logprobs`, `text.format`, `truncation`, `background`
- `include[]` (response item inclusion control)

---

## Responses API Format Reference

### Request

```json
{
  "model": "gpt-5.2",
  "input": "Hello!" | [{"type": "input_text", "text": "Hello!"}, {"type": "input_image", "image_url": "..."}],
  "instructions": "You are a helpful assistant",
  "stream": false,
  "temperature": 1.0,
  "max_output_tokens": 4096,
  "tools": [{"type": "function", "name": "...", "description": "...", "parameters": {...}}],
  "tool_choice": "auto",
  "reasoning": {"effort": "none"}
}
```

### Non-streaming Response

```json
{
  "id": "resp_xxx",
  "object": "response",
  "model": "gpt-5.2",
  "status": "completed",
  "created_at": 1234567890,
  "output": [
    {
      "id": "msg_xxx",
      "type": "message",
      "role": "assistant",
      "status": "completed",
      "content": [{"type": "output_text", "text": "Hi!", "annotations": []}]
    }
  ],
  "usage": {
    "input_tokens": 10,
    "output_tokens": 5,
    "total_tokens": 15,
    "input_tokens_details": {"cached_tokens": 0},
    "output_tokens_details": {"reasoning_tokens": 0}
  },
  "temperature": 1.0,
  "tool_choice": "auto",
  "tools": [],
  "parallel_tool_calls": true,
  "reasoning": {"effort": "none", "summary": null},
  "instructions": null,
  "max_output_tokens": 4096,
  "previous_response_id": null,
  "background": false,
  "truncation": "disabled"
}
```

### Streaming Events

| Event Type | When | Key Fields |
|---|---|---|
| `response.created` | Start | `response` (full object stub) |
| `response.in_progress` | Processing begins | `response` |
| `response.output_item.added` | New output item | `output_index`, `item` |
| `response.content_part.added` | New content part | `item_id`, `output_index`, `content_index`, `part` |
| `response.output_text.delta` | Text chunk | `item_id`, `output_index`, `content_index`, `delta` |
| `response.output_text.done` | Text complete | `item_id`, `output_index`, `content_index`, `text` |
| `response.content_part.done` | Content part done | `item_id`, `output_index`, `content_index`, `part` |
| `response.output_item.done` | Output item done | `output_index`, `item` |
| `response.function_call_arguments.delta` | Tool args chunk | `item_id`, `output_index`, `delta` |
| `response.function_call_arguments.done` | Tool args done | `item_id`, `output_index`, `arguments` |
| `response.completed` | End | `response` (full object) |

---

### Task 1: Add Responses API Types

**Files:**
- Create: `internal/proxy/types/responses.go`

**Step 1: Create the types file**

Write `internal/proxy/types/responses.go` with all Responses API request, response, and streaming types. These mirror the OpenAI API format closely.

```go
package types

import "encoding/json"

// ── Request types ────────────────────────────────────────────────

type ResponsesRequest struct {
	Model           string               `json:"model"`
	Input           ResponsesInput       `json:"input"`
	Instructions    *string              `json:"instructions,omitempty"`
	Stream          bool                 `json:"stream,omitempty"`
	Temperature     *float64             `json:"temperature,omitempty"`
	MaxOutputTokens *int                 `json:"max_output_tokens,omitempty"`
	Tools           []Tool               `json:"tools,omitempty"`
	ToolChoice      any                  `json:"tool_choice,omitempty"`
	Reasoning       *ResponsesReasoning  `json:"reasoning,omitempty"`
}

// ResponsesInput handles both string and content-block-array forms.
// Marshaled as `input` in JSON.
type ResponsesInput struct {
	Text   string               // when `input` is a plain string
	Blocks []ResponsesInputItem // when `input` is an array of content blocks
}

// UnmarshalJSON implements custom decoding for the dual input format.
func (ri *ResponsesInput) UnmarshalJSON(data []byte) error {
	data = trimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &ri.Text)
	}
	if data[0] == '[' {
		return json.Unmarshal(data, &ri.Blocks)
	}
	var txt string
	if err := json.Unmarshal(data, &txt); err != nil {
		return err
	}
	ri.Text = txt
	return nil
}

func (ri ResponsesInput) IsEmpty() bool {
	return ri.Text == "" && len(ri.Blocks) == 0
}

func (ri ResponsesInput) IsText() bool {
	return ri.Text != ""
}

type ResponsesInputItem struct {
	Type     string          `json:"type"`               // "input_text", "input_image", "function_call_output"
	Text     string          `json:"text,omitempty"`
	ImageURL *ResponsesImage `json:"image_url,omitempty"`
	CallID   string          `json:"call_id,omitempty"`  // for function_call_output
	Output   string          `json:"output,omitempty"`   // for function_call_output
}

type ResponsesImage struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type ResponsesReasoning struct {
	Effort          *string `json:"effort,omitempty"`
	GenerateSummary *string `json:"generate_summary,omitempty"`
}

// ── Non-streaming response types ─────────────────────────────────

type ResponsesResponse struct {
	ID                string                  `json:"id"`
	Object            string                  `json:"object"` // "response"
	Model             string                  `json:"model"`
	Status            string                  `json:"status"`
	CreatedAt         int64                   `json:"created_at"`
	Output            []ResponsesOutputItem   `json:"output"`
	Usage             *ResponsesUsage         `json:"usage,omitempty"`
	Temperature       *float64                `json:"temperature,omitempty"`
	ToolChoice        any                     `json:"tool_choice,omitempty"`
	Tools             []Tool                  `json:"tools,omitempty"`
	ParallelToolCalls bool                    `json:"parallel_tool_calls"`
	Reasoning         *ResponsesReasoning     `json:"reasoning,omitempty"`
	Instructions      *string                 `json:"instructions"`
	MaxOutputTokens   *int                    `json:"max_output_tokens"`
	PreviousRespID    *string                 `json:"previous_response_id"`
	Background        bool                    `json:"background"`
	Truncation        string                  `json:"truncation"`
	Error             *ResponsesError         `json:"error,omitempty"`
}

type ResponsesOutputItem struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"` // "message" or "function_call"
	Role    string                 `json:"role,omitempty"`
	Status  string                 `json:"status"`
	Content []ResponsesContentPart `json:"content,omitempty"`
	// function_call specific fields:
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ResponsesContentPart struct {
	Type        string                    `json:"type"` // "output_text"
	Text        string                    `json:"text,omitempty"`
	Annotations []ResponsesAnnotation     `json:"annotations"`
}

type ResponsesAnnotation struct{}

type ResponsesUsage struct {
	InputTokens          int                       `json:"input_tokens"`
	OutputTokens         int                       `json:"output_tokens"`
	TotalTokens          int                       `json:"total_tokens"`
	InputTokensDetails   *ResponsesTokenDetails    `json:"input_tokens_details,omitempty"`
	OutputTokensDetails  *ResponsesTokenDetails    `json:"output_tokens_details,omitempty"`
}

type ResponsesTokenDetails struct {
	CachedTokens    int `json:"cached_tokens"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

type ResponsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ── Streaming event types ────────────────────────────────────────

// ResponsesStreamEvent represents a single SSE event in the Responses API streaming format.
type ResponsesStreamEvent struct {
	Type         string                 `json:"type"`
	Response     *ResponsesResponse     `json:"response,omitempty"`
	Item         *ResponsesOutputItem   `json:"item,omitempty"`
	Part         *ResponsesContentPart  `json:"part,omitempty"`
	Delta        string                 `json:"delta,omitempty"`
	Text         string                 `json:"text,omitempty"`
	Arguments    string                 `json:"arguments,omitempty"`
	ItemID       string                 `json:"item_id,omitempty"`
	OutputIndex  int                    `json:"output_index"`
	ContentIndex int                    `json:"content_index"`
}

func trimSpace(data []byte) []byte {
	// Trim leading and trailing whitespace
	s := string(data)
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return []byte(s)
}
```

**Step 2: Verify compilation**

Run: `go vet ./internal/proxy/types/`
Expected: No errors.

**Step 3: Commit**

```bash
git add internal/proxy/types/responses.go
git commit -m "feat(proxy): add OpenAI Responses API types"
```

---

### Task 2: Add CanonicalProtocolResponses Constant

**Files:**
- Modify: `internal/proxy/types/types.go:71-74`

**Step 1: Add the new protocol constant**

Add `CanonicalProtocolResponses` to the `CanonicalProtocol` constants:

```go
const (
	CanonicalProtocolOpenAI    CanonicalProtocol = "openai"
	CanonicalProtocolAnthropic CanonicalProtocol = "anthropic"
	CanonicalProtocolResponses CanonicalProtocol = "responses"
)
```

**Step 2: Commit**

```bash
git add internal/proxy/types/types.go
git commit -m "feat(proxy): add CanonicalProtocolResponses constant"
```

---

### Task 3: Add CanonicalizeResponsesRequest

**Files:**
- Modify: `internal/proxy/message_ir.go` — append new function at end of file

**Step 1: Implement `CanonicalizeResponsesRequest()`**

Add to the end of `internal/proxy/message_ir.go`:

```go
// CanonicalizeResponsesRequest converts a Responses API request to a CanonicalRequest.
// input text → user turn, instructions → system turn, function_call_output → tool turn,
// input_image → image block.
func CanonicalizeResponsesRequest(req ResponsesRequest, sessionID string) (CanonicalRequest, error) {
	turns := make([]CanonicalTurn, 0, 3)

	// instructions → system turn
	if req.Instructions != nil && *req.Instructions != "" {
		turns = append(turns, CanonicalTurn{
			Role: "system",
			Blocks: []CanonicalContentBlock{{
				Type: CanonicalBlockText,
				Text: *req.Instructions,
			}},
		})
	}

	// input → user or tool turns
	if req.Input.IsText() {
		turns = append(turns, CanonicalTurn{
			Role: "user",
			Blocks: []CanonicalContentBlock{{
				Type: CanonicalBlockText,
				Text: req.Input.Text,
			}},
		})
	} else {
		for _, item := range req.Input.Blocks {
			switch item.Type {
			case "input_text":
				if item.Text != "" {
					turns = append(turns, CanonicalTurn{
						Role: "user",
						Blocks: []CanonicalContentBlock{{
							Type: CanonicalBlockText,
							Text: item.Text,
						}},
					})
				}
			case "input_image":
				if item.ImageURL != nil {
					src, err := parseOpenAIImageURL(item.ImageURL.URL)
					if err != nil {
						return CanonicalRequest{}, fmt.Errorf("input_image: %w", err)
					}
					rawSrc, err := json.Marshal(src)
					if err != nil {
						return CanonicalRequest{}, fmt.Errorf("input_image marshal: %w", err)
					}
					turns = append(turns, CanonicalTurn{
						Role: "user",
						Blocks: []CanonicalContentBlock{{
							Type:     CanonicalBlockImage,
							Data:     rawSrc,
							Metadata: imageBlockMetadata(src, len(turns)),
						}},
					})
				}
			case "function_call_output":
				turns = append(turns, CanonicalTurn{
					Role: "tool",
					Blocks: []CanonicalContentBlock{{
						Type: CanonicalBlockToolResult,
						ToolResult: &CanonicalToolResult{
							ToolCallID: item.CallID,
							Content:    item.Output,
						},
					}},
				})
			}
		}
	}

	hasReasoning := req.Reasoning != nil && req.Reasoning.Effort != nil && *req.Reasoning.Effort != "none"

	return CanonicalRequest{
		SchemaVersion: 1,
		Protocol:      CanonicalProtocolResponses,
		Model:         req.Model,
		Stream:        req.Stream,
		Temperature:   req.Temperature,
		Tools:         canonicalToolDefinitions(req.Tools),
		ToolChoice:    req.ToolChoice,
		HasTools:      len(req.Tools) > 0,
		HasReasoning:  hasReasoning,
		SessionID:     sessionID,
		Turns:         turns,
	}, nil
}
```

**Step 2: Verify compilation**

Run: `go vet ./internal/proxy/`
Expected: No errors.

**Step 3: Commit**

```bash
git add internal/proxy/message_ir.go
git commit -m "feat(proxy): add CanonicalizeResponsesRequest for Responses API input translation"
```

---

### Task 4: Add Responses API Error Writer

**Files:**
- Modify: `internal/api/response/writers.go` — append new functions at end of file

**Step 1: Add error writers**

Add to the end of `internal/api/response/writers.go`:

```go
// ── Responses API response helpers ───────────────────────────────

func WriteResponsesError(writer http.ResponseWriter, statusCode int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func WriteResponsesMappedError(writer http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	switch {
	case errors.Is(err, proxy.ErrUnknownModel):
		statusCode = http.StatusBadRequest
	case errors.Is(err, proxy.ErrAdapterProtocolNotConfigured):
		statusCode = http.StatusNotImplemented
	case errors.Is(err, proxy.ErrCredentialsUnavailable):
		statusCode = http.StatusInternalServerError
	default:
		var upstream *proxy.UpstreamHTTPError
		if errors.As(err, &upstream) {
			if upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden {
				statusCode = http.StatusUnauthorized
			} else {
				statusCode = http.StatusBadGateway
			}
		}
	}
	WriteResponsesError(writer, statusCode, err.Error())
}
```

**Step 2: Verify compilation**

Run: `go vet ./internal/api/response/`
Expected: No errors.

**Step 3: Commit**

```bash
git add internal/api/response/writers.go
git commit -m "feat(api): add Responses API error response writers"
```

---

### Task 5: Add Responses API Handler

**Files:**
- Create: `internal/api/handler/responses.go`

This is the largest task. The handler follows the exact same pattern as `HandleChatCompletions` and `HandleAnthropicMessages`.

**Step 1: Create the handler file**

Write `internal/api/handler/responses.go`:

```go
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/api/service"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

// ── Request parsing ──────────────────────────────────────────────

func decodeResponsesRequest(writer http.ResponseWriter, request *http.Request) (proxy.ResponsesRequest, error) {
	body := http.MaxBytesReader(writer, request.Body, 1<<20)
	defer body.Close()

	var req proxy.ResponsesRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		return proxy.ResponsesRequest{}, err
	}
	return req, nil
}

// ── Handler ──────────────────────────────────────────────────────

func (s *Server) HandleResponses(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteMethodNotAllowed(writer, http.MethodPost)
		return
	}

	responsesReq, err := decodeResponsesRequest(writer, request)
	if err != nil {
		response.WriteResponsesError(writer, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if responsesReq.Model == "" {
		response.WriteResponsesError(writer, http.StatusBadRequest, "model is required")
		return
	}
	if responsesReq.Input.IsEmpty() {
		response.WriteResponsesError(writer, http.StatusBadRequest, "input must not be empty")
		return
	}

	sessionID := service.RequestSessionID(request, "")
	canonicalRequest, err := proxy.CanonicalizeResponsesRequest(responsesReq, sessionID)
	if err != nil {
		response.WriteResponsesError(writer, http.StatusBadRequest, err.Error())
		return
	}
	service.AttachCanonicalRequestMetadata(&canonicalRequest, request.Header)

	if err := proxy.ValidateVisionLimits(canonicalRequest); err != nil {
		response.WriteResponsesError(writer, http.StatusBadRequest, "vision: "+err.Error())
		return
	}
	var visionStore model.SettingsStore
	if s.DB != nil {
		visionStore = s.DB
	}
	if _, err := service.EvaluateVisionGate(request.Context(), visionStore, canonicalRequest); err != nil {
		if service.IsVisionNotImplemented(err) {
			response.WriteResponsesError(writer, http.StatusNotImplemented, "vision input is not implemented")
			return
		}
		response.WriteResponsesMappedError(writer, err)
		return
	}

	policyResult, err := service.EvaluateCanonicalRequest(request.Context(), s.DB, canonicalRequest)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	sessionCanonicalRequest, err := s.Deps.Sessions.BuildCanonicalRequest(request.Context(), sessionID, policyResult.PostPolicyRequest)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	projectedRequest, projectedMessages, err := proxy.ProjectCanonicalToOpenAIRequest(sessionCanonicalRequest)
	if err != nil {
		response.WriteResponsesError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChatRequest(&projectedRequest); err != nil {
		response.WriteResponsesError(writer, http.StatusBadRequest, err.Error())
		return
	}

	messages := projectedMessages
	resolvedModelKey, err := s.Deps.Models.ResolveChatModel(request.Context(), projectedRequest.Model)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}

	if s.accountRoutingEnabled() {
		s.handleAccountRoutedResponses(
			writer, request, responsesReq, projectedRequest,
			sessionCanonicalRequest, canonicalRequest, policyResult.PostPolicyRequest,
			sessionID, messages, resolvedModelKey,
		)
		return
	}

	credential, err := s.Deps.Credentials.Current(request.Context())
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}

	if s.Deps.Uploader != nil {
		imageURLs, err := s.uploadImagesFromCanonicalRequest(request.Context(), credential, sessionCanonicalRequest)
		if err != nil {
			response.WriteResponsesMappedError(writer, err)
			return
		}
		if len(imageURLs) > 0 {
			sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
			sessionCanonicalRequest.Metadata["is_vl"] = true
		}
	}

	remoteRequest, err := s.Deps.Builder.BuildCanonical(sessionCanonicalRequest, resolvedModelKey)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	stream, err := s.Deps.Transport.StreamChat(request.Context(), remoteRequest, credential)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	defer stream.Close()

	responseID := "resp_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()

	if !responsesReq.Stream {
		s.writeResponsesNonStream(
			request.Context(), writer, stream, responseID,
			responsesReq, projectedRequest, remoteRequest,
			sessionID, messages,
			canonicalRequest, policyResult.PostPolicyRequest,
			sessionCanonicalRequest, traceID,
		)
	} else {
		s.writeResponsesStream(
			writer, request, stream, responseID,
			responsesReq, projectedRequest, remoteRequest,
			sessionID, messages,
			canonicalRequest, policyResult.PostPolicyRequest,
			sessionCanonicalRequest, traceID,
		)
	}
}

func (s *Server) handleAccountRoutedResponses(
	writer http.ResponseWriter,
	request *http.Request,
	responsesReq proxy.ResponsesRequest,
	projectedRequest proxy.OpenAIChatRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionID string,
	messages []proxy.Message,
	modelKey string,
) {
	account, adapter, err := s.selectAccountAndAdapter(request.Context(), modelKey)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	attachAccountRoutingMetadata(&prePolicyRequest, account)
	attachAccountRoutingMetadata(&postPolicyRequest, account)
	attachAccountRoutingMetadata(&sessionCanonicalRequest, account)
	imageURLs, err := s.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	if len(imageURLs) > 0 {
		sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
		sessionCanonicalRequest.Metadata["is_vl"] = true
	}

	remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
	if err != nil {
		response.WriteResponsesMappedError(writer, fmt.Errorf("build request: %w", err))
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		response.WriteResponsesMappedError(writer, fmt.Errorf("upstream: %w", err))
		return
	}
	defer stream.Close()

	responseID := "resp_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()

	if !responsesReq.Stream {
		s.writeResponsesNonStream(
			request.Context(), writer, stream, responseID,
			responsesReq, projectedRequest, remoteRequest,
			sessionID, messages,
			prePolicyRequest, postPolicyRequest,
			sessionCanonicalRequest, traceID,
		)
	} else {
		s.writeResponsesStream(
			writer, request, stream, responseID,
			responsesReq, projectedRequest, remoteRequest,
			sessionID, messages,
			prePolicyRequest, postPolicyRequest,
			sessionCanonicalRequest, traceID,
		)
	}
}
```

Add the non-streaming and streaming response formatters after the handler. These are covered in Tasks 6 and 7.

**Step 2: Verify compilation**

Wait until Tasks 6 and 7 are complete before compiling.

**Step 3: Commit**

Commit together with Tasks 6 and 7.

---

### Task 6: Non-Streaming Response Formatter

**Files:**
- Modify: `internal/api/handler/responses.go` — append after Task 5 handler

**Step 1: Implement `writeResponsesNonStream()`**

Append to `responses.go`:

```go
func (s *Server) writeResponsesNonStream(
	ctx context.Context,
	writer http.ResponseWriter,
	stream io.Reader,
	responseID string,
	responsesReq proxy.ResponsesRequest,
	projectedRequest proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	content, rawSSELines, promptTokens, completionTokens, totalTokens, err := collectSSEContentWithUsage(stream)
	if err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}

	messageID := "msg_" + remoteRequest.RequestID
	msgOutput := proxy.ResponsesOutputItem{
		ID:     messageID,
		Type:   "message",
		Role:   "assistant",
		Status: "completed",
		Content: []proxy.ResponsesContentPart{
			{
				Type:        "output_text",
				Text:        content,
				Annotations: []proxy.ResponsesAnnotation{},
			},
		},
	}

	resp := proxy.ResponsesResponse{
		ID:     responseID,
		Object: "response",
		Model:  responsesReq.Model,
		Status: "completed",
		CreatedAt: s.Deps.Now().Unix(),
		Output: []proxy.ResponsesOutputItem{msgOutput},
		Usage: &proxy.ResponsesUsage{
			InputTokens:  promptTokens,
			OutputTokens: completionTokens,
			TotalTokens:  totalTokens,
			InputTokensDetails: &proxy.ResponsesTokenDetails{
				CachedTokens: 0,
			},
			OutputTokensDetails: &proxy.ResponsesTokenDetails{
				ReasoningTokens: 0,
			},
		},
		Temperature:       responsesReq.Temperature,
		ToolChoice:        responsesReq.ToolChoice,
		Tools:             responsesReq.Tools,
		ParallelToolCalls: true,
		Instructions:      responsesReq.Instructions,
		MaxOutputTokens:   responsesReq.MaxOutputTokens,
		Background:        false,
		Truncation:        "disabled",
	}
	if resp.Tools == nil {
		resp.Tools = []proxy.Tool{}
	}
	if responsesReq.Reasoning != nil {
		resp.Reasoning = responsesReq.Reasoning
	}

	assistant := proxy.Message{
		Role:    "assistant",
		Content: content,
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(context.Background(), sessionID, sessionCanonicalRequest, assistant); err != nil {
		response.WriteResponsesMappedError(writer, err)
		return
	}
	service.PersistCanonicalExecutionRecord(
		ctx, s.DB, s.Deps.Now(), s.StoreExecutionLogs,
		traceID,
		prePolicyRequest.Protocol,
		"/v1/responses",
		prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
		responsesReq, messages, assistant,
		remoteRequest, rawSSELines,
		promptTokens, completionTokens, totalTokens,
	)
	if s.TokenStats != nil {
		_ = s.TokenStats.AddTokens(ctx, totalTokens)
	}

	response.WriteJSON(writer, http.StatusOK, resp)
}
```

**Step 2: Commit**

Will commit together with Tasks 5 and 7.

---

### Task 7: Streaming Response Formatter

**Files:**
- Modify: `internal/api/handler/responses.go` — append after Task 6

**Step 1: Implement `writeResponsesStream()`**

Append to `responses.go`:

```go
func (s *Server) writeResponsesStream(
	writer http.ResponseWriter,
	request *http.Request,
	stream io.Reader,
	responseID string,
	responsesReq proxy.ResponsesRequest,
	projectedRequest proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		response.WriteResponsesError(writer, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	createdAt := s.Deps.Now().Unix()
	messageID := "msg_" + remoteRequest.RequestID

	// Emit initial events
	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type: "response.created",
		Response: &proxy.ResponsesResponse{
			ID:        responseID,
			Object:    "response",
			Model:     responsesReq.Model,
			Status:    "in_progress",
			CreatedAt: createdAt,
		},
	})
	flusher.Flush()

	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type: "response.in_progress",
		Response: &proxy.ResponsesResponse{
			ID:        responseID,
			Object:    "response",
			Model:     responsesReq.Model,
			Status:    "in_progress",
			CreatedAt: createdAt,
		},
	})
	flusher.Flush()

	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item: &proxy.ResponsesOutputItem{
			ID:     messageID,
			Type:   "message",
			Role:   "assistant",
			Status: "in_progress",
		},
	})
	flusher.Flush()

	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:        "response.content_part.added",
		ItemID:      messageID,
		OutputIndex: 0,
		ContentIndex: 0,
		Part: &proxy.ResponsesContentPart{
			Type: "output_text",
			Text: "",
		},
	})
	flusher.Flush()

	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var rawSSELines []string
	var promptTokens, completionTokens, totalTokens int

	err := proxy.ScanSSEWithLines(stream, func(line string) error {
		rawSSELines = append(rawSSELines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			promptTokens = event.Usage.PromptTokens
			completionTokens = event.Usage.CompletionTokens
			totalTokens = event.Usage.TotalTokens
		}
		if event.Done {
			return nil
		}
		if event.Content != "" {
			contentBuilder.WriteString(event.Content)
			writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
				Type:        "response.output_text.delta",
				ItemID:      messageID,
				OutputIndex: 0,
				ContentIndex: 0,
				Delta:       event.Content,
			})
			flusher.Flush()
		}
		if event.ReasoningContent != "" {
			reasoningBuilder.WriteString(event.ReasoningContent)
		}
		return nil
	})

	if err != nil {
		// Send error event
		writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
			Type: "error",
		})
		flusher.Flush()
		return
	}

	fullText := contentBuilder.String()
	totalReasoning := reasoningBuilder.String()

	// Content part done
	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:        "response.output_text.done",
		ItemID:      messageID,
		OutputIndex: 0,
		ContentIndex: 0,
		Text:        fullText,
	})
	flusher.Flush()

	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:        "response.content_part.done",
		ItemID:      messageID,
		OutputIndex: 0,
		ContentIndex: 0,
		Part: &proxy.ResponsesContentPart{
			Type: "output_text",
			Text: fullText,
		},
	})
	flusher.Flush()

	// Output item done
	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:        "response.output_item.done",
		OutputIndex: 0,
		Item: &proxy.ResponsesOutputItem{
			ID:     messageID,
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []proxy.ResponsesContentPart{
				{Type: "output_text", Text: fullText},
			},
		},
	})
	flusher.Flush()

	// Final response object
	resp := &proxy.ResponsesResponse{
		ID:     responseID,
		Object: "response",
		Model:  responsesReq.Model,
		Status: "completed",
		CreatedAt: createdAt,
		Output: []proxy.ResponsesOutputItem{
			{
				ID:     messageID,
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []proxy.ResponsesContentPart{
					{Type: "output_text", Text: fullText},
				},
			},
		},
		Usage: &proxy.ResponsesUsage{
			InputTokens:  promptTokens,
			OutputTokens: completionTokens,
			TotalTokens:  totalTokens,
			InputTokensDetails: &proxy.ResponsesTokenDetails{
				CachedTokens: 0,
			},
			OutputTokensDetails: &proxy.ResponsesTokenDetails{
				ReasoningTokens: 0,
			},
		},
		Temperature:       responsesReq.Temperature,
		ToolChoice:        responsesReq.ToolChoice,
		Tools:             responsesReq.Tools,
		ParallelToolCalls: true,
		Instructions:      responsesReq.Instructions,
		MaxOutputTokens:   responsesReq.MaxOutputTokens,
		Background:        false,
		Truncation:        "disabled",
	}
	if resp.Tools == nil {
		resp.Tools = []proxy.Tool{}
	}
	if responsesReq.Reasoning != nil {
		resp.Reasoning = responsesReq.Reasoning
	}

	writeResponsesSSE(writer, proxy.ResponsesStreamEvent{
		Type:     "response.completed",
		Response: resp,
	})
	flusher.Flush()

	// Save session and log
	assistantContent := fullText
	if totalReasoning != "" {
		assistantContent = "[thinking]" + totalReasoning + "[/thinking]" + assistantContent
	}
	assistant := proxy.Message{
		Role:    "assistant",
		Content: assistantContent,
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(request.Context(), sessionID, sessionCanonicalRequest, assistant); err == nil {
		service.PersistCanonicalExecutionRecord(
			request.Context(), s.DB, s.Deps.Now(), s.StoreExecutionLogs,
			traceID,
			prePolicyRequest.Protocol,
			"/v1/responses",
			prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
			responsesReq, messages, assistant,
			remoteRequest, rawSSELines,
			promptTokens, completionTokens, totalTokens,
		)
		if s.TokenStats != nil {
			_ = s.TokenStats.AddTokens(request.Context(), totalTokens)
		}
	}
}

func writeResponsesSSE(writer http.ResponseWriter, event proxy.ResponsesStreamEvent) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(event)
	data := strings.TrimSpace(buf.String())
	fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, data)
}
```

**Step 2: Verify compilation**

Run: `go vet ./internal/api/handler/`
Expected: No errors.

**Step 3: Commit**

```bash
git add internal/api/handler/responses.go
git commit -m "feat(api): add Responses API handler with streaming and non-streaming support"
```

---

### Task 8: Register the `/v1/responses` Route

**Files:**
- Modify: `internal/api/router/router.go:35` — add new route

**Step 1: Add the route**

Add after the `/v1/models` route:

```go
mux.HandleFunc("/v1/responses", s.HandleResponses)
```

Full context (after the change):
```go
mux.HandleFunc("/v1/chat/completions", s.HandleChatCompletions)
mux.HandleFunc("/v1/messages", s.HandleAnthropicMessages)
mux.HandleFunc("/v1/models", s.HandleModels)
mux.HandleFunc("/v1/responses", s.HandleResponses)  // <-- NEW
```

**Step 2: Verify compilation**

Run: `go vet ./internal/api/router/`
Expected: No errors.

**Step 3: Commit**

```bash
git add internal/api/router/router.go
git commit -m "feat(router): register /v1/responses endpoint"
```

---

### Task 9: Full Build Verification

**Step 1: Build the entire project**

Run: `go build ./...`
Expected: No errors.

**Step 2: Run all tests**

Run: `go test ./...`
Expected: All tests pass.

**Step 3: Quick manual smoke test**

Start the server: `go run . -config ./config.yaml`

Test non-streaming:
```bash
curl -s http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-coder","input":"Hello, how are you?"}' | jq .
```

Expected: JSON response with `id`, `object: "response"`, `output` array containing text.

Test streaming:
```bash
curl -s http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3-coder","input":"Hello","stream":true}'
```

Expected: SSE events with `event: response.created`, `event: response.output_text.delta`, etc.

**Step 4: Commit any fixes if needed**

---

## Files Summary

| File | Action | Description |
|---|---|---|
| `internal/proxy/types/responses.go` | **Create** | All Responses API types |
| `internal/proxy/types/types.go:72-74` | Modify | Add `CanonicalProtocolResponses` |
| `internal/proxy/message_ir.go` | Modify | Add `CanonicalizeResponsesRequest()` |
| `internal/api/response/writers.go` | Modify | Add `WriteResponsesError()` and `WriteResponsesMappedError()` |
| `internal/api/handler/responses.go` | **Create** | Handler + formatters |
| `internal/api/router/router.go:36` | Modify | Register `/v1/responses` |

## What Is DE-SCOPED (Not Implemented)

- `previous_response_id` — requires session storage integration for Responses-specific state
- `store` — store responses server-side for later retrieval
- Built-in tools (`web_search`, `file_search`, `code_interpreter`, MCP) — don't exist in Lingma
- Tool calls in streaming responses — complex to map from SSE ToolCall events; the IR pipeline does support function calling in non-streaming mode
- Function call output streaming events (`response.function_call_arguments.delta`/`.done`) — only `output_text` events in stream, tool calls returned in non-streaming
