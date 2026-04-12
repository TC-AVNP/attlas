// Package db owns the SQLite connection and embedded migrations.
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
// migrations, and — if the steps table is still empty afterwards —
// loads the bootstrap seed.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		path,
	)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
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

func maybeSeed(conn *sql.DB) error {
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM steps`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("db: empty steps table — loading bootstrap seed")
	if _, err := conn.Exec(seedSQL); err != nil {
		return fmt.Errorf("exec seed: %w", err)
	}
	return nil
}
