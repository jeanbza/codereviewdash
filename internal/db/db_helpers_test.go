package db_test

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"

	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func postgresDetails() (username string, password string, host string, port uint16, dbname string, _ error) {
	username = os.Getenv("POSTGRES_USERNAME")
	if username == "" {
		return "", "", "", 0, "", fmt.Errorf("POSTGRES_USERNAME is not set. Must set POSTGRES_USERNAME, POSTGRES_HOST, POSTGRES_PORT, and POSTGRES_DB (POSTGRES_PASSWORD is optional)")
	}
	password = os.Getenv("POSTGRES_PASSWORD")
	// Note: password can be empty (e.g., macOS Homebrew PostgreSQL doesn't require a password)
	host = os.Getenv("POSTGRES_HOST")
	if host == "" {
		return "", "", "", 0, "", fmt.Errorf("POSTGRES_HOST is not set. Must set POSTGRES_USERNAME, POSTGRES_HOST, POSTGRES_PORT, and POSTGRES_DB (POSTGRES_PASSWORD is optional)")
	}
	portStr := os.Getenv("POSTGRES_PORT")
	if portStr == "" {
		return "", "", "", 0, "", fmt.Errorf("POSTGRES_PORT is not set. Must set POSTGRES_USERNAME, POSTGRES_HOST, POSTGRES_PORT, and POSTGRES_DB (POSTGRES_PASSWORD is optional)")
	}
	portUint64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", "", "", 0, "", fmt.Errorf("POSTGRES_PORT is invalid: %v", err)
	}
	dbname = os.Getenv("POSTGRES_DB")
	if dbname == "" {
		return "", "", "", 0, "", fmt.Errorf("POSTGRES_DB is not set. Must set POSTGRES_USERNAME, POSTGRES_HOST, POSTGRES_PORT, and POSTGRES_DB (POSTGRES_PASSWORD is optional)")
	}

	return username, password, host, uint16(portUint64), dbname, nil
}

// Drops tables and re-runs migrations.
func resetTables(t *testing.T, db *sql.DB) {
	if _, err := db.ExecContext(t.Context(), `
		DROP TABLE IF EXISTS pr_reviewers;
		DROP TABLE IF EXISTS repo_commits;
		DROP TABLE IF EXISTS repo_prs;
		DROP TABLE IF EXISTS repos;
		DROP TABLE IF EXISTS repo_indexing;
		DROP TABLE IF EXISTS team_members;
		DROP TABLE IF EXISTS teams;
		DROP TABLE IF EXISTS users;
		DROP TABLE IF EXISTS teams_reindexing;
		DROP TABLE IF EXISTS schema_migrations;
	`); err != nil {
		t.Fatalf("resetTables: error dropping repo_tags table: %v", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		t.Fatalf("resetTables: error creating postgres driver: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", "postgres", driver)
	if err != nil {
		t.Fatalf("resetTables: error creating database migrator: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("resetTables: error running up: %v", err)
	}
}

func setAllReposIndexing(t *testing.T, db *sql.DB, indexingBegan, indexingFinished time.Time) {
	t.Helper()

	query := fmt.Sprintf(`
UPDATE repo_indexing
SET indexing_began = TIMESTAMP WITH TIME ZONE '%s', indexing_finished = TIMESTAMP WITH TIME ZONE '%s'`,
		indexingBegan.Format(time.RFC3339), indexingFinished.Format(time.RFC3339))

	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatalf("setAllReposIndexing: error updating repo_indexing table:\nquery: %s\nerror: %v", query, err)
	}
}
