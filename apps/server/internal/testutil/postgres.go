package testutil

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/database"
)

func NewPostgres(t testing.TB) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	admin, err := pgxpool.New(context.Background(), adminURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	dbName := "zsm_test_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	if _, err = admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		admin.Close()
		t.Fatalf("create test database %s: %v", dbName, err)
	}
	testURL := withDatabase(t, adminURL, dbName)
	pool, err := database.Open(context.Background(), testURL)
	if err != nil {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Migrate(context.Background(), pool); err != nil {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
		t.Fatalf("migrate test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(),
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1`, dbName)
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
	})
	return pool
}

func withDatabase(t testing.TB, adminURL, dbName string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	parsed.Path = "/" + dbName
	return parsed.String()
}
