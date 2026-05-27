# CodeBuddy Provider Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CodeBuddy (codebuddy.ai) as a third provider alongside Lingma China/International in the FuckLingma proxy.

**Architecture:** CodeBuddy gets its own endpoint prefix (`/codebuddy/v1/*`) isolated from `/lingma/v1/*`. A new `CodeBuddyClient` handles HTTP forwarding with CLI header impersonation. Accounts use `region: "codebuddy"` in the existing `credentials.json`. Routes are reorganized from a single `router.go` into `internal/api/routes/`.

**Tech Stack:** Go (net/http), existing proxy types, Redis stats, React+TypeScript frontend

---

### Task 1: Add AccountRegionCodeBuddy to types

**Files:**
- Modify: `internal/proxy/types/types.go:199-204`
- Modify: `internal/proxy/accounts.go:252-264`

- [ ] **Step 1: Add the new region constant**

In `internal/proxy/types/types.go`:

```go
const (
    AccountRegionChina         AccountRegion = "china"
    AccountRegionInternational AccountRegion = "international"
    AccountRegionCodeBuddy     AccountRegion = "codebuddy"
)
```

- [ ] **Step 2: Update validateAccountSnapshot**

In `internal/proxy/accounts.go`, add `AccountRegionCodeBuddy` case:

```go
case AccountRegionCodeBuddy:
    if account.AccessToken == "" {
        return fmt.Errorf("%w: account %q missing access_token", ErrCredentialsUnavailable, account.ID)
    }
    return nil
```

- [ ] **Step 3: Build**

Run: `go build ./...` - Expected: success

- [ ] **Step 4: Commit**

---

### Task 2: Add CodeBuddyConfig

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add struct and defaults**

```go
type CodeBuddyConfig struct {
    BaseURL string   `json:"base_url"`
    Models  []string `json:"models"`
}
```

Add `CodeBuddy CodeBuddyConfig` to `Config` struct. Default: `BaseURL: "https://www.codebuddy.ai"`.

- [ ] **Step 2: Add YAML assignment**

Add `case "codebuddy":` in `assignValue()`, plus `assignCodeBuddyValue()` function handling `base_url` and `models` (comma-separated).

- [ ] **Step 3: Build and commit**

---

### Task 3: Create CodeBuddy HTTP client

**Files:**
- Create: `internal/proxy/codebuddy_client.go`

- [ ] **Step 1: Create the file**

```go
package proxy

import (
    "bytes"
    "context"
    "crypto/tls"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/rizxfrog/oh-my-api/internal/proxy/types"
)

type CodeBuddyClient struct {
    baseURL    string
    httpClient *http.Client
    keywords   map[string]string
}

func NewCodeBuddyClient(baseURL string) *CodeBuddyClient {
    return &CodeBuddyClient{
        baseURL: strings.TrimRight(baseURL, "/"),
        httpClient: &http.Client{
            Transport: &http.Transport{
                TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
            },
            Timeout: 300 * time.Second,
        },
        keywords: map[string]string{
            "Claude Code": "CodeBuddy Code",
            "Anthropic's official CLI for Claude": "Tencent's official CLI for CodeBuddy",
            "Claude": "CodeBuddy",
            "Anthropic": "Tencent",
            "https://github.com/anthropics/claude-code/issues": "https://cnb.cool/codebuddy/codebuddy-code/-/issues",
        },
    }
}
```

- [ ] **Step 2: Add SendChat method**

```go
func (c *CodeBuddyClient) SendChat(ctx context.Context, apiKey string, req types.OpenAIChatRequest) (io.ReadCloser, error) {
    req.Stream = true
    req.Messages = c.applyKeywordReplacement(req.Messages)
    body, _ := json.Marshal(req)
    httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v2/chat/completions", bytes.NewReader(body))
    for k, v := range c.buildHeaders(apiKey) {
        httpReq.Header[k] = v
    }
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode != 200 {
        resp.Body.Close()
        return nil, &types.UpstreamHTTPError{StatusCode: resp.StatusCode, Body: "upstream error"}
    }
    return resp.Body, nil
}
```

- [ ] **Step 3: Add buildHeaders**

