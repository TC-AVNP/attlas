// Package db owns the SQLite connection, embedded migrations, and the
// one-shot bootstrap seed. Everything that talks to the database goes
// through the *sql.DB exposed by Open.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed seed.sql
var seedSQL string

// Open creates the SQLite connection at path, runs all pending schema
// migrations, and — if the projects table is still empty afterwards —
// loads the bootstrap seed. Returns the open *sql.DB on success; the
// caller owns closing it.
//
// Connection string flags: WAL journaling for concurrent reads during
// SSE fan-out, foreign keys ON (they're off by default in SQLite), and
// busy_timeout so a long write doesn't blow up concurrent readers.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		path,
	)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// The modernc driver is fine with multiple connections but WAL
	// plus a single-user workload means we don't need a huge pool.
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := maybeSeed(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed: %w", err)
	}

	return conn, nil
}

// migrate applies every SQL file under migrations/ whose numeric prefix
// is greater than the highest applied version recorded in schema_version.
// The schema_version table is created on first run if it doesn't exist.
func migrate(conn *sql.DB) error {
	_, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	)`)
	if err != nil {
		return err
	}

	var current int
	if err := conn.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`,
	).Scan(&current); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := parseVersion(name)
		if version == 0 {
			log.Printf("db: skipping unparseable migration %q", name)
			continue
		}
		if version <= current {
			continue
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		log.Printf("db: applying migration %s", name)
		tx, err := conn.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_version(version) VALUES (?)`, version,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// parseVersion pulls the numeric prefix off a migration filename
// ("0001_init.sql" → 1). Returns 0 if the name doesn't start with digits.
func parseVersion(name string) int {
	var v int
	for _, ch := range name {
		if ch < '0' || ch > '9' {
			break
		}
		v = v*10 + int(ch-'0')
	}
	return v
}

// maybeSeed runs seed.sql if and only if the projects table is empty.
// This gives petboard its origin story — on a fresh install the very
// first project that exists is petboard itself.
func maybeSeed(conn *sql.DB) error {
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("db: empty projects table — loading bootstrap seed")
	if _, err := conn.Exec(seedSQL); err != nil {
		return fmt.Errorf("exec seed: %w", err)
	}
	return nil
}
