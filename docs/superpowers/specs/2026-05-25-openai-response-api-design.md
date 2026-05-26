# OpenAI Response API 支持 — 设计文档

**日期**: 2026-05-25  
**状态**: 设计完成  

## 概述

为 `lingma2api` 新增 OpenAI Response API (`/v1/responses`) 支持。该 API 是 OpenAI 推出的新一代接口，请求/响应格式与 Chat Completions API 有本质差异。本设计采用**独立处理器 + 复用 Canonical IR** 方案，最大程度复用现有管道（policy、session、vision、transport、logs），仅在入口解析和出口格式化处新增逻辑。

## 背景与动机

- 项目已支持 `POST /v1/chat/completions`（OpenAI Chat Completions）和 `POST /v1/messages`（Anthropic Messages）
- `/v1/responses` 是 OpenAI 的新一代 API，用于替代 Chat Completions
- 需要支持该接口以覆盖更广的客户端兼容性

## 架构决策

### 方案选择：方案 A — 独立处理器 + 复用 Canonical IR

对比方案 B（翻译为 Chat Completions）和方案 C（扩展 CanonicalRequest），选择方案 A 的理由：

- CanonicalRequest 作为归一化中间层已被 Chat Completions 和 Anthropic 两条路径验证
- 新增 Protocol 常量和处理器即可接入，改动集中在入口和出口
- Policy、Session、Vision、Transport、Logs 全部复用，无需变更
- 后续 Response API 新特性改动隔离

### 管道示意

```
Client (OpenAI Response format)
    |
    v
POST /v1/responses
    |
    v
解析 OpenAIResponseRequest
    |
    v
CanonicalizeOpenAIResponseRequest() → CanonicalRequest (Protocol: "openai_response")
    |
    +-- (复用) ValidateVisionLimits
    +-- (复用) EvaluateVisionGate
    +-- (复用) EvaluateCanonicalRequest (policy)
    +-- (复用) BuildCanonicalRequest (session merge)
    +-- (复用) ProjectCanonicalToOpenAIRequest → OpenAIChatRequest
    +-- (复用) ResolveChatModel
    +-- (复用) 图片上传 / BuildCanonical / StreamChat
    |
    v
(新) 响应格式化:
    - nonStreamResponseResponse()
    - streamResponseResponse()
    |
    v
OpenAIResponse (JSON) 或 SSE 流式事件
```

## 数据类型

### 新增到 `internal/proxy/types/types.go`

#### 请求

```go
type OpenAIResponseRequest struct {
    Model              string                `json:"model"`
    Input              json.RawMessage       `json:"input"`                // string | []ResponseInputItem
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
    Type    string          `json:"type"`    // "message", "function_call", "function_call_output", "reasoning"
    Role    string          `json:"role,omitempty"`
    Content json.RawMessage `json:"content,omitempty"`
    Name    string          `json:"name,omitempty"`
    Arguments string        `json:"arguments,omitempty"`
    CallID  string          `json:"call_id,omitempty"`
    Output  string          `json:"output,omitempty"`
    Summary []ResponseInputContentPart `json:"summary,omitempty"`
}

type ResponseInputContentPart struct {
    Type     string         `json:"type"`      // "input_text", "input_image", "input_file"
    Text     string         `json:"text,omitempty"`
    ImageURL *ImageURLPart  `json:"image_url,omitempty"`
}

type ImageURLPart struct {
    URL string `json:"url"`
}

type OpenAIResponseTool struct {
    Type        string `json:"type"`        // "function", "web_search_preview", "file_search", "code_interpreter"
    Name        string `json:"name,omitempty"`
    Description string `json:"description,omitempty"`
    Parameters  any    `json:"parameters,omitempty"`
}
```

#### 响应