```go
func (c *CodeBuddyClient) buildHeaders(apiKey string) map[string][]string {
    return map[string][]string{
        "Accept": {"application/json"},
        "Content-Type": {"application/json"},
        "Authorization": {"Bearer " + apiKey},
        "X-API-Key": {apiKey},
        "X-IDE-Type": {"CLI"},
        "X-IDE-Name": {"CLI"},
        "X-IDE-Version": {"1.0.7"},
        "User-Agent": {"CLI/1.0.7 CodeBuddy/1.0.7"},
        "X-Agent-Intent": {"craft"},
        "X-Product": {"SaaS"},
        "x-stainless-arch": {"x64"},
        "x-stainless-lang": {"js"},
        "x-stainless-os": {"Windows"},
        "x-stainless-package-version": {"5.10.1"},
        "x-stainless-runtime": {"node"},
        "x-stainless-runtime-version": {"v22.13.1"},
    }
}
```

- [ ] **Step 4: Add applyKeywordReplacement**

```go
func (c *CodeBuddyClient) applyKeywordReplacement(messages []types.Message) []types.Message {
    result := make([]types.Message, len(messages))
    copy(result, messages)
    for i := range result {
        if result[i].Role == "system" {
            content := result[i].Content
            for old, new_ := range c.keywords {
                content = strings.ReplaceAll(content, old, new_)
            }
            result[i].Content = content
        }
    }
    return result
}
```

- [ ] **Step 5: Add CodeBuddyChatRequest type alias if needed**

In `types.go`, the existing `OpenAIChatRequest` can be reused since CodeBuddy accepts the same format.

- [ ] **Step 6: Build and commit**

---

### Task 4: Create CodeBuddy SSE parser

**Files:**
- Create: `internal/proxy/codebuddy_sse.go`

- [ ] **Step 1: Create CodeBuddy SSE parser**

CodeBuddy's SSE format is standard (not the nested outer/inner format Lingma uses). Create a lightweight parser:

```go
package proxy

import (
    "bufio"
    "encoding/json"
    "io"
    "strings"
)

type codebuddySSEChunk struct {
    Choices []struct {
        Delta struct {
            Content    string `json:"content"`
            ToolCalls  []struct {
                Index    int    `json:"index"`
                ID       string `json:"id"`
                Type     string `json:"type"`
                Function struct {
                    Name      string `json:"name"`
                    Arguments string `json:"arguments"`
                } `json:"function"`
            } `json:"tool_calls"`
        } `json:"delta"`
        FinishReason *string `json:"finish_reason"`
    } `json:"choices"`
    Usage *struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
        TotalTokens      int `json:"total_tokens"`
    } `json:"usage"`
}

func parseCodeBuddySSELine(line string) (*codebuddySSEChunk, bool) {
    trimmed := strings.TrimSpace(line)
    if trimmed == "" || !strings.HasPrefix(trimmed, "data:") {
        return nil, false
    }
    payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
    if payload == "[DONE]" {
        return nil, true
    }
    var chunk codebuddySSEChunk
    if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
        return nil, false
    }
    return &chunk, false
}

func scanCodeBuddySSE(reader io.Reader, onChunk func(*codebuddySSEChunk) error, onDone func() error) error {
    scanner := bufio.NewScanner(reader)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
    for scanner.Scan() {
        chunk, done := parseCodeBuddySSELine(scanner.Text())
        if done {
            if onDone != nil {
                return onDone()
            }
            return nil
        }
        if chunk != nil && onChunk != nil {
            if err := onChunk(chunk); err != nil {
                return err
            }
        }
    }
    return scanner.Err()
}
```

- [ ] **Step 2: Build and commit**

---

### Task 5: Create CodeBuddy handler

**Files:**
- Create: `internal/api/handler/codebuddy.go`
- Modify: `internal/api/handler/server.go`

- [ ] **Step 1: Add CodeBuddyClient and round-robin counter to Server**

In `server.go`, add fields:

```go
type Server struct {
    // ... existing fields ...
    CodeBuddyClient   *proxy.CodeBuddyClient
    CodeBuddyRRIndex  uint64  // atomic round-robin counter
}
```

