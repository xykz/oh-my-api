# OpenAI Response API 支持 — 实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新增 `POST /v1/responses` 端点，支持 OpenAI Response API 格式请求，通过现有 CanonicalRequest 管道转化为灵码调用，并以 Response API 格式返回结果。

**Architecture:** 方案 A — 独立处理器 + 复用 Canonical IR。新增类型定义（`internal/proxy/types/response.go`）、canonicalization 函数（`internal/proxy/message_ir.go`）、处理器（`internal/api/handler/response.go`），在 router 注册路由。Policy、Session、Vision、Transport、Logs 全部复用现有管道。

**Tech Stack:** Go 1.21+, net/http, encoding/json, 现有项目框架

---

### Task 1: 新增 CanonicalProtocol 常量

**Files:**
- Modify: `internal/proxy/types/types.go:73-74`

**Step 1: 添加常量**

在 `internal/proxy/types/types.go` 第 73 行后新增一行：

```go
const (
    CanonicalProtocolOpenAI    CanonicalProtocol = "openai"
    CanonicalProtocolAnthropic CanonicalProtocol = "anthropic"
    CanonicalProtocolResponse  CanonicalProtocol = "openai_response"  // 新增
)
```

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功（无错误）

**Step 3: Commit**

```bash
git add internal/proxy/types/types.go
git commit -m "feat: add CanonicalProtocolResponse constant"
```

---

### Task 2: 新增 Response API 类型定义

**Files:**
- Create: `internal/proxy/types/response.go`

**Step 1: 创建类型文件**

新建 `internal/proxy/types/response.go`，内容如下：

```go
package types

import "encoding/json"

// ── Request types ─────────────────────────────────────────────────

type OpenAIResponseRequest struct {
    Model              string                `json:"model"`
    Input              json.RawMessage       `json:"input"`
    Instructions       string                `json:"instructions"`
    MaxOutputTokens    int                   `json:"max_output_tokens"`
    Temperature        float64               `json:"temperature"`
    TopP               float64               `json:"top_p"`
    Stream             bool                  `json:"stream"`
    Store              bool                  `json:"store"`
    PreviousResponseID string                `json:"previous_response_id"`
    Tools              []OpenAIResponseTool  `json:"tools"`
    ToolChoice         any                   `json:"tool_choice"`
    Conversation       string                `json:"conversation"`
    Metadata           map[string]string     `json:"metadata"`
}

type ResponseInputItem struct {
    Type      string                     `json:"type"`
    Role      string                     `json:"role,omitempty"`
    Content   json.RawMessage            `json:"content,omitempty"`
    Name      string                     `json:"name,omitempty"`
    Arguments string                     `json:"arguments,omitempty"`
    CallID    string                     `json:"call_id,omitempty"`
    Output    string                     `json:"output,omitempty"`
    Summary   []ResponseInputContentPart `json:"summary,omitempty"`
}

type ResponseInputContentPart struct {
    Type     string        `json:"type"`
    Text     string        `json:"text,omitempty"`
    ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

type ImageURLPart struct {
    URL    string `json:"url"`
    Detail string `json:"detail,omitempty"`
}

type OpenAIResponseTool struct {
    Type        string `json:"type"`
    Name        string `json:"name,omitempty"`
    Description string `json:"description,omitempty"`
    Parameters  any    `json:"parameters,omitempty"`
}

// ── Response types ────────────────────────────────────────────────

type OpenAIResponse struct {
    ID                 string                    `json:"id"`
    Object             string                    `json:"object"`
    CreatedAt          int64                     `json:"created_at"`
    Status             string                    `json:"status"`
    StatusDetails      *ResponseStatusDetails    `json:"status_details,omitempty"`
    Model              string                    `json:"model"`
    Output             []ResponseOutputItem      `json:"output"`
    Usage              *OpenAIResponseUsage      `json:"usage,omitempty"`
    Instructions       string                    `json:"instructions,omitempty"`
    MaxOutputTokens    int                       `json:"max_output_tokens"`
    Temperature        float64                   `json:"temperature"`
    TopP               float64                   `json:"top_p"`
    ParallelToolCalls  bool                      `json:"parallel_tool_calls"`
    PreviousResponseID string                    `json:"previous_response_id,omitempty"`
}

type ResponseStatusDetails struct {
    Type   string `json:"type"`
    Reason string `json:"reason"`
}

type ResponseOutputItem struct {
    ID        string                  `json:"id"`
    Type      string                  `json:"type"`
    Status    string                  `json:"status"`
    Role      string                  `json:"role,omitempty"`
    Content   []ResponseOutputContent `json:"content,omitempty"`
    Name      string                  `json:"name,omitempty"`
    Arguments string                  `json:"arguments,omitempty"`
    CallID    string                  `json:"call_id,omitempty"`
    Summary   []ResponseOutputContent `json:"summary,omitempty"`
}

type ResponseOutputContent struct {
    Type        string               `json:"type"`
    Text        string               `json:"text"`
    Annotations []ResponseAnnotation `json:"annotations,omitempty"`
}

type ResponseAnnotation struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
    URL  string `json:"url,omitempty"`
}

type OpenAIResponseUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
    TotalTokens  int `json:"total_tokens"`
}

// ── Streaming types ───────────────────────────────────────────────

type ResponseStreamEvent struct {
    Type         string              `json:"type"`
    Response     *OpenAIResponse     `json:"response,omitempty"`
    ItemID       string              `json:"item_id,omitempty"`
    OutputIndex  int                 `json:"output_index,omitempty"`
    ContentIndex int                 `json:"content_index,omitempty"`
    Delta        string              `json:"delta,omitempty"`
    Item         *ResponseOutputItem `json:"item,omitempty"`
    Part         *ResponseOutputItem `json:"part,omitempty"`
}
```

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 3: Commit**

```bash
git add internal/proxy/types/response.go
git commit -m "feat: add Response API type definitions"
```

