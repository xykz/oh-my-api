# Lingma Multi-Region Multi-Account Design

> Date: 2026-05-20
> Status: Draft for user review
> Scope: backend account routing, credential storage, region adapter boundaries, admin account UI, and tests.

## Goal

Let lingma2api support multiple Lingma accounts across China and International editions, route requests through a configurable account pool, and expose clear admin UI controls for China login, International login, account status, and routing mode.

## Confirmed Decisions

- Add a global account routing mode in config: `china_only`, `international_only`, or `mixed`.
- In `mixed`, load balancing is account-average. Two China accounts plus three International accounts means five equal candidates.
- Keep one credential JSON file, but upgrade it to `accounts: [...]`.
- Backward-compatible read of the current single-account credential file as one China account.
- Use an adapter-first backend architecture. Account selection, load balancing, and region API differences are separate concepts.
- China adapter fully preserves current COSY behavior.
- International adapter defaults to `https://api.lingma.ai` as a configurable base URL, but its concrete protocol remains a separate implementation point until real request samples are available.
- The UI distinguishes China login and International login buttons.
- Current `frontend/src/pages/Account.tsx` mojibake should be fixed while rebuilding the account page.

## External Evidence

Alibaba Cloud's public firewall documentation, last updated 2026-05-18, lists `https://lingma.alibabacloud.com` as the Qoder CN API service for code completion and chat. Public search did not reveal a documented contract for `api.lingma.ai` or `api.lingma.cn`, although local DNS and TCP checks showed both hosts resolve and accept HTTPS connections. Therefore, the design treats International API details as configurable and adapter-scoped rather than guessing paths, headers, or payload shapes.

Source: https://www.alibabacloud.com/help/en/lingma/qoder-cn/user-guide/firewall-configuration

## Configuration

Add an `account` section to `config.yaml`:

```yaml
account:
  routing_mode: mixed
  load_balance: round_robin
  china_base_url: "https://api.lingma.cn"
  international_base_url: "https://api.lingma.ai"
```

Allowed values:

- `routing_mode`: `china_only`, `international_only`, `mixed`
- `load_balance`: `round_robin` for the first implementation

Existing `lingma.base_url` remains supported for backward compatibility and as the China fallback if `account.china_base_url` is empty. Existing `lingma.cosy_version`, transport, and OAuth callback settings continue to apply to the China adapter.

Missing `account` uses:

- `routing_mode: mixed`
- `load_balance: round_robin`
- `china_base_url: cfg.Lingma.BaseURL`
- `international_base_url: https://api.lingma.ai`

Invalid values fail config loading with clear errors.

## Credential File

The credential file remains `credential.auth_file`, but schema version 2 stores multiple accounts:

```json
{
  "schema_version": 2,
  "accounts": [
    {
      "id": "china-1",
      "label": "China account 1",
      "region": "china",
      "enabled": true,
      "source": "oauth_v2_manual_callback",
      "lingma_version_hint": "2.11.2",
      "obtained_at": "2026-05-20T00:00:00+08:00",
      "updated_at": "2026-05-20T00:00:00+08:00",
      "token_expire_time": "1770000000000",
      "auth": {
        "cosy_key": "redacted",
        "encrypt_user_info": "redacted",
        "user_id": "uid",
        "machine_id": "machine"
      },
      "oauth": {
        "access_token": "redacted",
        "refresh_token": "redacted"
      }
    },
    {
      "id": "intl-1",
      "label": "International account 1",
      "region": "international",
      "enabled": true,
      "source": "manual_import",
      "auth": {
        "access_token": "redacted"
      }
    }
  ]
}
```

The existing schema version 1 format is read as:

- `id`: stable generated ID from user ID or a hash of non-secret identity fields
- `label`: `China account`
- `region`: `china`
- `enabled`: `true`
- account metadata copied from the top-level fields

Writers should preserve schema version 2. Secrets are never returned from list/status endpoints except existing masked or boolean fields.

## Runtime Components

### Account Store

`AccountStore` replaces the single-snapshot role of `CredentialManager` for routing-aware code. It is responsible for:

- Loading schema v1 or v2 credential files.
- Returning sanitized account summaries for admin UI.
- Returning secret-bearing account snapshots only to transports/adapters.
- Adding a newly bootstrapped account without deleting unrelated accounts.
- Enabling, disabling, refreshing, and testing individual accounts.

### Account Pool And Balancer

`AccountPool` filters accounts by config:

- `china_only`: enabled China accounts only
- `international_only`: enabled International accounts only
- `mixed`: all enabled accounts

`RoundRobinBalancer` selects one account from the filtered list with equal account weight. It should be concurrency-safe and deterministic enough for tests. If no account is eligible, request handling returns a mapped credentials-unavailable error that includes the routing mode but not secrets.

### Region Adapter

Introduce a backend interface shaped around real capabilities:

```go
type RegionAdapter interface {
    Region() AccountRegion
    ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error)
    BuildChatRequest(ctx context.Context, canonical CanonicalRequest, modelKey string, account AccountSnapshot) (RemoteChatRequest, error)
    StreamChat(ctx context.Context, request RemoteChatRequest, account AccountSnapshot) (io.ReadCloser, error)
    UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error)
    TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult
}
```

The exact method split may be adjusted to fit existing package boundaries, but the design boundary is fixed: account selection is outside the adapter; endpoint paths, headers, body mapping, response parsing, and region-specific support live inside the adapter.

### China Adapter

The China adapter moves the current behavior behind the interface:

- COSY signed headers from `internal/proxy/signature.go`
- model list at the existing model path
- chat body construction from `internal/proxy/body.go`
- streaming through existing native transport behavior
- image upload through existing upload path
- OAuth/bootstrap credential generation through existing China login flow

The migration must keep current China-only behavior working with old config and old credentials.

### International Adapter

The International adapter starts with:

- configurable `base_url`, default `https://api.lingma.ai`
- credential schema support for `region: international`
- admin visibility and routing eligibility
- explicit errors for unsupported protocol operations, such as `international adapter protocol not configured`

Once real International request samples are available, only this adapter should need protocol-specific changes. The balancer, account store, admin UI, and request pipeline should remain stable.

## Request Flow

OpenAI and Anthropic request handling should follow this flow:

1. Decode request and validate it.
2. Build canonical request and apply runtime policy.
3. Resolve requested model, using aggregated model metadata where available.
4. Select an account from the eligible account pool.
5. Pick the adapter for the selected account region.
6. Upload images through the selected adapter if needed and supported.
7. Build the region-specific remote chat request.
8. Stream through the selected adapter.
9. Persist logs/canonical execution metadata with selected account ID, account label, and region.

The selected account must remain stable for the full request. A single chat request is not split across accounts.

## Models

Model refresh should aggregate across eligible accounts:

- Use the active routing mode to decide which accounts participate.
- Try each eligible account.
- Merge model keys and aliases.
- Preserve last error per region or per account for admin diagnostics.
- Expose the same OpenAI-compatible `/v1/models` shape externally.
- Admin model data may include region/account metadata, but public model responses should not leak account IDs unless explicitly designed later.

If an International account is eligible but its adapter is not configured, model refresh records that account error and continues with other accounts.

## Admin API

Add or evolve admin endpoints around account collections:

- `GET /admin/account` returns routing mode, load-balance strategy, sanitized account summaries, aggregate counts, and backward-compatible fields for the currently selected or first China account where practical.
- `POST /admin/account/bootstrap` accepts `region` and `method`.
- `POST /admin/account/test` accepts an optional account ID; without one, tests eligible accounts or the selected account depending on existing UI needs.
- `POST /admin/account/refresh` accepts an optional account ID.
- Add enable/disable operations for account IDs, either through `PATCH /admin/account/{id}` or a small explicit endpoint.

Admin responses include:

- account ID
- label
- region
- enabled
- source
- loaded/valid/token-expired status
- OAuth presence booleans
- masked identity summaries

Admin responses never include raw tokens, authorization headers, callback URLs containing tokens, `cosy_key`, or `encrypt_user_info`.