- [ ] **Step 2: Create codebuddy.go with non-stream handler**

```go
package handler

import (
    "encoding/json"
    "net/http"
    "strings"
    "sync/atomic"

    "github.com/rizxfrog/oh-my-api/internal/api/response"
    "github.com/rizxfrog/oh-my-api/internal/proxy"
)

func (s *Server) HandleCodeBuddyChat(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        response.WriteMethodNotAllowed(w, http.MethodPost)
        return
    }
    if s.CodeBuddyClient == nil {
        response.WriteOpenAIError(w, http.StatusServiceUnavailable, "codebuddy not configured")
        return
    }

    account, apiKey, err := s.selectCodeBuddyAccount(r.Context())
    if err != nil {
        response.WriteOpenAIError(w, http.StatusUnauthorized, "no codebuddy account: "+err.Error())
        return
    }

    body := http.MaxBytesReader(w, r.Body, 2<<20)
    defer body.Close()
    var chatReq proxy.OpenAIChatRequest
    if err := json.NewDecoder(body).Decode(&chatReq); err != nil {
        response.WriteOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
        return
    }

    stream, err := s.CodeBuddyClient.SendChat(r.Context(), apiKey, chatReq)
    if err != nil {
        response.WriteOpenAIError(w, http.StatusBadGateway, "upstream: "+err.Error())
        return
    }
    defer stream.Close()

    if chatReq.Stream {
        s.codebuddyStreamResponse(w, r, stream, chatReq.Model)
    } else {
        s.codebuddyNonStreamResponse(w, r.Context(), stream, chatReq.Model)
    }
}

func (s *Server) selectCodeBuddyAccount(ctx context.Context) (proxy.AccountSnapshot, string, error) {
    accounts, err := s.Deps.Accounts.Accounts(ctx)
    if err != nil {
        return proxy.AccountSnapshot{}, "", err
    }
    enabled := make([]proxy.AccountSnapshot, 0)
    for _, a := range accounts {
        if a.Region == proxy.AccountRegionCodeBuddy && a.Enabled {
            enabled = append(enabled, a)
        }
    }
    if len(enabled) == 0 {
        return proxy.AccountSnapshot{}, "", fmt.Errorf("no enabled codebuddy accounts")
    }
    idx := atomic.AddUint64(&s.CodeBuddyRRIndex, 1) % uint64(len(enabled))
    account := enabled[idx]
    return account, account.AccessToken, nil
}
```

- [ ] **Step 3: Add non-stream response handler**

```go
func (s *Server) codebuddyNonStreamResponse(w http.ResponseWriter, ctx context.Context, stream io.Reader, model string) {
    startTime := s.Deps.Now()
    var contentBuilder strings.Builder
    var rawLines []string
    var promptTokens, completionTokens int

    err := scanCodeBuddySSE(stream, func(chunk *codebuddySSEChunk) error {
        rawLine, _ := json.Marshal(chunk)
        rawLines = append(rawLines, string(rawLine))
        if chunk.Usage != nil {
            promptTokens = chunk.Usage.PromptTokens
            completionTokens = chunk.Usage.CompletionTokens
        }
        for _, choice := range chunk.Choices {
            contentBuilder.WriteString(choice.Delta.Content)
        }
        return nil
    }, nil)
    if err != nil {
        response.WriteOpenAIError(w, http.StatusBadGateway, "upstream: "+err.Error())
        return
    }

    content := contentBuilder.String()
    finishReason := "stop"
    totalTokens := promptTokens + completionTokens
    if totalTokens == 0 {
        totalTokens = len(content) / 2
        promptTokens = totalTokens / 2
        completionTokens = totalTokens - promptTokens
    }

    if s.TokenStats != nil {
        _ = s.TokenStats.AddTokens(ctx, totalTokens)
    }
    if s.RequestStats != nil {
        ttftMs := int(s.Deps.Now().Sub(startTime).Milliseconds())
        _ = s.RequestStats.RecordRequest(ctx, true, ttftMs)
    }

    response.WriteJSON(w, http.StatusOK, map[string]interface{}{
        "id":      "chatcmpl-" + proxy.NewHexID(),
        "object":  "chat.completion",
        "created": s.Deps.Now().Unix(),
        "model":   model,
        "choices": []map[string]interface{}{{
            "index": 0,
            "message": map[string]string{
                "role":    "assistant",
                "content": content,
            },
            "finish_reason": finishReason,
        }},
        "usage": map[string]int{
            "prompt_tokens":     promptTokens,
            "completion_tokens": completionTokens,
            "total_tokens":      totalTokens,
        },
    })
}
```