---

### Task 3: 实现 CanonicalizeOpenAIResponseRequest

**Files:**
- Modify: `internal/proxy/message_ir.go`

**Step 1: 添加 canonicalization 函数**

在 `internal/proxy/message_ir.go` 末尾（`imageBlockMetadata` 函数之后）添加以下代码：

```go
// CanonicalizeOpenAIResponseRequest converts an OpenAI Response API request
// to the canonical internal representation.
func CanonicalizeOpenAIResponseRequest(req OpenAIResponseRequest, sessionID string) (CanonicalRequest, error) {
    if req.Model == "" {
        return CanonicalRequest{}, fmt.Errorf("model is required")
    }
    if sessionID == "" {
        sessionID = strings.TrimSpace(req.PreviousResponseID)
    }

    turns := make([]CanonicalTurn, 0, 8)

    // Inject instructions as system turn at the beginning
    if req.Instructions != "" {
        turns = append(turns, CanonicalTurn{
            Role: "system",
            Blocks: []CanonicalContentBlock{{
                Type: CanonicalBlockText,
                Text: req.Instructions,
            }},
        })
    }

    // Parse input: string or []ResponseInputItem
    inputItems, err := parseResponseInput(req.Input)
    if err != nil {
        return CanonicalRequest{}, fmt.Errorf("parse input: %w", err)
    }

    for _, item := range inputItems {
        turn, err := canonicalizeResponseInputItem(item)
        if err != nil {
            return CanonicalRequest{}, err
        }
        turns = append(turns, turn)
    }

    // Separate function tools from built-in tools
    functionTools, builtinToolTypes := splitResponseTools(req.Tools)

    return CanonicalRequest{
        SchemaVersion: 1,
        Protocol:      CanonicalProtocolResponse,
        Model:         req.Model,
        Stream:        req.Stream,
        Temperature:   req.Temperature,
        Tools:         functionTools,
        ToolChoice:    normalizeResponseToolChoice(req.ToolChoice),
        HasTools:      len(functionTools) > 0,
        HasReasoning:  hasReasoningInputItems(inputItems),
        SessionID:     sessionID,
        Metadata:      buildResponseMetadata(req, builtinToolTypes),
        Turns:         turns,
    }, nil
}

// parseResponseInput handles both string and []ResponseInputItem formats.
func parseResponseInput(raw json.RawMessage) ([]ResponseInputItem, error) {
    raw = json.RawMessage(strings.TrimSpace(string(raw)))
    if len(raw) == 0 {
        return nil, nil
    }

    // Case 1: plain string
    if raw[0] == '"' {
        var text string
        if err := json.Unmarshal(raw, &text); err != nil {
            return nil, err
        }
        return []ResponseInputItem{{Type: "message", Role: "user", Content: json.RawMessage(`"` + text + `"`)}}, nil
    }

    // Case 2: array of items
    if raw[0] == '[' {
        var items []ResponseInputItem
        if err := json.Unmarshal(raw, &items); err != nil {
            return nil, fmt.Errorf("unmarshal input items: %w", err)
        }
        return items, nil
    }

    return nil, fmt.Errorf("unsupported input format: %s", string(raw[:min(60, len(raw))]))
}

// canonicalizeResponseInputItem converts a single ResponseInputItem to a CanonicalTurn.
func canonicalizeResponseInputItem(item ResponseInputItem) (CanonicalTurn, error) {
    switch item.Type {
    case "message":
        return canonicalizeResponseMessageItem(item)
    case "function_call":
        return canonicalizeResponseFunctionCallItem(item)
    case "function_call_output":
        return canonicalizeResponseFunctionCallOutputItem(item)
    case "reasoning":
        return canonicalizeResponseReasoningItem(item)
    default:
        return CanonicalTurn{}, fmt.Errorf("unsupported input item type %q", item.Type)
    }
}

func canonicalizeResponseMessageItem(item ResponseInputItem) (CanonicalTurn, error) {
    role := item.Role
    switch role {
    case "user", "system", "developer":
        if role == "developer" {
            role = "system"
        }
    case "assistant":
        // ok
    default:
        return CanonicalTurn{}, fmt.Errorf("unsupported message role %q", item.Role)
    }

    turn := CanonicalTurn{Role: role}
    blocks, err := parseResponseMessageContent(item.Content, role)
    if err != nil {
        return CanonicalTurn{}, fmt.Errorf("parse message content: %w", err)
    }
    turn.Blocks = blocks
    return turn, nil
}

func parseResponseMessageContent(raw json.RawMessage, role string) ([]CanonicalContentBlock, error) {
    raw = json.RawMessage(strings.TrimSpace(string(raw)))
    if len(raw) == 0 {
        return nil, nil
    }

    // Plain string content
    if raw[0] == '"' {
        var text string
        if err := json.Unmarshal(raw, &text); err != nil {
            return nil, err
        }
        if text == "" {
            return nil, nil
        }
        return []CanonicalContentBlock{{Type: CanonicalBlockText, Text: text}}, nil
    }

    // Array of content parts
    if raw[0] == '[' {
        var parts []ResponseInputContentPart
        if err := json.Unmarshal(raw, &parts); err != nil {
            return nil, fmt.Errorf("unmarshal content parts: %w", err)
        }
        var blocks []CanonicalContentBlock
        for i, part := range parts {
            block, err := canonicalizeResponseContentPart(part, i)
            if err != nil {
                return nil, err
            }
            if block != nil {
                blocks = append(blocks, *block)
            }
        }
        return blocks, nil
    }

    return nil, fmt.Errorf("unsupported content format: %s", string(raw[:min(60, len(raw))]))
}

func canonicalizeResponseContentPart(part ResponseInputContentPart, index int) (*CanonicalContentBlock, error) {
    switch part.Type {
    case "input_text":
        if part.Text == "" {
            return nil, nil
        }
        return &CanonicalContentBlock{Type: CanonicalBlockText, Text: part.Text}, nil
    case "input_image":
        if part.ImageURL == nil {
            return nil, fmt.Errorf("input_image at index %d: image_url is nil", index)
        }
        src, err := parseOpenAIImageURL(part.ImageURL.URL)
        if err != nil {
            return nil, fmt.Errorf("input_image at index %d: %w", index, err)
        }
        rawSrc, _ := json.Marshal(src)
        return &CanonicalContentBlock{
            Type:     CanonicalBlockImage,
            Data:     json.RawMessage(rawSrc),
            Metadata: imageBlockMetadata(src, index),
        }, nil
    case "input_file":
        // Files not supported by current backend; skip with metadata
        return nil, nil
    default:
        return nil, fmt.Errorf("unsupported content part type %q", part.Type)
    }
}

func canonicalizeResponseFunctionCallItem(item ResponseInputItem) (CanonicalTurn, error) {
    turn := CanonicalTurn{Role: "assistant"}
    if item.Name != "" {
        turn.Name = item.Name
    }
    turn.Blocks = append(turn.Blocks, CanonicalContentBlock{
        Type: CanonicalBlockToolCall,
        ToolCall: &CanonicalToolCall{
            ID:        item.CallID,
            Name:      item.Name,
            Arguments: item.Arguments,
        },
    })
    return turn, nil
}

func canonicalizeResponseFunctionCallOutputItem(item ResponseInputItem) (CanonicalTurn, error) {
    return CanonicalTurn{
        Role: "tool",
        Blocks: []CanonicalContentBlock{{
            Type: CanonicalBlockToolResult,
            ToolResult: &CanonicalToolResult{
                ToolCallID: item.CallID,
                Content:    item.Output,
            },
        }},
    }, nil
}

func canonicalizeResponseReasoningItem(item ResponseInputItem) (CanonicalTurn, error) {
    turn := CanonicalTurn{Role: "assistant"}
    for _, part := range item.Summary {
        if part.Type == "input_text" || part.Text != "" {
            turn.Blocks = append(turn.Blocks, CanonicalContentBlock{
                Type: CanonicalBlockReasoning,
                Text: part.Text,
            })
        }
    }
    return turn, nil
}

// splitResponseTools separates function tools from built-in OpenAI tools.
func splitResponseTools(tools []OpenAIResponseTool) ([]CanonicalToolDefinition, []string) {
    var functionTools []CanonicalToolDefinition
    var builtinTypes []string
    for _, tool := range tools {
        switch tool.Type {
        case "function":
            var params json.RawMessage
            if tool.Parameters != nil {
                data, err := json.Marshal(tool.Parameters)
                if err == nil {
                    params = data
                }
            }
            functionTools = append(functionTools, CanonicalToolDefinition{
                Type:        "function",
                Name:        tool.Name,
                Description: tool.Description,
                Parameters:  params,
            })
        case "web_search_preview", "file_search", "code_interpreter":
            builtinTypes = append(builtinTypes, tool.Type)
        }
    }
    return functionTools, builtinTypes
}

func normalizeResponseToolChoice(toolChoice any) any {
    if toolChoice == nil {
        return nil
    }
    if s, ok := toolChoice.(string); ok {
        switch s {
        case "auto", "none", "required":
            return s
        }
    }
    return toolChoice
}

func hasReasoningInputItems(items []ResponseInputItem) bool {
    for _, item := range items {
        if item.Type == "reasoning" {
            return true
        }
    }
    return false
}

func buildResponseMetadata(req OpenAIResponseRequest, builtinToolTypes []string) map[string]any {
    metadata := make(map[string]any)
    if len(builtinToolTypes) > 0 {
        metadata["openai_builtin_tools"] = builtinToolTypes
    }
    if req.Conversation != "" {
        metadata["conversation"] = req.Conversation
    }
    return metadata
}
```

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 3: Commit**

