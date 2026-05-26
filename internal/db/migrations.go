package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

var sqliteMigrations = []string{
	`CREATE TABLE IF NOT EXISTS request_logs (
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
		prompt_tokens     INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens      INTEGER NOT NULL DEFAULT 0,
		ttft_ms           INTEGER NOT NULL DEFAULT 0,
		upstream_ms       INTEGER NOT NULL DEFAULT 0,
		downstream_ms     INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_created ON request_logs(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_model ON request_logs(model)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_status ON request_logs(status)`,

	`CREATE TABLE IF NOT EXISTS model_mappings (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		priority    INTEGER NOT NULL DEFAULT 0,
		name        TEXT NOT NULL,
		pattern     TEXT NOT NULL,
		target      TEXT NOT NULL,
		enabled     INTEGER NOT NULL DEFAULT 1,
		created_at  DATETIME NOT NULL,
		updated_at  DATETIME NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS policy_rules (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		priority         INTEGER NOT NULL DEFAULT 0,
		name             TEXT NOT NULL,
		enabled          INTEGER NOT NULL DEFAULT 1,
		match_json       TEXT NOT NULL,
		actions_json     TEXT NOT NULL,
		source           TEXT NOT NULL DEFAULT 'native',
		created_at       DATETIME NOT NULL,
		updated_at       DATETIME NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_policy_rules_priority ON policy_rules(priority ASC, id ASC)`,
	`CREATE INDEX IF NOT EXISTS idx_policy_rules_enabled ON policy_rules(enabled)`,

	`CREATE TABLE IF NOT EXISTS canonical_execution_records (
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
	)`,
	`CREATE INDEX IF NOT EXISTS idx_canonical_execution_created ON canonical_execution_records(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_canonical_execution_protocol ON canonical_execution_records(ingress_protocol)`,

	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS http_exchanges (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		log_id      TEXT NOT NULL,
		direction   TEXT NOT NULL,
		phase       TEXT NOT NULL,
		timestamp   DATETIME NOT NULL,
		method      TEXT,
		url         TEXT,
		path        TEXT,
		status_code INTEGER,
		headers     TEXT DEFAULT '',
		body        TEXT DEFAULT '',
		duration_ms INTEGER,
		error       TEXT DEFAULT '',
		raw_stream  TEXT DEFAULT '',
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_exchanges_log_id ON http_exchanges(log_id)`,
	`CREATE INDEX IF NOT EXISTS idx_exchanges_timestamp ON http_exchanges(timestamp)`,
}

var postgresMigrations = []string{
	`CREATE TABLE IF NOT EXISTS request_logs (
		id           TEXT PRIMARY KEY,
		created_at   TIMESTAMPTZ NOT NULL,
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
		prompt_tokens     INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens      INTEGER NOT NULL DEFAULT 0,
		ttft_ms           INTEGER NOT NULL DEFAULT 0,
		upstream_ms       INTEGER NOT NULL DEFAULT 0,
		downstream_ms     INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_created ON request_logs(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_model ON request_logs(model)`,
	`CREATE INDEX IF NOT EXISTS idx_logs_status ON request_logs(status)`,

	`CREATE TABLE IF NOT EXISTS model_mappings (
		id          BIGSERIAL PRIMARY KEY,
		priority    INTEGER NOT NULL DEFAULT 0,
		name        TEXT NOT NULL,
		pattern     TEXT NOT NULL,
		target      TEXT NOT NULL,
		enabled     INTEGER NOT NULL DEFAULT 1,
		created_at  TIMESTAMPTZ NOT NULL,
		updated_at  TIMESTAMPTZ NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS policy_rules (
		id               BIGSERIAL PRIMARY KEY,
		priority         INTEGER NOT NULL DEFAULT 0,
		name             TEXT NOT NULL,
		enabled          INTEGER NOT NULL DEFAULT 1,
		match_json       JSONB NOT NULL,
		actions_json     JSONB NOT NULL,
		source           TEXT NOT NULL DEFAULT 'native',
		created_at       TIMESTAMPTZ NOT NULL,
		updated_at       TIMESTAMPTZ NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_policy_rules_priority ON policy_rules(priority ASC, id ASC)`,
	`CREATE INDEX IF NOT EXISTS idx_policy_rules_enabled ON policy_rules(enabled)`,
	// Convert policy_rules JSON columns from TEXT to JSONB (for upgrades from older schemas)
	`DO $$ BEGIN
		ALTER TABLE policy_rules ALTER COLUMN match_json TYPE JSONB USING match_json::jsonb;
	EXCEPTION WHEN others THEN
	END $$`,
	`DO $$ BEGIN
		ALTER TABLE policy_rules ALTER COLUMN actions_json TYPE JSONB USING actions_json::jsonb;
	EXCEPTION WHEN others THEN
	END $$`,

	`CREATE TABLE IF NOT EXISTS canonical_execution_records (
		id                    TEXT PRIMARY KEY,
		created_at            TIMESTAMPTZ NOT NULL,
		ingress_protocol      TEXT NOT NULL,
		ingress_endpoint      TEXT NOT NULL,
		session_id            TEXT DEFAULT '',
		pre_policy_json       TEXT NOT NULL,
		post_policy_json      TEXT NOT NULL,
		session_snapshot_json TEXT DEFAULT '',
		southbound_request    TEXT DEFAULT '',
		sidecar_json          TEXT DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_canonical_execution_created ON canonical_execution_records(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_canonical_execution_protocol ON canonical_execution_records(ingress_protocol)`,

	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS http_exchanges (
		id          BIGSERIAL PRIMARY KEY,
		log_id      TEXT NOT NULL,
		direction   TEXT NOT NULL,
		phase       TEXT NOT NULL,
		timestamp   TIMESTAMPTZ NOT NULL,
		method      TEXT,
		url         TEXT,
		path        TEXT,
		status_code INTEGER,
		headers     TEXT DEFAULT '',
		body        TEXT DEFAULT '',
		duration_ms INTEGER,
		error       TEXT DEFAULT '',
		raw_stream  TEXT DEFAULT '',
		created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_exchanges_log_id ON http_exchanges(log_id)`,
	`CREATE INDEX IF NOT EXISTS idx_exchanges_timestamp ON http_exchanges(timestamp)`,
}

func runMigrations(conn *sql.DB, driver string) error {
	migrations := sqliteMigrations
	if driver == "postgres" {
		migrations = postgresMigrations
	}
	for _, ddl := range migrations {
		if _, err := conn.Exec(ddl); err != nil {
			return err
		}
	}
	return backfillModelMappingsIntoPolicies(conn, driver)
}

func backfillModelMappingsIntoPolicies(conn *sql.DB, driver string) error {
	var query string
	if driver == "postgres" {
		query = `
		SELECT id,priority,name,pattern,target,enabled,created_at,updated_at
		FROM model_mappings
		WHERE NOT EXISTS (
			SELECT 1
			FROM policy_rules
			WHERE source='model_mapping'
			  AND match_json::jsonb->>'requested_model' = model_mappings.pattern
			  AND actions_json::jsonb->>'rewrite_model' = model_mappings.target
		)
		ORDER BY priority ASC, id ASC`
	} else {
		query = `
		SELECT id,priority,name,pattern,target,enabled,created_at,updated_at
		FROM model_mappings
		WHERE NOT EXISTS (
			SELECT 1
			FROM policy_rules
			WHERE source='model_mapping'
			  AND json_extract(match_json, '$.requested_model') = model_mappings.pattern
			  AND json_extract(actions_json, '$.rewrite_model') = model_mappings.target
		)
		ORDER BY priority ASC, id ASC`
	}

	rows, err := conn.Query(query)
	if err != nil {
		return err
	}
	var mappings []ModelMapping
	for rows.Next() {
		var mapping ModelMapping
		var enabled int
		if err := rows.Scan(
			&mapping.ID,
			&mapping.Priority,
			&mapping.Name,
			&mapping.Pattern,
			&mapping.Target,
			&enabled,
			&mapping.CreatedAt,
			&mapping.UpdatedAt,
		); err != nil {
			rows.Close()
			return err
		}
		mapping.Enabled = enabled == 1
		mappings = append(mappings, mapping)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, mapping := range mappings {
		matchJSON, err := json.Marshal(PolicyMatch{RequestedModel: mapping.Pattern})
		if err != nil {
			return err
		}
		actionsJSON, err := json.Marshal(PolicyActions{RewriteModel: &mapping.Target})
		if err != nil {
			return err
		}
		if _, err := conn.Exec(
			`INSERT INTO policy_rules (priority,name,enabled,match_json,actions_json,source,created_at,updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			mapping.Priority,
			mapping.Name,
			boolToInt(mapping.Enabled),
			string(matchJSON),
			string(actionsJSON),
			"model_mapping",
			mapping.CreatedAt,
			mapping.UpdatedAt,
		); err != nil {
			return fmt.Errorf("backfill model mapping %d: %w", mapping.ID, err)
		}
	}
	return nil
}