- [ ] **Step 4: Add stream response handler**

```go
func (s *Server) codebuddyStreamResponse(w http.ResponseWriter, r *http.Request, stream io.Reader, model string) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        response.WriteOpenAIError(w, http.StatusInternalServerError, "streaming unsupported")
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    startTime := s.Deps.Now()
    var firstTokenTime time.Time
    var totalContent strings.Builder
    var promptTokens, completionTokens int
    toolCallIndexMap := map[string]int{}

    responseID := "chatcmpl-" + proxy.NewHexID()

    err := scanCodeBuddySSE(stream, func(chunk *codebuddySSEChunk) error {
        if chunk.Usage != nil {
            promptTokens = chunk.Usage.PromptTokens
            completionTokens = chunk.Usage.CompletionTokens
            return nil
        }
        for _, choice := range chunk.Choices {
            delta := choice.Delta
            converted := convertCodeBuddyDelta(delta, toolCallIndexMap)
            if delta.Content != "" {
                if firstTokenTime.IsZero() {
                    firstTokenTime = s.Deps.Now()
                }
                totalContent.WriteString(delta.Content)
            }
            chunkResp := map[string]interface{}{
                "id":      responseID,
                "object":  "chat.completion.chunk",
                "created": s.Deps.Now().Unix(),
                "model":   model,
                "choices": []map[string]interface{}{{
                    "index": 0,
                    "delta": converted,
                }},
            }
            if choice.FinishReason != nil {
                chunkResp["choices"].([]map[string]interface{})[0]["finish_reason"] = *choice.FinishReason
            }
            data, _ := json.Marshal(chunkResp)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
        return nil
    }, func() error {
        fmt.Fprintf(w, "data: [DONE]\n\n")
        flusher.Flush()
        return nil
    })
    if err != nil {
        var errorBuf strings.Builder
        errorBuf.WriteString(`{"error":{"message":"`)
        errorBuf.WriteString(err.Error())
        errorBuf.WriteString(`"}}`)
        fmt.Fprintf(w, "data: %s\n\n", errorBuf.String())
        flusher.Flush()
        return
    }

    totalTokens := promptTokens + completionTokens
    if totalTokens == 0 {
        totalTokens = totalContent.Len() / 2
    }
    if s.TokenStats != nil {
        _ = s.TokenStats.AddTokens(r.Context(), totalTokens)
    }
    if s.RequestStats != nil && !firstTokenTime.IsZero() {
        ttftMs := int(firstTokenTime.Sub(startTime).Milliseconds())
        _ = s.RequestStats.RecordRequest(r.Context(), true, ttftMs)
    }
}
```

- [ ] **Step 5: Add delta conversion helper**

```go
func convertCodeBuddyDelta(delta codebuddySSEChunkChoiceDelta, toolCallIndexMap map[string]int) map[string]interface{} {
    result := map[string]interface{}{}
    if delta.Content != "" {
        result["content"] = delta.Content
    }
    if len(delta.ToolCalls) > 0 {
        toolCalls := make([]map[string]interface{}, 0, len(delta.ToolCalls))
        for _, tc := range delta.ToolCalls {
            convertedID := tc.ID
            if strings.HasPrefix(convertedID, "tooluse_") {
                convertedID = "call_" + convertedID[len("tooluse_"):]
            }
            if _, exists := toolCallIndexMap[convertedID]; !exists {
                toolCallIndexMap[convertedID] = len(toolCallIndexMap)
            }
            tcMap := map[string]interface{}{
                "index": toolCallIndexMap[convertedID],
                "id":    convertedID,
                "type":  tc.Type,
            }
            if tc.Function.Name != "" || tc.Function.Arguments != "" {
                tcMap["function"] = map[string]string{
                    "name":      tc.Function.Name,
                    "arguments": tc.Function.Arguments,
                }
            }
            toolCalls = append(toolCalls, tcMap)
        }
        result["tool_calls"] = toolCalls
    }
    return result
}
```

- [ ] **Step 6: Add HandleCodeBuddyModels**

```go
func (s *Server) HandleCodeBuddyModels(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        response.WriteMethodNotAllowed(w, http.MethodGet)
        return
    }
    models := s.Deps.CodeBuddyConfig.Models
    data := make([]map[string]interface{}, 0, len(models))
    for _, m := range models {
        data = append(data, map[string]interface{}{
            "id": m, "object": "model",
            "created": s.Deps.Now().Unix(), "owned_by": "codebuddy",
        })
    }
    response.WriteJSON(w, http.StatusOK, map[string]interface{}{
        "object": "list", "data": data,
    })
}
```

- [ ] **Step 7: Add CodeBuddyConfig to Dependencies**

In `internal/api/model/types.go`, add to `Dependencies`:

```go
CodeBuddyConfig config.CodeBuddyConfig
```

- [ ] **Step 8: Build and commit**

---

### Task 6: Add POST support to /admin/account for CodeBuddy

**Files:**
- Modify: `internal/api/handler/admin_extended.go:349-395`

- [ ] **Step 1: Update HandleAdminAccount to also accept POST**

Currently only handles GET. Add POST support for creating CodeBuddy accounts:

```go
func (s *Server) HandleAdminAccount(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        s.handleAdminAccountGet(w, r)
    case http.MethodPost:
        s.handleAdminAccountPost(w, r)
    default:
        response.WriteMethodNotAllowed(w, http.MethodGet, http.MethodPost)
    }
}
```

- [ ] **Step 2: Move existing GET logic to handleAdminAccountGet**

Extract the current `HandleAdminAccount` body into `handleAdminAccountGet`.

- [ ] **Step 3: Create handleAdminAccountPost**

```go
func (s *Server) handleAdminAccountPost(w http.ResponseWriter, r *http.Request) {
    if !s.isAdminAuthorized(r) {
        response.WriteOpenAIError(w, http.StatusUnauthorized, "unauthorized")
        return
    }
    if s.Deps.Accounts == nil {
        response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "account store not configured"})
        return
    }
    body := http.MaxBytesReader(w, r.Body, 1<<20)
    defer body.Close()
    var account proxy.StoredCredentialAccount
    if err := json.NewDecoder(body).Decode(&account); err != nil {
        response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
        return
    }
    if account.Region != proxy.AccountRegionCodeBuddy {
        response.WriteJSON(w, http.StatusBadRequest, map[string]string{
            "error": "POST /admin/account currently only supports region=codebuddy; use bootstrap for lingma accounts",
        })
        return
    }
    if account.Auth.AccessToken == "" {
        response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "access_token is required"})
        return
    }
    if err := s.Deps.Accounts.UpsertAccount(r.Context(), account); err != nil {
        response.WriteMappedError(w, err)
        return
    }
    response.WriteJSON(w, http.StatusOK, map[string]string{"message": "account added"})
}
```

- [ ] **Step 4: Add UpsertAccount to AccountProvider interface if missing**

Check `internal/api/model/types.go`. If `AccountProvider` doesn't have `UpsertAccount`, add it:

```go
type AccountProvider interface {
    Accounts(context.Context) ([]proxy.AccountSnapshot, error)
    Summaries(context.Context) ([]proxy.AccountSummary, error)
    UpsertAccount(context.Context, proxy.StoredCredentialAccount) error
}
```

- [ ] **Step 5: Build and commit**

---

### Task 7: Reorganize routes into internal/api/routes/

**Files:**
- Create: `internal/api/routes/routes.go`
- Create: `internal/api/routes/lingma_routes.go`
- Create: `internal/api/routes/codebuddy_routes.go`
- Create: `internal/api/routes/admin_routes.go`
- Delete: `internal/api/router/router.go`

- [ ] **Step 1: Create routes.go main entry**

Copy the `New()` function from `router.go` into `routes/routes.go`, removing route registrations and calling sub-register functions:

```go
package routes