```bash
git add internal/proxy/message_ir.go
git commit -m "feat: add CanonicalizeOpenAIResponseRequest"
```

---

### Task 4: 新增 Response API 响应写入器

**Files:**
- Modify: `internal/api/response/writers.go`

**Step 1: 添加 Response API 专用 error writer**

在 `internal/api/response/writers.go` 末尾添加：

```go
func WriteResponseError(writer http.ResponseWriter, statusCode int, reason string) {
    writer.Header().Set("Content-Type", "application/json")
    writer.WriteHeader(statusCode)
    _ = json.NewEncoder(writer).Encode(proxy.OpenAIResponse{
        Object:       "response",
        Status:       "failed",
        StatusDetails: &proxy.ResponseStatusDetails{
            Type:   "invalid_request_error",
            Reason: reason,
        },
    })
}

func WriteResponseSSE(writer http.ResponseWriter, event proxy.ResponseStreamEvent) {
    data, err := json.Marshal(event)
    if err != nil {
        return
    }
    fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, data)
}
```

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 3: Commit**

```bash
git add internal/api/response/writers.go
git commit -m "feat: add Response API error and SSE writers"
```

---

### Task 5: 实现 Response API 处理器（基本骨架 + 非流式）

**Files:**
- Create: `internal/api/handler/response.go`

**Step 1: 创建处理器文件（基础结构 + 非流式逻辑）**