```go
type OpenAIResponse struct {
    ID                 string                   `json:"id"`
    Object             string                   `json:"object"` // "response"
    CreatedAt          int64                    `json:"created_at"`
    Status             string                   `json:"status"`
    StatusDetails      *ResponseStatusDetails   `json:"status_details,omitempty"`
    Model              string                   `json:"model"`
    Output             []ResponseOutputItem     `json:"output"`
    Usage              *OpenAIResponseUsage     `json:"usage,omitempty"`
    Instructions       string                   `json:"instructions,omitempty"`
    MaxOutputTokens    int                      `json:"max_output_tokens"`
    Temperature        float64                  `json:"temperature"`
    TopP               float64                  `json:"top_p"`
    ParallelToolCalls  bool                     `json:"parallel_tool_calls"`
    PreviousResponseID string                   `json:"previous_response_id,omitempty"`
}

type ResponseStatusDetails struct {
    Type   string `json:"type"`
    Reason string `json:"reason"`
}

type ResponseOutputItem struct {
    ID        string                   `json:"id"`
    Type      string                   `json:"type"`    // "message", "function_call", "reasoning"
    Status    string                   `json:"status"`  // "completed", "in_progress", "incomplete"
    Role      string                   `json:"role,omitempty"`
    Content   []ResponseOutputContent  `json:"content,omitempty"`
    Name      string                   `json:"name,omitempty"`
    Arguments string                   `json:"arguments,omitempty"`
    CallID    string                   `json:"call_id,omitempty"`
    Summary   []ResponseOutputContent  `json:"summary,omitempty"`
}

type ResponseOutputContent struct {
    Type        string                `json:"type"` // "output_text", "refusal"
    Text        string                `json:"text"`
    Annotations []ResponseAnnotation  `json:"annotations,omitempty"`
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
```

#### 流式事件

