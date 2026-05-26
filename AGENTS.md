# AGENTS.md

Repository instructions for agentic coding assistants working in this project.

## Project Overview

`lingma2api` is a Go service with an embedded Vite/React admin console.

- Backend entry point: `main.go`
- Backend packages: `internal/api`, `internal/auth`, `internal/config`, `internal/db`, `internal/middleware`, `internal/policy`, `internal/proxy`
- CLI/helper commands: `cmd/*`
- Frontend app: `frontend`
- Frontend build output: `frontend-dist`
- Runtime config: `config.yaml`
- Local credentials path is configured by `credential.auth_file`; do not commit real credentials.

## Key Data Flows

### Authentication Flow
1. User initiates login via `/admin/account/bootstrap`
2. Lingma provides auth/token through an OAuth-style callback (complex URL parsing in `internal/auth`)
3. Credentials are derived locally via Lingma binary (`internal/auth/credential_derive.go`) or remotely
4. Resulting credentials are stored in `credential.auth_file` (default `./auth/credentials.json`)

### API Request Flow
1. OpenAI-format request received (e.g. `/v1/chat/completions`)
2. Proxy layer (`internal/proxy`) translates request to Lingma's internal API format
3. Translated request is forwarded to Lingma
4. Streaming SSE responses are parsed and translated back to OpenAI format before returning to the client

### Credential Refresh
1. `CredentialManager` (`internal/proxy/credentials.go`) monitors token expiration (5-minute grace period before expiry)
2. Multiple refresh strategies are available: WebSocket (`WSRefresher`) and remote (`MultiRefresher`) in `internal/auth/refresh.go`
3. Strategy pattern enables pluggable refresh mechanisms
4. Updated credentials are persisted via `SaveCredentialFile`

## Key Concepts

### COSY Credentials
- Encrypted credential format generated via RSA encryption of a random temp key
- Core logic in `internal/auth/cosy_generate.go`
- Used as the primary credential storage mechanism

### Credential Manager
- Central credential lifecycle manager (`internal/proxy/credentials.go`)
- Tracks current state via `CredentialSnapshot` struct
- Coordinates token validation, refresh scheduling, and persistence

### Canonical Format
- Used throughout the proxy layer for request/response translation
- Core types defined in `internal/proxy/types.go`
- Ensures consistent mapping between OpenAI and Lingma API shapes

### Strategy Pattern
- Applied to token refresh: `WSRefresher`, `MultiRefresher` in `internal/auth/refresh.go`
- Enables pluggable refresh strategies selected based on runtime availability

## Build, Test, And Run Commands

Run backend commands from the repository root unless noted otherwise.

### Backend

- Run server: `go run . -config ./config.yaml`
- Run all Go tests: `go test ./...`
- Run one Go package: `go test ./internal/proxy`
- Run one Go test: `go test ./internal/proxy -run TestName`
- Format Go code: `gofmt -w <files>`
- Vet code when relevant: `go vet ./...`

### Frontend

Run these from `frontend`.

- Install dependencies: `npm install`
- Start Vite dev server: `npm run dev`
- Build frontend: `npm run build`
- Preview frontend build: `npm run preview`
- Run Playwright tests: `npx playwright test`
- Run one Playwright test: `npx playwright test tests/account-bootstrap.spec.ts`
- Debug Playwright tests: `npx playwright test --debug`

### Whole-App Scripts

Run these from the repository root.

- Development mode on Windows: `.\dev.ps1`
- Development mode on Linux/macOS: `./dev.sh`

The Go service normally listens on port `8080`. The Vite dev server uses port `3000` and proxies `/v1` and `/admin` to `http://127.0.0.1:8080`.

## Code Style

### General

- Keep changes focused on the requested behavior.
- Prefer existing package structure and local helper APIs over new abstractions.
- Do not reformat unrelated files.
- Do not commit generated or local runtime state unless the task explicitly requires it.
- Never log or expose sensitive tokens, credentials, callback URLs containing tokens, or authorization headers.

### Go

- Use standard Go formatting with `gofmt`.
- Keep package boundaries clear:
  - HTTP/admin/API behavior belongs in `internal/api`.
  - Lingma/authentication helpers belong in `internal/auth`.
  - Config loading belongs in `internal/config`.
  - SQLite persistence belongs in `internal/db`.
  - Request/response translation, model handling, SSE, signatures, and transports belong in `internal/proxy`.
  - Runtime policy evaluation belongs in `internal/policy`.
- Use `context.Context` for request-scoped and timeout-aware operations.
- Return errors with useful context; avoid swallowing errors silently.
- Keep tests close to the package being tested with `*_test.go`.
- Prefer table-driven tests for parser, mapper, policy, and transport edge cases.

### TypeScript And React

- Use React function components and hooks.
- Use TypeScript `.ts` and `.tsx` files.
- Use 2-space indentation, single quotes, and trailing commas where the local style already does.
- Put shared types in `frontend/src/types` when they are used across files.
- Keep API calls in `frontend/src/api`.
- Keep reusable hooks in `frontend/src/hooks`.
- Use explicit prop and function parameter types for public component/helper boundaries.
- Use `lucide-react` for icons when adding UI controls.

### Styling And Accessibility

- Follow the existing frontend styling conventions.
- Use semantic HTML where practical.
- Ensure interactive controls are keyboard accessible.
- Add labels, `aria-*` attributes, or accessible names where needed.
- Avoid `!important` unless there is no reasonable alternative.

## API And Runtime Notes

- Public API routes include `/v1/models`, `/v1/chat/completions`, and `/v1/messages`.
- Admin routes live under `/admin`, including:
  - `/admin/status` — service health and runtime status
  - `/admin/account` — account management
  - `/admin/account/bootstrap` — authentication flow entry point
- Admin authentication may use `Authorization: Bearer <admin_token>` or `X-Admin-Token: <admin_token>`.
- Frontend dev requests to `/v1` and `/admin` are proxied by Vite to the backend.
- The embedded production UI is served from `frontend-dist`, which is produced by the frontend build.

## Testing Expectations

- Run the smallest relevant test first while iterating.
- For backend logic changes, run the affected package tests and then `go test ./...` when feasible.
- For frontend changes, run `npm run build` from `frontend`.
- For user-facing frontend flows, add or update Playwright coverage under `frontend/tests`.
- Mock network/API behavior in Playwright tests where possible.
- Include both success and error cases for critical auth, proxy, policy, and admin flows.

## Configuration And Data Safety

- Use `VITE_` prefixes for frontend environment variables.
- Keep secrets out of source control.
- Treat `auth/credentials.json`, local databases, logs, and imported auth caches as sensitive local files.
- Do not alter real user credentials or large local data files unless the user explicitly asks.
- If a change needs sample credentials/config, use example files and placeholder values.

## Version Control Guidance

- Check `git status --short` before making broad edits.
- The working tree may contain user changes. Do not revert or overwrite unrelated changes.
- Follow conventional commit style when asked to commit.
- Keep commits small and focused.