```go
package handler

import (
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

func (s *Server) HandleResponses(writer http.ResponseWriter, request *http.Request) {
    if request.Method != http.MethodPost {
        response.WriteMethodNotAllowed(writer, http.MethodPost)
        return
    }

    body := http.MaxBytesReader(writer, request.Body, 2<<20)
    defer body.Close()

    var respReq proxy.OpenAIResponseRequest
    if err := json.NewDecoder(body).Decode(&respReq); err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, "invalid JSON: "+err.Error())
        return
    }

    if respReq.Model == "" {
        response.WriteResponseError(writer, http.StatusBadRequest, "model is required")
        return
    }

    sessionID := service.RequestSessionID(request, respReq.PreviousResponseID)
    canonicalRequest, err := proxy.CanonicalizeOpenAIResponseRequest(respReq, sessionID)
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
        return
    }
    service.AttachCanonicalRequestMetadata(&canonicalRequest, request.Header)

    if err := proxy.ValidateVisionLimits(canonicalRequest); err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
        return
    }

    var visionStore model.SettingsStore
    if s.DB != nil {
        visionStore = s.DB
    }
    if _, err := service.EvaluateVisionGate(request.Context(), visionStore, canonicalRequest); err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, err.Error())
        return
    }

    policyResult, err := service.EvaluateCanonicalRequest(request.Context(), s.DB, canonicalRequest)
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "policy: "+err.Error())
        return
    }
    sessionCanonicalRequest, err := s.Deps.Sessions.BuildCanonicalRequest(request.Context(), sessionID, policyResult.PostPolicyRequest)
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "sessions: "+err.Error())
        return
    }
    projectedRequest, projectedMessages, err := proxy.ProjectCanonicalToOpenAIRequest(sessionCanonicalRequest)
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
        return
    }
    if err := validateChatRequest(&projectedRequest); err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
        return
    }

    messages := projectedMessages
    modelKey, err := s.Deps.Models.ResolveChatModel(request.Context(), projectedRequest.Model)
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadRequest, "resolve model: "+err.Error())
        return
    }

    if s.accountRoutingEnabled() {
        s.handleAccountRoutedResponses(
            writer, request, respReq, projectedRequest, sessionCanonicalRequest,
            canonicalRequest, policyResult.PostPolicyRequest, sessionID, messages, modelKey,
        )
        return
    }

    snapshot, err := s.Deps.Credentials.Current(request.Context())
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "credentials: "+err.Error())
        return
    }

    if s.Deps.Uploader != nil {
        imageURLs, err := s.uploadImagesFromCanonicalRequest(request.Context(), snapshot, sessionCanonicalRequest)
        if err != nil {
            response.WriteResponseError(writer, http.StatusInternalServerError, err.Error())
            return
        }
        if len(imageURLs) > 0 {
            sessionCanonicalRequest.Metadata = service.CloneMetadataMap(sessionCanonicalRequest.Metadata)
            if sessionCanonicalRequest.Metadata == nil {
                sessionCanonicalRequest.Metadata = map[string]any{}
            }
            sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
            sessionCanonicalRequest.Metadata["is_vl"] = true
        }
    }

    remoteRequest, err := s.Deps.Builder.BuildCanonical(sessionCanonicalRequest, modelKey)
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "build request: "+err.Error())
        return
    }

    stream, err := s.Deps.Transport.StreamChat(request.Context(), remoteRequest, snapshot)
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
        return
    }
    defer stream.Close()

    responseID := "resp_" + remoteRequest.RequestID
    traceID := proxy.NewUUID()

    if !respReq.Stream {
        s.nonStreamResponse(writer, request.Context(), stream, responseID, projectedRequest, remoteRequest,
            sessionID, messages, canonicalRequest, policyResult.PostPolicyRequest,
            sessionCanonicalRequest, traceID, respReq)
    } else {
        s.streamResponse(writer, request, stream, responseID, projectedRequest, remoteRequest,
            sessionID, messages, canonicalRequest, policyResult.PostPolicyRequest,
            sessionCanonicalRequest, traceID, respReq)
    }
}

func (s *Server) nonStreamResponse(
    writer http.ResponseWriter,
    ctx context.Context,
    stream io.Reader,
    responseID string,
    chatRequest proxy.OpenAIChatRequest,
    remoteRequest proxy.RemoteChatRequest,
    sessionID string,
    messages []proxy.Message,
    prePolicyRequest proxy.CanonicalRequest,
    postPolicyRequest proxy.CanonicalRequest,
    sessionCanonicalRequest proxy.CanonicalRequest,
    traceID string,
    respReq proxy.OpenAIResponseRequest,
) {
    var contentBuilder strings.Builder
    var reasoningBuilder strings.Builder
    var rawSSELines []string
    var promptTokens, completionTokens, totalTokens int
    var toolCalls []proxy.ToolCall

    err := proxy.ScanSSEWithLines(stream, func(line string) error {
        rawSSELines = append(rawSSELines, line)
        return nil
    }, func(event proxy.SSEEvent) error {
        if event.Usage != nil {
            promptTokens = event.Usage.PromptTokens
            completionTokens = event.Usage.CompletionTokens
            totalTokens = event.Usage.TotalTokens
        }
        contentBuilder.WriteString(event.Content)
        reasoningBuilder.WriteString(event.ReasoningContent)
        toolCalls = append(toolCalls, event.ToolCalls...)
        return nil
    })
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
        return
    }

    output := buildResponseOutput(responseID, contentBuilder.String(), reasoningBuilder.String(), toolCalls, respReq)
    if len(output) == 0 {
        output = append(output, proxy.ResponseOutputItem{
            ID:     responseID + "_msg_0",
            Type:   "message",
            Status: "completed",
            Role:   "assistant",
            Content: []proxy.ResponseOutputContent{{
                Type: "output_text",
                Text: "",
            }},
        })
    }

    if promptTokens <= 0 && completionTokens <= 0 {
        promptTokens = len(contentBuilder.String())/4 + len(reasoningBuilder.String())/4
        completionTokens = promptTokens
        totalTokens = promptTokens + completionTokens
    }

    assistant := proxy.Message{
        Role:    "assistant",
        Content: buildAssistantContent(contentBuilder.String(), reasoningBuilder.String()),
    }
    _ = s.Deps.Sessions.SaveCanonicalResponse(ctx, sessionID, sessionCanonicalRequest, assistant)

    service.PersistCanonicalExecutionRecord(
        ctx, s.DB, s.Deps.Now(), s.StoreExecutionLogs, traceID,
        prePolicyRequest.Protocol, "/v1/responses",
        prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
        chatRequest, messages, assistant, remoteRequest,
        rawSSELines, promptTokens, completionTokens, totalTokens,
    )
    if s.TokenStats != nil {
        _ = s.TokenStats.AddTokens(ctx, totalTokens)
    }

    builtinTools, _ := canonicalRequest.Metadata["openai_builtin_tools"].([]string)
    output = appendBuiltinToolOutput(output, responseID, builtinTools)

    response.WriteJSON(writer, http.StatusOK, proxy.OpenAIResponse{
        ID:                 responseID,
        Object:             "response",
        CreatedAt:          s.Deps.Now().Unix(),
        Status:             "completed",
        Model:              chatRequest.Model,
        Output:             output,
        Instructions:       respReq.Instructions,
        MaxOutputTokens:    respReq.MaxOutputTokens,
        Temperature:        respReq.Temperature,
        TopP:               respReq.TopP,
        PreviousResponseID: respReq.PreviousResponseID,
        Usage: &proxy.OpenAIResponseUsage{
            InputTokens:  promptTokens,
            OutputTokens: completionTokens,
            TotalTokens:  totalTokens,
        },
    })
}

func buildResponseOutput(
    responseID string,
    content string,
    reasoning string,
    toolCalls []proxy.ToolCall,
    _ proxy.OpenAIResponseRequest,
) []proxy.ResponseOutputItem {
    var output []proxy.ResponseOutputItem
    idx := 0

    if reasoning != "" {
        output = append(output, proxy.ResponseOutputItem{
            ID:     fmt.Sprintf("%s_reasoning_%d", responseID, idx),
            Type:   "reasoning",
            Status: "completed",
            Summary: []proxy.ResponseOutputContent{{
                Type: "output_text",
                Text: reasoning,
            }},
        })
        idx++
    }

    if content != "" || len(toolCalls) == 0 {
        output = append(output, proxy.ResponseOutputItem{
            ID:     fmt.Sprintf("%s_msg_%d", responseID, idx),
            Type:   "message",
            Status: "completed",
            Role:   "assistant",
            Content: []proxy.ResponseOutputContent{{
                Type: "output_text",
                Text: content,
            }},
        })
        idx++
    }

    merged := mergeToolCallDeltas(toolCalls)
    for _, tc := range merged {
        output = append(output, proxy.ResponseOutputItem{
            ID:        fmt.Sprintf("%s_fc_%d", responseID, idx),
            Type:      "function_call",
            Status:    "completed",
            Name:      tc.Function.Name,
            Arguments: tc.Function.Arguments,
            CallID:    tc.ID,
        })
        idx++
    }

    return output
}

func appendBuiltinToolOutput(output []proxy.ResponseOutputItem, responseID string, builtinTypes []string) []proxy.ResponseOutputItem {
    for _, bt := range builtinTypes {
        output = append(output, proxy.ResponseOutputItem{
            ID:     fmt.Sprintf("%s_bt_%s", responseID, bt),
            Type:   "message",
            Status: "incomplete",
            Role:   "assistant",
            Content: []proxy.ResponseOutputContent{{
                Type: "output_text",
                Text: fmt.Sprintf("[Built-in tool %q is not available in this backend]", bt),
            }},
        })
    }
    return output
}

func buildAssistantContent(content string, reasoning string) string {
    if reasoning != "" {
        return "[thinking]" + reasoning + "[/thinking]" + content
    }
    return content
}
```

