# SQLite Database Schema

This document provides a detailed description of the SQLite database schema used in the Lingma API proxy system.

## request_logs Table

The `request_logs` table stores detailed information about API requests and responses.

### Schema
```sql
CREATE TABLE IF NOT EXISTS request_logs (
	id           TEXT PRIMARY KEY,
	created_at   DATETIME NOT NULL,
	session_id   TEXT DEFAULT '',
	model        TEXT NOT NULL,
	mapped_model TEXT NOT NULL,
	stream       INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL,
	error_msg    TEXT DEFAULT '',
	downstream_method TEXT NOT NULL,
	downstream_path   TEXT NOT NULL,
	downstream_req    TEXT NOT NULL,
	downstream_resp   TEXT NOT NULL,
	upstream_req      TEXT NOT NULL,
	upstream_resp     TEXT NOT NULL,
	upstream_status   INTEGER NOT NULL,
	prompt_tokens     INTEGER NOT NULL DEFAULT 100,
	completion_tokens INTEGER NOT NULL DEFAULT 50,
	total_tokens      INTEGER NOT NULL DEFAULT 150,
	ttft_ms           INTEGER NOT NULL DEFAULT 200,
	upstream_ms       INTEGER NOT NULL DEFAULT 1500,
	downstream_ms     INTEGER NOT NULL DEFAULT 1600
)
```

### Indexes
- `idx_logs_created` on `created_at` (DESC)
- `idx_logs_model` on `model`
- `idx_logs_status` on `status`

### Description
- `id`: Unique identifier for the request
- `created_at`: Timestamp of when the request was created
- `session_id`: ID of the session (if any)
- `model`: The model requested by the client
- `mapped_model`: The model that was actually used after mapping
- `stream`: Whether this was a streaming request (0 = false, 1 = true)
- `status`: Status of the request (e.g., "success", "error")
- `error_msg`: Any error message that occurred during processing
- `downstream_method`: HTTP method used by the client
- `downstream_path`: HTTP path used by the client
- `downstream_req`: The request sent by the client
- `downstream_resp`: The response sent to the client
- `upstream_req`: The request sent to the upstream service
- `upstream_resp`: The response received from the upstream service
- `upstream_status`: HTTP status code from the upstream service
- `prompt_tokens`: Number of prompt tokens used
- `completion_tokens`: Number of completion tokens used
- `total_tokens`: Total tokens used (prompt + completion)
- `ttft_ms`: Time to first token in milliseconds
- `upstream_ms`: Time taken for the upstream request in milliseconds
- `downstream_ms`: Time taken for the downstream request in milliseconds

This table is used to track detailed information about each API request for monitoring, analysis, and performance tracking purposes.

## model_mappings Table

The `model_mappings` table stores configuration for mapping between different model names.

### Schema
```sql
CREATE TABLE IF NOT EXISTS model_mappings (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	priority    INTEGER NOT NULL DEFAULT 0,
	named        TEXT NOT NULL,
	pattern     TEXT NOT NULL,
	target      TEXT NOT NULL,
	enabled     INTEGER NOT NULL DEFAULT 1,
	created_at  DATETIME NOT NULL,
	updated_at  DATETIME NOT NULL
)
```

### Indexes
- No explicit indexes defined for this table

### Description
- `id`: Unique identifier for the mapping
- `priority`: Priority of this mapping (lower numbers have higher priority)
- `name`: Name of the mapping
- `pattern`: Pattern to match against requested model names
- `target`: Target model to use when the pattern matches
- `enabled`: Whether this mapping is currently enabled (0 = disabled, 1 = enabled)
- `created_at`: Timestamp of when the mapping was created
- `updated_at`: Timestamp of when the mapping was last updated

This table is used to define how requested model names should be mapped to different models in the system.

## policy_rules Table

The `policy_rules` table stores policy rules that determine how requests should be handled.

### Schema
```sql
CREATE TABLE IF NOT EXISTS policy_rules (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	priority         INTEGER NOT NULL DEFAULT 0,
	name             TEXT NOT NULL,
	enabled          INTEGER NOT NULL DEFAULT 1,
	match_json       TEXT NOT NULL,
	actions_json     TEXT NOT NULL,
	source           TEXT NOT NULL DEFAULT 'native',
	created_at       DATETIME NOT NULL,
	updated_at       DATETIME NOT NULL
)
```

### Indexes
- `idx_policy_rules_priority` on `priority` (ASC) and `id` (ASC)
- `idx_policy_rules_enabled` on `enabled`

