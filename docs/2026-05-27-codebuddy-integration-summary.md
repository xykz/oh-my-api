# CodeBuddy Provider Integration Summary

> **Completed:** 2026-05-27
> **Plan:** `docs/superpowers/plans/2026-05-26-codebuddy-integration.md`
> **Design:** `docs/superpowers/specs/2026-05-26-codebuddy-integration-design.md`

## Overview

This document summarizes the implementation of CodeBuddy as a third provider alongside Lingma China/International in the FuckLingma proxy.

## Architecture

CodeBuddy gets its own isolated endpoint prefix (`/codebuddy/v1/*`) separate from `/lingma/v1/*`. A new `CodeBuddyClient` handles HTTP forwarding with CLI header impersonation. Accounts use `region: "codebuddy"` in the existing `credentials.json`.

## Implementation Tasks (All Complete)

### Task 1: Add AccountRegionCodeBuddy to types
**Files Modified:**
- `internal/proxy/types/types.go` — Added `AccountRegionCodeBuddy = "codebuddy"` constant
- `internal/proxy/types_aliases.go` — Added alias export
- `internal/proxy/accounts.go` — Added validation case requiring `AccessToken`

### Task 2: Add CodeBuddyConfig
**Files Modified:**
- `internal/config/config.go` — Added `CodeBuddyConfig` struct with `BaseURL` (default: `https://www.codebuddy.ai`) and `Models []string`, plus YAML parsing support

### Task 3: Create CodeBuddy HTTP client
**Files Created:**
- `internal/proxy/codebuddy_client.go` — HTTP client with:
  - `NewCodeBuddyClient(baseURL string)` constructor
  - `SendChat(ctx, apiKey, req) (io.ReadCloser, error)` — forces `Stream: true` upstream, applies keyword replacement
  - `buildHeaders(apiKey)` — 16 headers for CLI impersonation (X-IDE-Type: CLI, User-Agent: CLI/1.0.7, X-Product: SaaS, etc.)
  - `applyKeywordReplacement(messages)` — 5 keyword rules applied to system messages (Claude→CodeBuddy, Anthropic→Tencent, etc.)

