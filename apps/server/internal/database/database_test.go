package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestBaselineCreatesQualityCoreSchema(t *testing.T) {
	pool := testutil.NewPostgres(t)
	expected := []string{
		"users", "user_identities", "user_sessions", "user_profiles",
		"media_assets", "upload_intents", "photo_sets", "photo_set_items",
		"operations", "tasks", "provider_invocations", "idempotency_keys",
		"analysis_runs", "reports", "report_findings",
		"plan_sets", "plan_variants", "plan_steps", "plan_step_groundings",
		"render_specs", "render_heads", "render_runs", "render_candidates",
		"quality_evaluations", "render_publications",
		"plan_selections", "executions", "execution_steps", "execution_events",
		"generation_feedback", "execution_feedback", "object_gc_jobs",
		"billing_wallets", "billing_reservations", "billing_ledger",
	}
	for _, table := range expected {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("missing table %s", table)
		}
	}
}

func TestBaselineRejectsCrossUserMediaAttachment(t *testing.T) {
	pool := testutil.NewPostgres(t)
	userA := insertUser(t, pool)
	userB := insertUser(t, pool)
	asset := insertMedia(t, pool, userA, "user_upload", "face")
	photoSet := insertPhotoSet(t, pool, userB)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO photo_set_items(user_id,photo_set_id,role,media_asset_id)
		VALUES($1,$2,'face',$3)`, userB, photoSet, asset)
	if err == nil {
		t.Fatal("expected cross-user media attachment to fail")
	}
	pgErr, ok := err.(*pgconn.PgError)
	if !ok {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != "23503" {
		t.Fatalf("expected SQLSTATE 23503, got %s", pgErr.Code)
	}
}

func TestBaselineMigrateIsIdempotent(t *testing.T) {
	pool := testutil.NewPostgres(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM schema_migrations WHERE version=$1`, "001_baseline.sql").Scan(&n)
	if err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one schema_migrations row for 001_baseline.sql, got %d", n)
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func insertMedia(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, origin, purpose string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	objectKey := "users/" + userID.String() + "/uploads/" + uuid.NewString()
	sha := strings.Repeat("ab", 32)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(
			user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind
		) VALUES ($1,$2,$3,$4,$5,'image/jpeg',128,'ready','original')
		RETURNING id`, userID, origin, purpose, objectKey, sha).Scan(&id)
	if err != nil {
		t.Fatalf("insert media: %v", err)
	}
	return id
}

func insertPhotoSet(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO photo_sets(user_id, profile_snapshot, content_hash, schema_version)
		VALUES ($1,'{}'::jsonb,$2,'v1')
		RETURNING id`, userID, strings.Repeat("cd", 32)).Scan(&id)
	if err != nil {
		t.Fatalf("insert photo set: %v", err)
	}
	return id
}
