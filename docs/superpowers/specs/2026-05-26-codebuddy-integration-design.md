# CodeBuddy Provider Integration Design

## Context

FuckLingma is a Go proxy that translates OpenAI-format API requests to Lingma's internal API. Currently it supports two providers: Lingma China and Lingma International. The user wants to add CodeBuddy (codebuddy.ai) as a third provider, inspired by the Python project CodeBuddy2API which implements a FastAPI reverse proxy that impersonates the CodeBuddy CLI.

**Design principle:** Providers are isolated. CodeBuddy gets its own endpoint prefix (`/codebuddy/v1/*`) independent of the existing `/lingma/v1/*` paths.
CodeBuddy accounts are selected only for `/codebuddy/v1/*` requests (separate from the Lingma region routing_mode system).

## Scope

### In scope
- New `AccountRegionCodeBuddy` region type
- Multi-account support: one `CODEBUDDY_API_KEY` = one account, stored in `credentials.json`
- `/codebuddy/v1/chat/completions` endpoint (stream + non-stream)
- `/codebuddy/v1/models` endpoint
- CLI header impersonation (X-IDE-Type, x-stainless-*, etc.)
- Keyword replacement for system messages (Claude → CodeBuddy, Anthropic → Tencent, etc.)
- Frontend: tab-based account page redesign with CodeBuddy tab
- Redis-backed stats integration (TokenStats, RequestStats)
- Route file reorganization: split `router.go` into `internal/api/routes/`

### Out of scope
- Session/policy/CanonicalRequest pipeline for CodeBuddy — goes through simpler direct path
- CodeBuddy OAuth token rotation (API Key only for now)
- CodeBuddy response API (`/v1/responses`)

## Architecture

### Config

```yaml
codebuddy:
  base_url: "https://www.codebuddy.ai"
  models:
    - claude-sonnet-4-6
    - claude-opus-4-7
```

New `CodeBuddyConfig` struct in `config.go`, API Key managed via `credentials.json` not config file.

### Account model extension

Extend `AccountRegion` in `types/types.go`:

```go
const (
    AccountRegionChina         AccountRegion = "china"
    AccountRegionInternational AccountRegion = "international"
    AccountRegionCodeBuddy     AccountRegion = "codebuddy"
)
```

Reuse existing `AccountSnapshot` fields for CodeBuddy accounts:
- `Region` = `"codebuddy"`
- `AccessToken` = API Key
- `UserID` / `Label` = user-defined identifier
- `ID` = hash-derived from `"codebuddy" + api_key` (via sha256, same pattern as `generatedAccountID`)

`validateAccountSnapshot` must be updated to accept `AccountRegionCodeBuddy`:
- Must have `AccessToken` (API Key)
- Does NOT require `CosyKey`, `EncryptUserInfo`, `UserID`, or `MachineID`

Example `credentials.json` entry:

```json
{
  "id": "cb-k9x3",
  "label": "My CodeBuddy",
  "region": "codebuddy",
  "enabled": true,
  "auth": { "access_token": "sk-xxxx" }
}
```

### File structure

```
internal/api/routes/
├── routes.go              # Main entry: New() + middleware + frontend serving
├── lingma_routes.go       # /lingma/v1/* registration
├── codebuddy_routes.go    # /codebuddy/v1/* registration
├── admin_routes.go        # /admin/* registration

internal/proxy/
├── codebuddy_client.go    # NEW: HTTP client + header impersonation + keyword replacement

internal/api/handler/
├── codebuddy.go           # NEW: HandleCodeBuddyChat, HandleCodeBuddyModels
```

### CodeBuddy Client (`internal/proxy/codebuddy_client.go`)

```go
type CodeBuddyClient struct {
    baseURL    string
    httpClient *http.Client
    keywords   map[string]string
}

func NewCodeBuddyClient(baseURL string) *CodeBuddyClient
func (c *CodeBuddyClient) SendChat(ctx context.Context, apiKey string, req CodeBuddyChatRequest) (io.ReadCloser, error)
func (c *CodeBuddyClient) buildHeaders(apiKey string) http.Header
func (c *CodeBuddyClient) applyKeywordReplacement(messages []Message) []Message
```

Key behaviors:
- TLS verification disabled by default (matching CodeBuddy2API)
- Timeout: connect 30s, read 300s
- CLI header impersonation: X-IDE-Type: CLI, x-stainless-*, User-Agent: CLI/1.0.7
- Forces `stream: true` upstream regardless of client preference
- Keyword replacement table: `{"Claude Code": "CodeBuddy Code", "Anthropic's official CLI for Claude": "Tencent's official CLI for CodeBuddy", "Claude": "CodeBuddy", "Anthropic": "Tencent", "https://github.com/anthropics/claude-code/issues": "https://cnb.cool/codebuddy/codebuddy-code/-/issues"}`