import (
    "context"
    "embed"
    "io/fs"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/rizxfrog/oh-my-api/internal/api/handler"
    "github.com/rizxfrog/oh-my-api/internal/api/model"
    "github.com/rizxfrog/oh-my-api/internal/db"
    "github.com/rizxfrog/oh-my-api/internal/middleware"
)

func New(deps model.Dependencies, store *db.Store, bootstrap *handler.BootstrapManager) http.Handler {
    if deps.Now == nil {
        deps.Now = time.Now
    }

    s := &handler.Server{
        Deps:               deps,
        DB:                 store,
        StoreExecutionLogs: deps.StoreExecutionLogs,
        Bootstrap:          bootstrap,
        TokenStats:         deps.TokenStats,
        RequestStats:       deps.RequestStats,
    }

    mux := http.NewServeMux()
    registerLingmaRoutes(mux, s)
    registerCodeBuddyRoutes(mux, s)
    registerAdminRoutes(mux, s)

    if deps.FrontendFS != (embed.FS{}) {
        subFS, err := fs.Sub(deps.FrontendFS, "frontend-dist")
        if err == nil {
            fileServer := http.FileServerFS(subFS)
            mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                f, err := subFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
                if err == nil {
                    f.Close()
                    fileServer.ServeHTTP(w, r)
                    return
                }
                r.URL.Path = "/"
                fileServer.ServeHTTP(w, r)
            })
        }
    }

    hdlr := http.Handler(mux)
    if store != nil {
        settings, _ := store.GetSettings(context.Background())
        cfg := middleware.LoggingConfig{
            StorageMode:    settings["storage_mode"],
            TruncateLength: parseIntOr(settings["truncate_length"], 102400),
        }
        hdlr = middleware.Logging(store, cfg)(hdlr)
    }
    return hdlr
}

