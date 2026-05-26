# Codebase Directory Structure

This document provides a comprehensive overview of the directory structure and key files in the Lingma proxy project.

## Root Directory

- `README.md` - Project overview and basic documentation
- `go.mod` / `go.sum` - Go module configuration and dependencies
- `dev.ps1` / `dev.sh` - Development scripts for Windows and Unix
- `import-auth.ps1` / `import-auth.sh` - Authentication import scripts
- `cmd/` - Command packages for various utilities
  - `lingma-auth-bootstrap/` - Main authentication bootstrap implementation
  - `encode-crack/` - Encoding/decoding utilities
  - `refresh-test/` - Token refresh testing utilities
  - `ws-refresh-test/` - WebSocket refresh testing utilities

## Internal Packages

### `internal/api/`
- Core API implementation
- Key files:
  - `server.go` - Main server implementation
  - `admin_logs_view.go` - Admin log viewing functionality
  - `vision_gate.go` - Vision feature gate implementation
  - `canonical_runtime.go` - Canonical request/response handling
  - `exchange_logger.go` - Request/response logging
  - `runtime_policy_test.go` - Policy enforcement

### `internal/auth/`
- Authentication and credential management
- Key files:
  - `cosy_generate.go` - COSY credential generation
  - `credential_derive.go` - Credential derivation logic
  - `store.go` - Credential storage management
  - `refresh.go` - Token refresh implementation
  - `remote_login.go` - Remote login handling
  - `callback_html.go` - HTML callback handling
  - `callback_inject_html.go` - HTML injection for login flow
  - `bootstrap_test.go` - Authentication flow testing
  - `store_test.go` - Credential storage testing

### `internal/proxy/`
- Core proxy functionality
- Key files:
  - `credentials.go` - Credential management
  - `types.go` - Core data structures
  - `models.go` - Model management
  - `anthropic_types.go` - Anthropic API specific types
  - `body.go` - Request/response body handling
  - `sse.go` - Server-Sent Events handling
  - `signature.go` - Request signing and verification
  - `tool_registry.go` - Tool call handling

### `internal/db/`
- Database management
- Key files:
  - `store.go` - Database connection and initialization
  - `migrations.go` - Database schema migrations
  - `logs.go` - Request logging
  - `policies.go` - Policy enforcement
  - `settings.go` - Configuration management
  - `canonical_records.go` - Canonical execution history

### `internal/policy/`
- Policy management
- Key files:
  - `evaluator.go` - Policy evaluation
  - `evaluator_test.go` - Policy evaluation testing

### `internal/middleware/`
- Middleware components
- Key files:
  - `logging.go` - Request logging

## Frontend

- `frontend/` - Web frontend implementation
  - `package.json` - Frontend dependencies
  - `vite.config.ts` - Vite configuration
  - `tsconfig.json` - TypeScript configuration
  - `src/` - Source code
    - `App.tsx` - Main application component
    - `main.tsx` - Entry point
    - `components/` - Reusable UI components
      - `CodeViewer.tsx` - Code display component
      - `EmptyState.tsx` - Empty state display
      - `ExchangeCard.tsx` - API exchange display
      - `LogDetailDrawer.tsx` - Log details display
      - `ReplayModal.tsx` - Request replay
      - `Skeleton.tsx` - Loading skeleton
      - `Spinner.tsx` - Loading indicator
      - `StatCard.tsx` - Statistics display
    - `hooks/` - Custom React hooks
      - `useAdminToken.ts` - Admin token management
      - `usePolling.ts` - Polling functionality
      - `useSettings.ts` - Settings management
    - `pages/` - Page components
      - `Dashboard.tsx` - Main dashboard
      - `LogDetail.tsx` - Log details view
    - `vite-env.d.ts` - Vite environment definitions
  - `tsconfig.app.json` - Application TypeScript configuration
  - `tsconfig.json` - Main TypeScript configuration
  - `tsconfig.node.json` - Node.js TypeScript configuration
  - `vite.config.ts` - Vite build configuration

## Documentation

- `docs/` - Project documentation
  - `superpowers/` - Superpowers skill documentation and plans
  - `code-landscape.md` - This file
  - `sqlite_database_schema.md` - Database schema documentation

## Configuration and Scripts

- `start.ps1` / `start.sh` - Production startup scripts
- `dev.ps1` / `dev.sh` - Development mode scripts
- `config.yaml` - Configuration file (not shown in git)
- `.git/` - Git configuration and hooks
- `.gitignore` - Files to ignore in git

## Test Files

- Files ending with `_test.go` - Unit and integration tests
- Found throughout the codebase in relevant directories

## Authentication Files

- `auth/` - Authentication-related files
  - `backup/` - Credential backup files
  - `credentials-remote.json` - Remote credentials file
  - `credentials.example.json` - Example credentials file

## Build and Development

- `Makefile` - Build and test automation
- `go.mod` / `go.sum` - Go module configuration
- `Dockerfile` - Docker configuration (if exists)
- `.dockerignore` - Docker build ignore file (if exists)

## External Dependencies

- `node_modules/` - Frontend dependencies
- `vendor/` - Go dependencies (if exists)

## Temporary and Generated Files

- `.worktrees/` - Git worktrees (for parallel development)
- `frontend-dist/` - Built frontend files
- `*.log` - Log files
- `*.db` - Database files
- `*.exe` / `*.dll` / `*.so` / `*.dylib` - Binary files
- `*.out` / `*.test` - Test binaries
- `*.lock` - Dependency lock files
- `.DS_Store` / `Thumbs.db` - OS files

## Security Sensitive Files (in .gitignore)

- `auth/credentials.json` - Local credentials
- `auth/credentials_remote.json` - Remote credentials
- `auth/bootstrap-callback.json` - Bootstrap callback
- `configs/client_id.txt` - Client ID
- `configs/session_key.txt` - Session key
- `configs/` - Configuration files
- `*.pem` - TLS certificates
- `*.key` - TLS private keys
- `*.crt` - TLS certificates
- `*.cert` - TLS certificates
- `*.p12` - TLS certificates
- `*.jks` - TLS certificates
- `*.keystore` - TLS certificates
- `*.yaml` / `*.yml` / `*.toml` / `*.json` - Configuration files
- `*.env` / `.env.*` - Environment files
- `*.pem` - TLS certificates
- `*.key` - TLS private keys
- `*.crt` - TLS certificates
- `*.cert` - TLS certificates
- `*.p12` - TLS certificates
- `*.jks` - TLS certificates
- `*.keystore` - TLS certificates
- `*.yaml` / `*.yml` / `*.toml` / `*.json` - Configuration files
- `*.env` / `.env.*` - Environment files