### Task 4: Create CodeBuddy SSE parser
**Files Created:**
- `internal/proxy/codebuddy_sse.go` — Standard SSE parsing (unlike Lingma's nested format):
  - `CodeBuddySSEChunk` struct with exported fields
  - `CodeBuddySSEDelta` and `CodeBuddySSEToolCall` types
  - `ScanCodeBuddySSE(reader, onChunk, onDone) error` — bufio.Scanner with 1MB buffer

### Task 5: Create CodeBuddy handler
**Files Created:**
- `internal/api/handler/codebuddy.go` — Handlers:
  - `HandleCodeBuddyChat(w, r)` — POST /codebuddy/v1/chat/completions
  - `HandleCodeBuddyModels(w, r)` — GET /codebuddy/v1/models
  - `selectCodeBuddyAccount(ctx)` — filters enabled codebuddy accounts, round-robin with atomic counter
  - `codebuddyNonStreamResponse(w, ctx, stream, model)` — reads SSE, builds JSON response with usage
  - `codebuddyStreamResponse(w, r, stream, model)` — streams SSE events with delta conversion
  - `convertCodeBuddyDelta(delta, toolCallIndexMap)` — `tooluse_xxx` → `call_xxx` ID conversion

**Files Modified:**
- `internal/api/handler/server.go` — Added `CodeBuddyClient *proxy.CodeBuddyClient` and `CodeBuddyRRIndex uint64` fields
- `internal/api/model/types.go` — Added `CodeBuddyConfig config.CodeBuddyConfig` to Dependencies

### Task 6: Add POST support to /admin/account for CodeBuddy
**Files Modified:**
- `internal/api/handler/admin_extended.go`:
  - Refactored `HandleAdminAccount` to support GET and POST
  - Added `handleAdminAccountPost` for CodeBuddy account creation
  - Validates `region=codebuddy` and `access_token` requirement
- `internal/api/model/types.go`:
  - Added `UpsertAccount(context.Context, proxy.StoredCredentialAccount) error` to AccountProvider interface

### Task 7: Reorganize routes into internal/api/routes/
**Files Created:**
- `internal/api/routes/routes.go` — Main entry point with `New(deps, store, bootstrap, codebuddyClient)` function
- `internal/api/routes/lingma_routes.go` — `/lingma/v1/*` routes
- `internal/api/routes/codebuddy_routes.go` — `/codebuddy/v1/*` routes
- `internal/api/routes/admin_routes.go` — `/admin/*` routes

**Files Deleted:**
- `internal/api/router/router.go` — Replaced by routes package

**Files Modified:**
- `main.go` — Updated import and calls to use new routes package

### Task 8: Wire CodeBuddyClient in main.go
**Files Modified:**
- `main.go`:
  - Creates `codebuddyClient := proxy.NewCodeBuddyClient(cfg.CodeBuddy.BaseURL)`
  - Passes `CodeBuddyConfig: cfg.CodeBuddy` to Dependencies
  - Passes `codebuddyClient` to `routes.New()`

### Task 9: Frontend - tab redesign of Account.tsx
**Files Modified:**
- `frontend/src/pages/Account.tsx`:
  - Added tab state with 3 tabs: 国内版, 国际版, CodeBuddy
  - Tab bar component with `setActiveTab()` switching
  - Filter accounts by active tab
  - Stat grid now includes CodeBuddy count
- `frontend/src/styles/global.css`:
  - Added `.account-tabs` and `.account-tab` styles with active state

### Task 10: Frontend - CodeBuddy tab content
**Files Modified:**
- `frontend/src/pages/Account.tsx`:
  - Added CodeBuddy API key input form with label field
  - Shows only when `activeTab === 'codebuddy'`
  - `handleAddCodeBuddy()` function calls `addAccount()` API
  - Credential badges updated to show Access Token for CodeBuddy accounts
- `frontend/src/api/client.ts`:
  - Added `addAccount(body)` POST function
- `frontend/src/types/index.ts`:
  - Added `'codebuddy'` to `AccountRegion` type
  - Added `codebuddy` to `AccountCounts` interface

## API Endpoints

### CodeBuddy Endpoints
- `POST /codebuddy/v1/chat/completions` — Chat completions (streaming and non-streaming)
- `GET /codebuddy/v1/models` — List configured CodeBuddy models

### Existing Lingma Endpoints (No Changes)
- `POST /lingma/v1/chat/completions`
- `POST /lingma/v1/messages`
- `POST /lingma/v1/responses`
- `GET /lingma/v1/models`

### Admin Endpoints (Updated)
- `GET /admin/account` — List all accounts (china, international, codebuddy)
- `POST /admin/account` — Create CodeBuddy account (requires `region: "codebuddy"` and `access_token`)

## Configuration

Add to `config.yaml`:

```yaml
codebuddy:
  base_url: "https://www.codebuddy.ai"
  models:
    - "claude-sonnet-4-20250514"
    - "claude-3-7-sonnet-20250219"
```

## Account Management

CodeBuddy accounts are stored in `credentials.json` alongside Lingma accounts with `region: "codebuddy"`:

```json
{
  "accounts": [
    {
      "id": "codebuddy-xxx",
      "label": "My CodeBuddy Account",
      "region": "codebuddy",
      "enabled": true,
      "auth": {
        "access_token": "sk-xxx"
      }
    }
  ]
}
```

## Key Features

1. **Provider Isolation** — CodeBuddy uses `/codebuddy/v1/*` prefix, separate from Lingma's `/lingma/v1/*`
2. **CLI Header Impersonation** — Requests impersonate CLI client with specific headers
3. **Keyword Replacement** — System messages automatically replace Claude/Anthropic references with CodeBuddy/Tencent
4. **Forced Streaming** — All upstream requests use `stream: true` even if client requests non-streaming
5. **Tool Call ID Conversion** — `tooluse_xxx` → `call_xxx` conversion for OpenAI compatibility
6. **Round-Robin Load Balancing** — Multiple CodeBuddy accounts are selected using atomic counter
7. **Redis Stats Integration** — Token and request stats are recorded at handler exit points

## Files Summary

| File | Action |
|------|--------|
| `internal/proxy/types/types.go` | Modified: add `AccountRegionCodeBuddy` |
| `internal/proxy/types_aliases.go` | Modified: add alias |
| `internal/proxy/accounts.go` | Modified: validate codebuddy accounts |
| `internal/config/config.go` | Modified: add `CodeBuddyConfig` |
| `internal/proxy/codebuddy_client.go` | **New** |
| `internal/proxy/codebuddy_sse.go` | **New** |
| `internal/api/handler/codebuddy.go` | **New** |
| `internal/api/handler/server.go` | Modified: add CodeBuddyClient fields |
| `internal/api/model/types.go` | Modified: add CodeBuddyConfig to Dependencies, add UpsertAccount |
| `internal/api/routes/routes.go` | **New**: main entry |
| `internal/api/routes/lingma_routes.go` | **New** |
| `internal/api/routes/codebuddy_routes.go` | **New** |
| `internal/api/routes/admin_routes.go` | **New** |
| `internal/api/router/router.go` | **Deleted** |
| `main.go` | Modified: init CodeBuddyClient, use routes package |
| `frontend/src/pages/Account.tsx` | Modified: tab redesign |
| `frontend/src/api/client.ts` | Modified: add account CRUD API |
| `frontend/src/types/index.ts` | Modified: add codebuddy region |
| `frontend/src/styles/global.css` | Modified: add tab styles |
| `internal/api/handler/admin_extended.go` | Modified: POST admin account support |

## Testing

1. Start the app with CodeBuddy configuration
2. Add a CodeBuddy account via the admin UI or API
3. Send requests to `/codebuddy/v1/chat/completions`
4. Verify streaming and non-streaming responses
5. Check that keyword replacement is applied in system messages
6. Verify tool call IDs are properly converted
7. Check admin dashboard for token and request stats