```go
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

### CanonicalRequest 扩展

```go
const CanonicalProtocolResponse CanonicalProtocol = "openai_response"
```

新增到 `internal/proxy/types/types.go` 的常量区块。

## Canonicalization 规则

### Input → CanonicalTurn 映射

| Input Item `type` | CanonicalTurn `role` | ContentBlock 类型 |
|-------------------|---------------------|--------------------|
| `message` (user) | `user` | text / image |
| `message` (assistant) | `assistant` | text / tool_call |
| `message` (system/developer) | `system` | text |
| `function_call` | `assistant` | tool_call |
| `function_call_output` | `tool` | tool_result |
| `reasoning` | `assistant` | reasoning |

当 `input` 为字符串时，包装为单个 user message。

### Instructions → System Turn

`instructions` 字段作为第一个 `CanonicalTurn`（role: `system`，block: text）注入到 Turns 开头，与 Anthropic 的 system prompt 处理方式类似。

### PreviousResponseID → SessionID

直接映射为 `CanonicalRequest.SessionID`。现有的 session 合并逻辑会自动查找对应历史并追加本次 Turns。

### Tools 处理

- `type: "function"` → 转为 `CanonicalToolDefinition`（与 OpenAIChatRequest 一致）
- `type: "web_search_preview"` / `"file_search"` / `"code_interpreter"` → 记录到 `Metadata["openai_builtin_tools"]`，在响应中标记为 `status: "incomplete"`，原因是当前后端不可用

## 路由与处理器

### 路由注册

在 `internal/api/router/router.go` 新增：

```go
mux.HandleFunc("/v1/responses", s.HandleResponses)
```

### 处理器（新文件 `internal/api/handler/response.go`）

函数签名：`func (s *Server) HandleResponses(writer http.ResponseWriter, request *http.Request)`

管道流程：

1. 检查 Method = POST
2. `json.Decode` → `OpenAIResponseRequest`
3. 验证必填字段：model、input
4. `sessionID = service.RequestSessionID(request, req.PreviousResponseID)`
5. `proxy.CanonicalizeOpenAIResponseRequest(req, sessionID)` → `CanonicalRequest`
6. `service.AttachCanonicalRequestMetadata`
7. `proxy.ValidateVisionLimits` / `service.EvaluateVisionGate`
8. `service.EvaluateCanonicalRequest` (policy)
9. `s.Deps.Sessions.BuildCanonicalRequest` (session merge)
10. `proxy.ProjectCanonicalToOpenAIRequest` → 后端仍是 Lingma，走 OpenAIChatRequest 格式
11. `s.Deps.Models.ResolveChatModel`
12. 图片上传 / BuildCanonical / StreamChat（复用）
13. 分叉：
    - 非流式 → `nonStreamResponse`
    - 流式 → `streamResponse`
14. Session 保存：`s.Deps.Sessions.SaveCanonicalResponse`
15. 执行日志：`PersistCanonicalExecutionRecord(..., "/v1/responses", ..., CanonicalProtocolResponse, ...)`
16. Token 统计：`s.TokenStats.AddTokens`

### AccountRouting 支持

`HandleResponses` 同样支持多账户路由：

```go
if s.accountRoutingEnabled() {
    s.handleAccountRoutedResponses(...)
}
```

与 Chat Completions 的 account routing 逻辑一致，通过 `AdapterRegistry` 发送。

## 响应格式化

### 非流式

构建 `OpenAIResponse`：

```json
{
    "id": "resp_<requestID>",
    "object": "response",
    "status": "completed",
    "model": "<model>",
    "output": [
        {
            "id": "<prefix>_reasoning_0",
            "type": "reasoning",
            "status": "completed",
            "summary": [{"type": "output_text", "text": "<reasoning>"}]
        },
        {
            "id": "<prefix>_msg_0",
            "type": "message",
            "status": "completed",
            "role": "assistant",
            "content": [{"type": "output_text", "text": "<content>"}]
        },
        {
            "id": "<prefix>_fc_0",
            "type": "function_call",
            "status": "completed",
            "name": "<name>",
            "arguments": "<args>"
        }
    ],
    "usage": { "input_tokens": N, "output_tokens": N, "total_tokens": N }
}
```

### 流式响应事件

| 顺序 | 事件类型 | 触发条件 |
|------|---------|---------|
| 1 | `response.created` | 流式开始 |
| 2 | `response.in_progress` | 紧随 created |
| 3 | `response.output_item.added` | 检测到 output 开始时 |
| 4 | `response.content_part.added` | 每个 content part 开始时 |
| 5 | `response.output_text.delta` | 每段文本增量 |
| 6 | `response.output_text.done` | 文本输出完成 |
| 7 | `response.content_part.done` | content part 完成 |
| 8 | `response.reasoning_summary_part.added` | reasoning 开始时 |
| 9 | `response.reasoning_summary_part.done` | reasoning 结束时 |
| 10 | `response.function_call_arguments.delta` | 工具调用参数增量 |
| 11 | `response.function_call_arguments.done` | 工具调用参数完成 |
| 12 | `response.output_item.done` | output item 完成 |
| 13 | `response.completed` | 流式结束 |

事件格式：

```
event: response.created
data: {"type":"response.created","response":{"id":"resp_...","object":"response","status":"in_progress",...}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"...","output_index":0,"content_index":0,"delta":"Hello"}
```

## 错误处理

### 请求验证错误

返回标准 OpenAI Response 错误格式（非流式）：

```json
{
    "id": "",
    "object": "response",
    "status": "failed",
    "status_details": {"type": "invalid_request_error", "reason": "<message>"}
}
```

HTTP 状态码根据错误类型：
- 400：请求格式/验证错误
- 502：上游错误

### 流式错误

发送 `event: error`，data 为错误 JSON。

## 改动文件清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/proxy/types/types.go` | 修改 | 新增 Response API 类型定义 + CanonicalProtocol 常量 |
| `internal/proxy/message_ir.go` | 修改 | 新增 `CanonicalizeOpenAIResponseRequest` |
| `internal/api/handler/response.go` | 新建 | Response API 处理器 |
| `internal/api/router/router.go` | 修改 | 注册 `/v1/responses` 路由 |

## 测试范围

### 单元测试
- `CanonicalizeOpenAIResponseRequest` — 各种 input type 的转化正确性
- 字符串 input / 数组 input / instructions / previous_response_id / tools 映射
- 内置工具标记到 metadata

### 集成测试
- `HandleResponses` 非流式请求 → 正确响应格式
- `HandleResponses` 流式请求 → 正确 SSE 事件序列
- previous_response_id → session 正确追加历史
- 内置工具请求 → 返回 status: "incomplete"
- AccountRouting 路径正确

### 前端确认
- `npm run build` 不引入 regressions

## 不实现的功能

以下功能在本阶段不实现：

- `text.format`（格式化输出类型，如 JSON Schema 约束）
- `store` 字段的持久化（当前忽略此字段）
- `priority` / `top_logprobs` / `logit_bias` 等 Response API 专有参数
- 真正的内置工具执行（web_search 等仅标记不可用）
