package db

import (
	"database/sql"
	"fmt"
	"regexp"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	driver string
}

// Open opens a database connection using the given driver and DSN.
// Supported drivers: "sqlite", "postgres".
func Open(driver, dsn string) (*Store, error) {
	var conn *sql.DB
	var err error

	switch driver {
	case "sqlite":
		conn, err = sql.Open("sqlite", dsn)
	case "postgres":
		conn, err = sql.Open("postgres", dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}

	// PostgreSQL uses connection pool; SQLite needs single connection
	if driver == "sqlite" {
		conn.SetMaxOpenConns(1)
	} else {
		conn.SetMaxOpenConns(10)
		conn.SetMaxIdleConns(5)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}
	return &Store{db: conn, driver: driver}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate() error {
	return runMigrations(s.db, s.driver)
}

var pgPlaceholder = regexp.MustCompile(`\$\d+`)

// sql converts PostgreSQL-style $N placeholders to the driver's native format.
// For PostgreSQL: keeps $N as-is. For SQLite: converts to ?.
func (s *Store) sql(query string) string {
	if s.driver == "postgres" {
		return query
	}
	return pgPlaceholder.ReplaceAllString(query, "?")
}