> **注意**: 此文件引用了 `canonicalRequest` 变量名，在 `nonStreamResponse` 中用于获取 `builtin tools` 时应使用 `prePolicyRequest` 或在 `HandleResponses` 上部保存引用。

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 3: Commit**

```bash
git add internal/api/handler/response.go
git commit -m "feat: add Response API handler (non-streaming)"
```

---

### Task 6: 实现流式响应 + AccountRouting

**Files:**
- Modify: `internal/api/handler/response.go`

**Step 1: 添加流式响应方法和 AccountRouting 方法**

在 `internal/api/handler/response.go` 末尾追加：

```go
func (s *Server) streamResponse(
    writer http.ResponseWriter,
    request *http.Request,
    stream io.Reader,
    responseID string,
    chatRequest proxy.OpenAIChatRequest,
    remoteRequest proxy.RemoteChatRequest,
    sessionID string,
    messages []proxy.Message,
    prePolicyRequest proxy.CanonicalRequest,
    postPolicyRequest proxy.CanonicalRequest,
    sessionCanonicalRequest proxy.CanonicalRequest,
    traceID string,
    respReq proxy.OpenAIResponseRequest,
) {
    flusher, ok := writer.(http.Flusher)
    if !ok {
        response.WriteResponseError(writer, http.StatusInternalServerError, "streaming unsupported")
        return
    }

    writer.Header().Set("Content-Type", "text/event-stream")
    writer.Header().Set("Cache-Control", "no-cache")
    writer.Header().Set("Connection", "keep-alive")

    // response.created
    response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
        Type: "response.created",
        Response: &proxy.OpenAIResponse{
            ID:           responseID,
            Object:       "response",
            Status:       "in_progress",
            CreatedAt:    s.Deps.Now().Unix(),
            Model:        chatRequest.Model,
        },
    })
    flusher.Flush()

    // response.in_progress
    response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
        Type: "response.in_progress",
        Response: &proxy.OpenAIResponse{
            ID:           responseID,
            Object:       "response",
            Status:       "in_progress",
            CreatedAt:    s.Deps.Now().Unix(),
            Model:        chatRequest.Model,
        },
    })
    flusher.Flush()

    var contentBuilder strings.Builder
    var reasoningBuilder strings.Builder
    var rawSSELines []string
    var promptTokens, completionTokens, totalTokens int
    var hasReasoning, hasText, hasToolCall bool
    outputIndex := 0
    contentIndex := 0
    itemStarted := false
    currentItemType := ""
    currentItemID := ""

    startItem := func(itemType string) {
        if itemStarted {
            closeItem()
        }
        switch itemType {
        case "reasoning":
            currentItemID = fmt.Sprintf("%s_reasoning_%d", responseID, outputIndex)
        case "message":
            currentItemID = fmt.Sprintf("%s_msg_%d", responseID, outputIndex)
        case "function_call":
            currentItemID = fmt.Sprintf("%s_fc_%d", responseID, outputIndex)
        }
        outputIndex++
        currentItemType = itemType
        itemStarted = true

        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type: "response.output_item.added",
            OutputIndex: outputIndex - 1,
            Item: &proxy.ResponseOutputItem{
                ID:      currentItemID,
                Type:    itemType,
                Status:  "in_progress",
            },
        })
        flusher.Flush()
    }

    startContentPart := func() {
        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type:         "response.content_part.added",
            ItemID:       currentItemID,
            OutputIndex:  outputIndex - 1,
            ContentIndex: contentIndex,
            Part: &proxy.ResponseOutputItem{
                Type: "message",
                Content: []proxy.ResponseOutputContent{{
                    Type: "output_text",
                }},
            },
        })
        flusher.Flush()
    }

    closeContentPart := func() {
        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type:         "response.content_part.done",
            ItemID:       currentItemID,
            OutputIndex:  outputIndex - 1,
            ContentIndex: contentIndex,
        })
        flusher.Flush()
        contentIndex++
    }

    closeItem := func() {
        if !itemStarted {
            return
        }
        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type:        "response.output_item.done",
            ItemID:      currentItemID,
            OutputIndex: outputIndex - 1,
            Item: &proxy.ResponseOutputItem{
                ID:     currentItemID,
                Type:   currentItemType,
                Status: "completed",
            },
        })
        flusher.Flush()
        itemStarted = false
    }

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

        if event.ReasoningContent != "" {
            if !hasReasoning {
                startItem("reasoning")
                hasReasoning = true
            }
            hasText = false
            reasoningBuilder.WriteString(event.ReasoningContent)
            // reasoning_summary_part.added (first time)
            // reasoning_summary_part.delta
            response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
                Type:        "response.reasoning_summary_part.added",
                ItemID:      currentItemID,
                OutputIndex: outputIndex - 1,
            })
            flusher.Flush()
            return nil
        }

        if event.Content != "" {
            if !hasText {
                closeItem()
                startItem("message")
                startContentPart()
                hasText = true
            }
            contentBuilder.WriteString(event.Content)
            response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
                Type:         "response.output_text.delta",
                ItemID:       currentItemID,
                OutputIndex:  outputIndex - 1,
                ContentIndex: contentIndex,
                Delta:        event.Content,
            })
            flusher.Flush()
            return nil
        }

        for _, tc := range event.ToolCalls {
            isNew := tc.ID != "" || tc.Function.Name != ""
            if isNew {
                closeItem()
                closeContentPart()
                startItem("function_call")
                hasToolCall = true
                hasText = false
            }
            if tc.Function.Arguments != "" {
                response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
                    Type:        "response.function_call_arguments.delta",
                    ItemID:      currentItemID,
                    OutputIndex: outputIndex - 1,
                    Delta:       tc.Function.Arguments,
                })
                flusher.Flush()
            }
        }
        return nil
    })

    if err != nil {
        closeContentPart()
        closeItem()
        var errorBuf strings.Builder
        errorBuf.WriteString(`{"type":"error","message":"`)
        errorBuf.WriteString(err.Error())
        errorBuf.WriteString(`"}`)
        fmt.Fprintf(writer, "event: error\ndata: %s\n\n", errorBuf.String())
        flusher.Flush()
        return
    }

    if hasText {
        // output_text.done
        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type:         "response.output_text.done",
            ItemID:       currentItemID,
            OutputIndex:  outputIndex - 1,
            ContentIndex: contentIndex,
            Delta:        "",
        })
        flusher.Flush()

        // content_part.done
        closeContentPart()
    }

    if hasReasoning {
        response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
            Type:        "response.reasoning_summary_part.done",
            ItemID:      responseID + "_reasoning_0",
            OutputIndex: 0,
        })
        flusher.Flush()
    }

    closeItem()

    if completionTokens <= 0 {
        completionTokens = (contentBuilder.Len() + reasoningBuilder.Len()) / 4
        if completionTokens == 0 {
            completionTokens = 1
        }
    }
    if promptTokens <= 0 {
        promptTokens = completionTokens * 2
    }
    totalTokens = promptTokens + completionTokens

    // response.completed
    response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
        Type: "response.completed",
        Response: &proxy.OpenAIResponse{
            ID:      responseID,
            Object:  "response",
            Status:  "completed",
            Model:   chatRequest.Model,
            Usage: &proxy.OpenAIResponseUsage{
                InputTokens:  promptTokens,
                OutputTokens: completionTokens,
                TotalTokens:  totalTokens,
            },
        },
    })
    flusher.Flush()

    assistant := proxy.Message{
        Role:    "assistant",
        Content: buildAssistantContent(contentBuilder.String(), reasoningBuilder.String()),
    }
    _ = s.Deps.Sessions.SaveCanonicalResponse(request.Context(), sessionID, sessionCanonicalRequest, assistant)

    service.PersistCanonicalExecutionRecord(
        request.Context(), s.DB, s.Deps.Now(), s.StoreExecutionLogs, traceID,
        prePolicyRequest.Protocol, "/v1/responses",
        prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
        chatRequest, messages, assistant, remoteRequest,
        rawSSELines, promptTokens, completionTokens, totalTokens,
    )
    if s.TokenStats != nil {
        _ = s.TokenStats.AddTokens(request.Context(), totalTokens)
    }
}

