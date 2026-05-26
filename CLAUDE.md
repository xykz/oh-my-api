# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common Development Tasks

### Authentication
The authentication system uses Lingma for token management. Key files:
- `cmd/lingma-auth-bootstrap/main.go` - Main entry point for Lingma authentication flow
- `internal/auth/cosy_generate.go` - Handles generation of COSY credentials
- `internal/auth/credential_derive.go` - Derives credentials using Lingma binary or remotely
- `internal/auth/store.go` - Manages credential storage

### Building the Project
To build the project:
```bash
# For development
make build

# For production
make release
```

### Running Tests
To run tests:
```bash
# All tests
make test

# Specific package tests
make test PKG=internal/auth

# Specific test
make test PKG=internal/auth TEST=TestParseCallbackHTMLHints
```

## High-Level Architecture

The repository implements a proxy that translates between OpenAI API format and Lingma's internal API. Key components:

1. **Authentication Layer** (`internal/auth`):
   - Manages Lingma authentication flow
   - Generates COSY credentials
   - Handles token refresh
   - Stores credentials in `credentials.json`

2. **Proxy Layer** (`internal/proxy`):
   - Core proxy functionality
   - Translates between OpenAI and Lingma API formats
   - Manages sessions and streaming
   - Handles tool calls and responses

3. **API Layer** (`internal/api`):
   - Exposes HTTP endpoints
   - Handles admin functions
   - Manages the bootstrap flow
   - Provides status endpoints

4. **Model Layer** (`internal/models.go`):
   - Manages model information
   - Handles model listing and selection

## Key Concepts

1. **COSY Credentials**:
   - Generated via RSA encryption of a random temp key
   - Used for secure credential storage
   - Found in `internal/auth/cosy_generate.go`

2. **Credential Manager**:
   - Manages credential loading and refreshing
   - Found in `internal/proxy/credentials.go`
   - Uses `CredentialSnapshot` to track current state

3. **Token Refresh**:
   - Tokens can be refreshed via WebSocket or remotely
   - Implemented in `internal/auth/refresh.go`
   - Uses multiple strategies (WSRefresher, MultiRefresher)

## Development Notes

1. When working with credentials:
   - Always use `SaveCredentialFile` to write credentials
   - Never store credentials in version control
   - Credentials should be validated after loading

2. When implementing new features:
   - Follow the existing pattern of using CanonicalRequest
   - Ensure proper error handling
   - Add appropriate tests

3. When working with the proxy:
   - Be careful with stream handling
   - Ensure proper error handling
   - Maintain compatibility with both OpenAI and Lingma formats

## Important Files

- `internal/auth/cosy_generate.go` - Critical for credential generation
- `internal/auth/refresh.go` - Manages token refresh
- `internal/proxy/credentials.go` - Core credential management
- `internal/api/server.go` - Main server implementation
- `internal/proxy/types.go` - Core data structures

## Security Considerations

1. Never log sensitive credential information
2. Always validate credentials before use
3. Use the `CredentialManager` for credential handling
4. Be careful with token expiration and refresh logic

## Testing

1. Use `make test` to run all tests
2. For specific tests:
   - `make test PKG=internal/auth`
   - `make test PKG=internal/auth TEST=TestParseCallbackHTMLHints`
3. Pay special attention to:
   - `internal/auth/cosy_generate_test.go` - Credential generation
   - `internal/auth/refresh_test.go` - Token refresh
   - `internal/proxy/credentials_test.go` - Credential management

## Deployment

1. Use `start.ps1` for Windows
2. Use `start.sh` for Linux/macOS
3. The service listens on `0.0.0.0:8080` by default
4. Credentials are stored in `./auth/credentials.json` by default

## Key Data Flows

1. Authentication flow:
   - User initiates login
   - Lingma provides auth/token via callback
   - Credentials are derived and stored

2. API request flow:
   - OpenAI request received
   - Translated to Lingma format
   - Request processed
   - Response streamed back in OpenAI format

3. Credential refresh:
   - Check token expiration
   - Use refresh strategies
   - Update credentials file

## Key Patterns

1. **Strategy pattern** - Used in token refresh (WSRefresher, MultiRefresher)
2. **Canonical format** - Used throughout the proxy layer
3. **Callback handling** - Complex URL parsing and decode logic in auth package

## Key External Dependencies

1. Lingma binary for credential derivation
2. Lingma's WebSocket for token refresh
3. Lingma's remote API for user info

## Key Configuration

Configuration is managed through:
- `config.yaml`
- Environment variables
- Command line flags

Key auth configuration:
- `credential.auth_file` - Path to credentials.json
- `lingma.oauth_callback_addr` - Callback address for Lingma

## Key Endpoints

- `/v1/models` - Model listing
- `/v1/chat/completions` - Chat endpoint
- `/admin/status` - Status endpoint
- `/admin/account` - Account management
- `/admin/account/bootstrap` - Authentication flow endpoint

## Key Testing Considerations

1. Always test both success and failure cases
2. Pay special attention to:
   - Token expiration handling
   - Credential validation
   - Error cases in auth flow

## Key Security Aspects

1. Credentials are stored with restricted permissions (0o600)
2. Sensitive information is not logged
3. Token refresh has a 5 minute expiration grace period

## Key Performance Considerations

1. Streaming is handled efficiently with block-based processing
2. Avoid unnecessary credential refreshes
3. Use connection pooling where possible