### Handler (`internal/api/handler/codebuddy.go`)

**HandleCodeBuddyChat:**
1. Parse OpenAI-format request
2. Select account (round-robin among enabled CodeBuddy accounts)
3. Apply keyword replacement to system messages
4. Call `client.SendChat()` → upstream SSE stream
5. If client `stream: true` → SSE relay with `tooluse_xxx` → `call_xxx` ID conversion + delta compaction
6. If client `stream: false` → aggregate all SSE chunks → build single JSON response
7. Record to Redis RequestStats/TokenStats

**HandleCodeBuddyModels:** Return model list from config.

### SSE conversion details (from CodeBuddy2API)

- `tooluse_xxx` → `call_xxx`
- Tool call index: assigned incrementally per unique tool call ID
- Delta compaction: remove empty `""`, `[]`, `null` values
- `finish_reason: ""` → `null` for mid-stream chunks
- Non-stream aggregation: merge tool calls by ID in a map, validate/fix concatenated JSON

### Account routing

CodeBuddy accounts are stored in the same `credentials.json` file as Lingma accounts, loaded by the existing `AccountStore`. The CodeBuddy handler uses a simple internal round-robin counter (not the existing Lingma `Balancer` which hardcodes China/International region checks).

```go
func (s *Server) selectCodeBuddyAccount(ctx context.Context) (proxy.AccountSnapshot, string, error) {
    accounts, _ := s.Deps.Accounts.Accounts(ctx)
    // filter to Region == codebuddy && Enabled
    // simple round-robin via atomic increment on Server field
    // return account + account.AccessToken as apiKey
}
```

### Routes (`internal/api/routes/codebuddy_routes.go`)

```go
func registerCodeBuddyRoutes(mux *http.ServeMux, s *handler.Server) {
    mux.HandleFunc("/codebuddy/v1/chat/completions", s.HandleCodeBuddyChat)
    mux.HandleFunc("/codebuddy/v1/models", s.HandleCodeBuddyModels)
}
```

### Frontend redesign

Account.tsx → Tab-based layout:

```
[国内版] [国际版] [CodeBuddy]
```

**CodeBuddy tab content:**
- API Key input + "添加账号" button
- Account list: Label, API Key preview (sk-xxx...xxxx), status, test/delete buttons
- Connection test button per account

**Admin API for CodeBuddy accounts:**

Reuse the existing `/admin/account/*` endpoints which operate via `AccountStore.UpsertAccount()` and the generic `/admin/account/test` handler. No new admin endpoints needed — the region field in the request body distinguishes CodeBuddy accounts from Lingma accounts.

- `POST /admin/account` with `{"region": "codebuddy", "auth": {"access_token": "sk-xxx"}, "label": "My CB"}` → upsert
- `GET /admin/account` → returns all accounts (Lingma + CodeBuddy) with region info
- `POST /admin/account/test` with `{"account_id": "cb-xxx"}` → test connection (adapter-registered test path)
- Existing delete endpoint at `/admin/account` via DELETE body

**Account.tsx changes:**
- Add active Tab state (default based on first account's region or "china")
- Filter displayed accounts by active Tab's region
- CodeBuddy tab: inline form for API Key + label → POST /admin/account
- Each tab independently renders its add/delete/test UI

**Lingma tabs:** keep existing logic unchanged.

### New/modified files summary

| File | Action |
|------|--------|
| `internal/proxy/types/types.go` | Modify: add `AccountRegionCodeBuddy` |
| `internal/proxy/codebuddy_client.go` | New |
| `internal/api/handler/codebuddy.go` | New |
| `internal/config/config.go` | Modify: add `CodeBuddyConfig` |
| `internal/api/routes/routes.go` | New: main entry |
| `internal/api/routes/lingma_routes.go` | New: extracted from router.go |
| `internal/api/routes/codebuddy_routes.go` | New |
| `internal/api/routes/admin_routes.go` | New: extracted from router.go |
| `internal/api/router/router.go` | Delete (split into routes/) |
| `main.go` | Modify: init CodeBuddyClient, pass to handler |
| `frontend/src/pages/Account.tsx` | Modify: tab redesign |
| `frontend/src/api/client.ts` | Modify: CodeBuddy API calls |