func (s *Server) handleAccountRoutedResponses(
    writer http.ResponseWriter,
    request *http.Request,
    respReq proxy.OpenAIResponseRequest,
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
        response.WriteResponseError(writer, http.StatusInternalServerError, "routing: "+err.Error())
        return
    }
    attachAccountRoutingMetadata(&prePolicyRequest, account)
    attachAccountRoutingMetadata(&postPolicyRequest, account)
    attachAccountRoutingMetadata(&sessionCanonicalRequest, account)

    imageURLs, err := s.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "upload: "+err.Error())
        return
    }
    if len(imageURLs) > 0 {
        sessionCanonicalRequest.Metadata = service.CloneMetadataMap(sessionCanonicalRequest.Metadata)
        if sessionCanonicalRequest.Metadata == nil {
            sessionCanonicalRequest.Metadata = map[string]any{}
        }
        sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
        sessionCanonicalRequest.Metadata["is_vl"] = true
    }

    remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
    if err != nil {
        response.WriteResponseError(writer, http.StatusInternalServerError, "build request: "+err.Error())
        return
    }
    stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
    if err != nil {
        response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
        return
    }
    defer stream.Close()

    responseID := "resp_" + remoteRequest.RequestID
    traceID := proxy.NewUUID()

    if !respReq.Stream {
        s.nonStreamResponse(writer, request.Context(), stream, responseID, projectedRequest, remoteRequest,
            sessionID, messages, prePolicyRequest, postPolicyRequest,
            sessionCanonicalRequest, traceID, respReq)
    } else {
        s.streamResponse(writer, request, stream, responseID, projectedRequest, remoteRequest,
            sessionID, messages, prePolicyRequest, postPolicyRequest,
            sessionCanonicalRequest, traceID, respReq)
    }
}
```

**Step 2: 修复 nonStreamResponse 中的变量引用问题**

在 `HandleResponses` 中，`canonicalRequest` 变量在 pipeline 流程后被 `policyResult.PostPolicyRequest` 等变量覆盖。需要在 `nonStreamResponse` 中使用 `prePolicyRequest` 而不是顶层变量。

修改 `nonStreamResponse` 调用处的 builtin tool 获取逻辑：

```go
// 将
builtinTools, _ := canonicalRequest.Metadata["openai_builtin_tools"].([]string)
// 改为
builtinTools, _ := prePolicyRequest.Metadata["openai_builtin_tools"].([]string)
```

**Step 3: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 4: Commit**

```bash
git add internal/api/handler/response.go
git commit -m "feat: add streaming response and account routing for Response API"
```

---

### Task 7: 注册路由

**Files:**
- Modify: `internal/api/router/router.go:36`

**Step 1: 添加路由**

在 `internal/api/router/router.go` 中 `HandleAnthropicMessages` 行后添加：

```go
mux.HandleFunc("/v1/chat/completions", s.HandleChatCompletions)
mux.HandleFunc("/v1/messages", s.HandleAnthropicMessages)
mux.HandleFunc("/v1/responses", s.HandleResponses)  // 新增
mux.HandleFunc("/v1/models", s.HandleModels)
```

**Step 2: 验证编译**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`
Expected: 编译成功

