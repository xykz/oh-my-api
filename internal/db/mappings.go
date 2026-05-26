package db

import (
	"context"
	"time"
)

type ModelMapping struct {
	ID        int       `json:"id"`
	Priority  int       `json:"priority"`
	Name      string    `json:"name"`
	Pattern   string    `json:"pattern"`
	Target    string    `json:"target"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) ListMappings(ctx context.Context) ([]ModelMapping, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,priority,name,pattern,target,enabled,created_at,updated_at FROM model_mappings ORDER BY priority ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ModelMapping
	for rows.Next() {
		var m ModelMapping
		var enabled int
		if err := rows.Scan(&m.ID, &m.Priority, &m.Name, &m.Pattern, &m.Target, &enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		items = append(items, m)
	}
	if items == nil {
		items = []ModelMapping{}
	}
	return items, rows.Err()
}

func (s *Store) CreateMapping(ctx context.Context, m *ModelMapping) error {
	now := time.Now()
	row := s.db.QueryRowContext(ctx, s.sql(
		`INSERT INTO model_mappings (priority,name,pattern,target,enabled,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`),
		m.Priority, m.Name, m.Pattern, m.Target, boolToInt(m.Enabled), now, now)
	if err := row.Scan(&m.ID); err != nil {
		return err
	}
	m.CreatedAt = now
	m.UpdatedAt = now
	return nil
}

func (s *Store) UpdateMapping(ctx context.Context, m *ModelMapping) error {
	_, err := s.db.ExecContext(ctx, s.sql(
		`UPDATE model_mappings SET priority=$1,name=$2,pattern=$3,target=$4,enabled=$5,updated_at=$6 WHERE id=$7`),
		m.Priority, m.Name, m.Pattern, m.Target, boolToInt(m.Enabled), time.Now(), m.ID)
	return err
}

func (s *Store) DeleteMapping(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, s.sql(`DELETE FROM model_mappings WHERE id=$1`), id)
	return err
}

func (s *Store) GetEnabledMappings(ctx context.Context) ([]ModelMapping, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,priority,name,pattern,target,enabled,created_at,updated_at FROM model_mappings WHERE enabled=1 ORDER BY priority ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ModelMapping
	for rows.Next() {
		var m ModelMapping
		var enabled int
		if err := rows.Scan(&m.ID, &m.Priority, &m.Name, &m.Pattern, &m.Target, &enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		items = append(items, m)
	}
	if items == nil {
		items = []ModelMapping{}
	}
	return items, rows.Err()
}
