//go:build integration

// Package testdb gives each integration test its own throwaway, fully migrated database.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// driver
	_ "github.com/golang-migrate/migrate/v4/source/file"     // file:// source
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsDir is the absolute path of the repo's migrations folder.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}

func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_POSTGRES_URL")
	if u == "" {
		t.Fatal("TEST_POSTGRES_URL is not set; integration tests need a Postgres 18 server")
	}
	return u
}

// withDatabase returns rawURL with its database name replaced.
func withDatabase(t *testing.T, rawURL, name string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse TEST_POSTGRES_URL: %v", err)
	}
	u.Path = "/" + name
	return u
}

func newMigrate(t *testing.T, dbURL *url.URL) *migrate.Migrate {
	t.Helper()
	u := *dbURL
	u.Scheme = "pgx5"
	m, err := migrate.New("file://"+filepath.ToSlash(migrationsDir()), u.String())
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	return m
}

// New creates database test_<random>, applies every up migration and returns a pool on it.
// The pool is closed and the database dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin := adminURL(t)

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "test_" + hex.EncodeToString(b)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect to TEST_POSTGRES_URL: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("create %s: %v", name, err)
	}
	_ = conn.Close(ctx)

	dbURL := withDatabase(t, admin, name)
	pool, err := pgxpool.New(ctx, dbURL.String())
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			t.Logf("drop %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(),
			"DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})

	m := newMigrate(t, dbURL)
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return pool
}

// MigrateDown runs every down migration on the database behind pool.
func MigrateDown(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	cfg := pool.Config().ConnConfig
	dbURL := withDatabase(t, adminURL(t), cfg.Database)
	m := newMigrate(t, dbURL)
	defer func() { _, _ = m.Close() }()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate down: %v", err)
	}
}