func parseIntOr(s string, def int) int {
    if v, err := strconv.Atoi(s); err == nil {
        return v
    }
    return def
}
```

- [ ] **Step 2: Create lingma_routes.go**

Extract all `/lingma/v1/*` route registrations from `router.go` into `registerLingmaRoutes(mux, s)`.

- [ ] **Step 3: Create codebuddy_routes.go**

```go
package routes

import (
    "net/http"
    "github.com/rizxfrog/oh-my-api/internal/api/handler"
)

func registerCodeBuddyRoutes(mux *http.ServeMux, s *handler.Server) {
    mux.HandleFunc("/codebuddy/v1/chat/completions", s.HandleCodeBuddyChat)
    mux.HandleFunc("/codebuddy/v1/models", s.HandleCodeBuddyModels)
}
```

- [ ] **Step 4: Create admin_routes.go**

Extract all `/admin/*` route registrations from `router.go` into `registerAdminRoutes(mux, s)`.

- [ ] **Step 5: Update main.go import**

Change `"github.com/rizxfrog/oh-my-api/internal/api/router"` to `"github.com/rizxfrog/oh-my-api/internal/api/routes"`.

- [ ] **Step 6: Delete router/router.go**

- [ ] **Step 7: Build and commit**

---

### Task 8: Wire CodeBuddyClient in main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Create CodeBuddyClient and add to Dependencies**

In `main()`, after the adapter registration and before the store/db setup:

```go
codebuddyClient := proxy.NewCodeBuddyClient(cfg.CodeBuddy.BaseURL)
```

Add `CodeBuddyConfig: cfg.CodeBuddy` to `model.Dependencies{}`.

After `httpHandler := routes.New(...)`:

```go
// Inject CodeBuddyClient into server (routes.New doesn't know about it)
// We'll handle this via a setter or by modifying New() signature
```

Better approach: modify `routes.New()` to accept and inject `*proxy.CodeBuddyClient`:

In `routes.go`, add parameter and set `s.CodeBuddyClient = codebuddyClient`.

- [ ] **Step 2: Build and commit**

---

### Task 9: Frontend - tab redesign of Account.tsx

**Files:**
- Modify: `frontend/src/pages/Account.tsx`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add Tab state and filter**

At top of Account component, add:

```tsx
type ProviderTab = 'china' | 'international' | 'codebuddy';
const [activeTab, setActiveTab] = useState<ProviderTab>('china');
const TABS: { key: ProviderTab; label: string; icon: React.ReactNode }[] = [
  { key: 'china', label: '国内版', icon: <Server size={16} /> },
  { key: 'international', label: '国际版', icon: <Globe2 size={16} /> },
  { key: 'codebuddy', label: 'CodeBuddy', icon: <Zap size={16} /> },
];
```

- [ ] **Step 2: Filter accounts by active tab**

```tsx
const filteredAccounts = accounts.filter(a => {
  if (activeTab === 'codebuddy') return a.region === 'codebuddy';
  if (activeTab === 'international') return a.region === 'international';
  return a.region !== 'international' && a.region !== 'codebuddy';
});
```

- [ ] **Step 3: Add CodeBuddy API Key input form**

Add state and form component:

```tsx
const [cbApiKey, setCbApiKey] = useState('');
const [cbLabel, setCbLabel] = useState('');

const handleAddCodeBuddy = async () => {
  await apiClient.post('/admin/account', {
    region: 'codebuddy',
    label: cbLabel || 'CodeBuddy',
    auth: { access_token: cbApiKey },
  });
  setCbApiKey('');
  setCbLabel('');
  await load();
};
```

- [ ] **Step 4: Render tab bar**

Replace the current page header with tab bar component:

```tsx
<div className="account-tabs">
  {TABS.map(tab => (
    <button key={tab.key} className={`account-tab ${activeTab === tab.key ? 'active' : ''}`}
      onClick={() => setActiveTab(tab.key)}>
      {tab.icon} {tab.label}
    </button>
  ))}
</div>
```

- [ ] **Step 5: Add CodeBuddy API calls to client.ts**

```ts
export async function addAccount(body: object) {
  const res = await fetch('/admin/account', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Admin-Token': getToken() },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
```

- [ ] **Step 6: Commit**

---

### Task 10: Frontend - CodeBuddy tab content

**Files:**
- Modify: `frontend/src/pages/Account.tsx`

- [ ] **Step 1: Render CodeBuddy add form when codebuddy tab active**

```tsx
{activeTab === 'codebuddy' && (
  <div className="card">
    <h4>添加 CodeBuddy 账号</h4>
    <div className="account-callback-box">
      <input className="input" placeholder="API Key (sk-...)" value={cbApiKey}
        onChange={e => setCbApiKey(e.target.value)} />
      <input className="input" placeholder="标签（可选）" value={cbLabel}
        onChange={e => setCbLabel(e.target.value)} />
      <button className="btn btn-primary" onClick={handleAddCodeBuddy}
        disabled={!cbApiKey.trim()}>
        添加账号
      </button>
    </div>
  </div>
)}
```

- [ ] **Step 2: Show CodeBuddy account list**

Adapt existing account table for CodeBuddy accounts — show label, API key preview, enabled status, test button.

- [ ] **Step 3: Commit**

---

### New/modified files summary

| File | Action |
|------|--------|
| `internal/proxy/types/types.go` | Modify: add `AccountRegionCodeBuddy` |
| `internal/proxy/accounts.go` | Modify: validate codebuddy accounts |
| `internal/config/config.go` | Modify: add `CodeBuddyConfig` |
| `internal/proxy/codebuddy_client.go` | New |
| `internal/proxy/codebuddy_sse.go` | New |
| `internal/api/handler/codebuddy.go` | New |
| `internal/api/handler/server.go` | Modify: add CodeBuddyClient fields |
| `internal/api/model/types.go` | Modify: add CodeBuddyConfig to Dependencies |
| `internal/api/routes/routes.go` | New: main entry |
| `internal/api/routes/lingma_routes.go` | New |
| `internal/api/routes/codebuddy_routes.go` | New |
| `internal/api/routes/admin_routes.go` | New |
| `internal/api/router/router.go` | Delete |
| `main.go` | Modify: init CodeBuddyClient |
| `frontend/src/pages/Account.tsx` | Modify: tab redesign |
| `frontend/src/api/client.ts` | Modify: add account CRUD API |