### Description
- `id`: Unique identifier for the policy
- `priority`: Priority of the policy (lower numbers have higher priority)
- `name`: Name of the policy
- `enabled`: Whether the policy is enabled (0 = disabled, 1 = enabled)
- `match_json`: JSON string defining the match criteria for the policy
- `actions_json`: JSON string defining the actions to take when a match is found
- `source`: Where the policy came from (e.g., "native", "model_mapping")
- `created_at`: Timestamp of when the policy was created
- `updated_at`: Timestamp of when the policy was last updated

This table stores policies that can match against requests and apply actions to modify how requests are handled.

## canonical_execution_records Table

The `canonical_execution_records` table stores canonical execution records for requests.

### Schema
```sql
CREATE TABLE IF NOT EXISTS canonical_execution_records (
	id                    TEXT PRIMARY KEY,
	created_at            DATETIME NOT NULL,
	ingress_protocol      TEXT NOT NULL,
	ingress_endpoint      TEXT NOT NULL,
	session_id            TEXT DEFAULT '',
	pre_policy_json       TEXT NOT NULL,
	post_policy_json      TEXT NOT NULL,
	session_snapshot_json TEXT DEFAULT '',
	southbound_request    TEXT DEFAULT '',
	sidecar_json          TEXT DEFAULT ''
)
```

### Indexes
- `idx_canonical_execution_created` on `created_at` (DESC)
- `idx_canonical_execution_protocol` on `ingress_protocol`

### Description
- `id`: Unique identifier for the execution record
- `created_at`: Timestamp of when the record was created
- `ingress_protocol`: Protocol used for the request
- `ingress_endpoint`: Endpoint that was accessed
- `session_id`: ID of the session (if any)
- `pre_policy_json`: JSON representation of the request before policy was applied
- `post_policy_json`: JSON representation of the request after policy was applied
- `session_snapshot_json`: JSON snapshot of the session
- `southbound_request`: The southbound request
- `sidecar_json`: JSON data from the sidecar

This table stores canonical execution records that capture the state of requests before and after policy application.

## settings Table

The `settings` table stores application settings.

### Schema
```sql
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
)
```

### Description
- `key`: The name of the setting
- `value`: The value of the setting

This table is used to store application-wide settings and configuration.

## http_exchanges Table

The `http_exchanges` table stores HTTP exchange data for requests.

### Schema
```sql
CREATE TABLE IF NOT EXISTS http_exchanges (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	log_id      TEXT NOT NULL,
	direction   TEXT NOT NULL,
	phase       TEXT NOT NULL,
	imestamp   DATETIME NOT NULL,
	method      TEXT,
	url         TEXT,
	path        TEXT,
	status_code INTEGER,
	theaders     TEXT DEFAULT '',
	body        TEXT DEFAULT '',
	duration_ms INTEGER,
	error       TEXT DEFAULT '',
	raw_stream  TEXT DEFAULT '',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

### Indexes
- `idx_exchanges_log_id` on `log_id`
- `idx_exchanges_timestamp` on `timestamp`

### Description
- `log_id`: ID of the associated request log
- `direction`: Direction of the exchange (e.g., "downstream", "upstream")
- `phase`: Phase of the exchange (e.g., "request", "response")
- `timestamp`: When the exchange occurred
- `method`: HTTP method used
- `url`: Full URL of the exchange
- `path`: Path of the exchange
- `status_code`: HTTP status code
- `headers`: HTTP headers
- `body`: HTTP body
- `duration_ms`: Duration of the exchange in milliseconds
- `error`: Any error that occurred
- `raw_stream`: Raw stream data
- `created_at`: Timestamp of when the exchange was created

This table stores detailed HTTP exchange data for debugging and analysis.

## Summary

The SQLite database in this system serves several purposes:

1. **Request Logging**: The `request_logs` table captures detailed information about each API request for monitoring and analysis.
2. **Model Mapping**: The `model_mappings` table allows for mapping between different model names.
3. **Policy Enforcement**: The `policy_rules` table defines policies that can be applied to requests.
4. **Execution Records**: The `canonical_execution_records` captures the state of requests before and after policy application.
5. **Configuration**: The `settings` table stores application-wide settings.
6. **HTTP Exchange Tracking**: The `http_exchanges` table tracks detailed HTTP exchange data for debugging and analysis.

The database is designed to support the API proxy's functionality of translating between OpenAI API format and Lingma's internal API format, while providing comprehensive logging, policy application, and request tracking.