**Step 3: Commit**

```bash
git add internal/api/router/router.go
git commit -m "feat: register /v1/responses route"
```

---

### Task 8: 修复 nonStreamResponse 中的变量引用

**Files:**
- Modify: `internal/api/handler/response.go`

**Step 1: 检查并修复编译错误**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go build ./...`

如果有编译错误（`canonicalRequest` 未定义等），修复：

- 在 `HandleResponses` 中将 `canonicalRequest` 重命名为 `prePolicyCanonicalReq`（更清晰地表明它的角色）
- 在 `nonStreamResponse` 中正确引用

或者在方法签名中传入 `prePolicyRequest` 以获取 builtin tools:

```go
// nonStreamResponse 签名中添加 respCanonicalRequest:
func (s *Server) nonStreamResponse(
    writer http.ResponseWriter,
    ctx context.Context,
    stream io.Reader,
    responseID string,
    chatRequest proxy.OpenAIChatRequest,
    remoteRequest proxy.RemoteChatRequest,
    sessionID string,
    messages []proxy.Message,
    prePolicyRequest proxy.CanonicalRequest,
    postPolicyRequest proxy.CanonicalRequest,
    sessionCanonicalRequest proxy.CanonicalRequest,
    traceID string,
    respReq proxy.OpenAIResponseRequest,
    respCanonicalRequest proxy.CanonicalRequest, // 新增：用于获取 builtin tools metadata
) {
    // ...
    // 使用 respCanonicalRequest 获取 builtin tools:
    builtinTools, _ := respCanonicalRequest.Metadata["openai_builtin_tools"].([]string)
    // ...
}
```

并在所有调用 `nonStreamResponse` 的地方传入 `prePolicyRequest`。

**Step 2: 验证编译并测试**

Run: 
```bash
cd d:/Repositories/MyRepository/FuckLingma && go build ./... && go vet ./...
```
Expected: 编译成功，无 vet 错误

**Step 3: Commit**

```bash
git add internal/api/handler/response.go
git commit -m "fix: resolve variable references in nonStreamResponse"
```

---

### Task 9: 添加单元测试

**Files:**
- Create: `internal/api/handler/response_test.go`

**Step 1: 创建测试文件**

```go
package handler

import (
    "testing"
    "github.com/rizxfrog/oh-my-api/internal/proxy"
)

// Test via proxy package since canonicalization logic lives there
```

> **注意**: 单元测试应在 `internal/proxy` 包中，因为 `CanonicalizeOpenAIResponseRequest` 位于该包。

**Files:**
- Create: `internal/proxy/response_api_test.go`

**Step 2: 创建 canonicalization 单元测试**

```go
package proxy

import (
    "encoding/json"
    "testing"
)