## Bootstrap And Login

China login:

- Existing browser callback flow remains the real implemented login flow.
- It now saves or updates one account in the schema v2 file instead of replacing the whole file.
- The China login button sends `region: "china"`.

International login:

- The UI shows a separate International login button.
- Until the real International login protocol is known, clicking it starts an explicit placeholder/import flow rather than a fake login.
- The backend returns a clear unsupported/protocol-not-configured response for International bootstrap.
- A later implementation can add manual import of a captured International credential JSON without changing the account pool model.

## Frontend UI

Rebuild the Account page as an account-pool management page:

- Top summary: routing mode, load-balance strategy, total enabled accounts, China count, International count.
- Region filters: all, China, International.
- Account rows/cards show label, region, enabled state, user ID, machine ID or credential summary, token expiry, source, and last test result.
- Buttons use normal Simplified Chinese UI text for these actions: Login China Lingma, Login International Lingma, Refresh account, Test connection, and enable/disable.
- China and International login buttons must be visually distinct but consistent with existing styling.
- The International login placeholder must be honest: protocol samples are needed before real login can be completed.
- Fix mojibake in Account page text while making these changes.

Dashboard and overview should show account-pool health: total usable accounts, China usable accounts, International usable accounts, current routing mode, and recent selected region if available.

## Error Handling

No eligible accounts:

- Return credentials-unavailable style error.
- Include routing mode and eligible region in the message.
- Do not list disabled accounts with sensitive details.

Unsupported International protocol:

- Admin test and model refresh record `international adapter protocol not configured`.
- Public chat requests fail only if the balancer selected an International account. In early rollout, this means users should avoid `mixed` or International-only with unconfigured International accounts unless they are testing.

Account failure during request:

- Initial implementation does not retry another account mid-stream.
- Future failover can be added as a separate load-balance strategy.

Token expiry:

- Preserve existing China refresh behavior where possible.
- Per-account refresh should not overwrite unrelated accounts.

## Testing Strategy

Backend tests:

- Config parsing accepts defaults and rejects invalid routing/load-balance values.
- Credential loader reads schema v1 as one China account.
- Credential loader reads schema v2 with China and International accounts.
- Account pool filters correctly for `china_only`, `international_only`, and `mixed`.
- Round-robin selection is account-average and concurrency-safe.
- China adapter preserves existing model list and chat signing behavior.
- International adapter returns explicit protocol-not-configured errors.
- Model aggregation merges models and records per-account errors.
- Chat flow selects one account and passes it to the matching adapter.
- Admin account responses do not expose raw secrets.

Frontend tests:

- Account page renders China and International login controls.
- Routing summary displays normal Simplified Chinese text for China-only, International-only, or mixed mode.
- Account list separates or filters China and International accounts.
- International login placeholder is shown when protocol is not configured.
- Existing bootstrap polling behavior still works for China login.

Verification commands:

- `go test ./internal/config ./internal/proxy ./internal/api`
- `go test ./...` when feasible
- `npm run build` from `frontend`
- `npx playwright test tests/account-bootstrap.spec.ts` when UI flows change

## Migration And Compatibility

The first run against an old credential file should not destroy it. The loader can read v1 directly; writing happens only after account mutation or successful login. When writing v2:

- keep all known existing credential fields inside the China account
- assign a stable account ID
- set `region: china`
- set `enabled: true`

Existing configs without `account` keep working as China-only behavior in practice if only one China account exists. Users who add International accounts can choose `routing_mode` explicitly.

## Out Of Scope

- Guessing undocumented International paths, headers, or body formats.
- Weighted load balancing.
- Retry/failover to another account mid-request.
- Per-model routing rules.
- Concurrent multi-account login sessions.
- Public `/v1/models` account-specific metadata.
- Committing local credentials, databases, logs, or imported auth caches.

## Open Follow-Up

The International adapter needs real protocol evidence before it can be completed. Useful samples include:

- a successful model-list request
- a successful chat request
- required auth headers
- login callback shape
- stored credential shape
- error response for expired or invalid credentials