func TestCanonicalizeOpenAIResponseRequest_StringInput(t *testing.T) {
    input := `"Hello, world!"`
    raw := json.RawMessage(input)
    req := OpenAIResponseRequest{
        Model: "test-model",
        Input: raw,
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "sess-1")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if canonical.Protocol != CanonicalProtocolResponse {
        t.Errorf("expected protocol %q, got %q", CanonicalProtocolResponse, canonical.Protocol)
    }
    if canonical.SessionID != "sess-1" {
        t.Errorf("expected session %q, got %q", "sess-1", canonical.SessionID)
    }
    if len(canonical.Turns) == 0 {
        t.Fatal("expected at least one turn")
    }
}

func TestCanonicalizeOpenAIResponseRequest_ArrayInput(t *testing.T) {
    req := OpenAIResponseRequest{
        Model: "test-model",
        Input: json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`),
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(canonical.Turns) != 1 {
        t.Fatalf("expected 1 turn, got %d", len(canonical.Turns))
    }
}

func TestCanonicalizeOpenAIResponseRequest_WithInstructions(t *testing.T) {
    req := OpenAIResponseRequest{
        Model:        "test-model",
        Instructions: "You are helpful.",
        Input:        json.RawMessage(`"hello"`),
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(canonical.Turns) < 2 {
        t.Fatalf("expected at least 2 turns (instructions + input), got %d", len(canonical.Turns))
    }
    if canonical.Turns[0].Role != "system" {
        t.Errorf("expected first turn role 'system', got %q", canonical.Turns[0].Role)
    }
}

func TestCanonicalizeOpenAIResponseRequest_PreviousResponseID(t *testing.T) {
    req := OpenAIResponseRequest{
        Model:              "test-model",
        PreviousResponseID: "resp_123",
        Input:              json.RawMessage(`"hello"`),
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if canonical.SessionID != "resp_123" {
        t.Errorf("expected sessionID 'resp_123', got %q", canonical.SessionID)
    }
}

func TestCanonicalizeOpenAIResponseRequest_BuiltinTools(t *testing.T) {
    req := OpenAIResponseRequest{
        Model: "test-model",
        Input: json.RawMessage(`"hello"`),
        Tools: []OpenAIResponseTool{
            {Type: "function", Name: "get_weather", Description: "get weather"},
            {Type: "web_search_preview"},
            {Type: "code_interpreter"},
        },
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(canonical.Tools) != 1 {
        t.Fatalf("expected 1 function tool, got %d", len(canonical.Tools))
    }
    builtin, _ := canonical.Metadata["openai_builtin_tools"].([]string)
    if len(builtin) != 2 {
        t.Fatalf("expected 2 builtin tools, got %d", len(builtin))
    }
}

func TestCanonicalizeOpenAIResponseRequest_MessageWithImage(t *testing.T) {
    req := OpenAIResponseRequest{
        Model: "test-model",
        Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"describe this"},{"type":"input_image","image_url":{"url":"data:image/png;base64,AAAA"}}]}]`),
    }
    canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(canonical.Turns) != 1 {
        t.Fatalf("expected 1 turn, got %d", len(canonical.Turns))
    }
    turn := canonical.Turns[0]
    if len(turn.Blocks) != 2 {
        t.Fatalf("expected 2 blocks (text + image), got %d", len(turn.Blocks))
    }
}

func TestCanonicalizeOpenAIResponseRequest_EmptyModel(t *testing.T) {
    _, err := CanonicalizeOpenAIResponseRequest(OpenAIResponseRequest{Input: json.RawMessage(`"hello"`)}, "")
    if err == nil {
        t.Fatal("expected error for empty model")
    }
}
```

**Step 3: 运行测试**

Run: `cd d:/Repositories/MyRepository/FuckLingma && go test ./internal/proxy/ -run TestCanonicalizeOpenAIResponseRequest -v`
Expected: 所有测试通过

**Step 4: Commit**

```bash
git add internal/proxy/response_api_test.go
git commit -m "test: add canonicalization tests for Response API"
```

---

### Task 10: 完整构建与测试验证

**Step 1: 后端完整测试**

Run:
```bash
cd d:/Repositories/MyRepository/FuckLingma && go build ./... && go vet ./... && go test ./...
```
Expected: 无构建/测试错误

**Step 2: 前端构建**

Run:
```bash
cd d:/Repositories/MyRepository/FuckLingma/frontend && npm run build
```
Expected: 构建成功，无 regression

**Step 3: 最终提交**

```bash
git add .
git commit -m "chore: final verification after Response API implementation"
```

---

## 文件变更汇总

| # | 文件 | 操作 |
|---|------|------|
| 1 | `internal/proxy/types/types.go` | 修改：新增 CanonicalProtocol 常量 |
| 2 | `internal/proxy/types/response.go` | 新建：所有 Response API 类型 |
| 3 | `internal/proxy/message_ir.go` | 修改：新增 CanonicalizeOpenAIResponseRequest |
| 4 | `internal/proxy/response_api_test.go` | 新建：canonicalization 测试 |
| 5 | `internal/api/response/writers.go` | 修改：新增 WriteResponseError / WriteResponseSSE |
| 6 | `internal/api/handler/response.go` | 新建：处理器（含非流式 + 流式 + AccountRouting） |
| 7 | `internal/api/router/router.go` | 修改：注册 /v1/responses 路由 |

## 验证清单

- [x] `go build ./...` 通过所有 Tasks
- [x] `go vet ./...` 无错误
- [x] `go test ./...` 包括所有新测试
- [x] `npm run build` frontend 无 regression
- [ ] 手动 curl 测试（开发环境）
  ```bash
  # 非流式
  curl -X POST http://127.0.0.1:8080/v1/responses \
    -H "Content-Type: application/json" \
    -d '{"model":"test","input":"hello"}'
  
  # 流式
  curl -X POST http://127.0.0.1:8080/v1/responses \
    -H "Content-Type: application/json" \
    -d '{"model":"test","input":"hello","stream":true}'
  ```
