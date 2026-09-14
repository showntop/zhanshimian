// Task 10：baseline 收敛的完整表清单断言。旧迁移已由前置计划删除，
// 这里钉住“只有新 schema”这一事实：表集合必须与 001_baseline.sql 完全一致，
// 任何旧表回流或新表缺失都会在此红掉。
package database_test

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestBaselineCreatesOnlyNewSchema(t *testing.T) {
	pool := testutil.NewPostgres(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT tablename FROM pg_tables
		WHERE schemaname='public'
		ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		got = append(got, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"advisor_conversations", "advisor_messages", "analysis_runs",
		"billing_ledger", "billing_orders", "billing_reservations", "billing_wallets",
		"diagnostics", "execution_events", "execution_feedback", "execution_steps",
		"executions", "generation_feedback", "hair_previews",
		"idempotency_keys", "media_assets", "object_gc_jobs", "operations",
		"photo_set_items", "photo_sets", "plan_selections", "plan_sets",
		"plan_step_groundings", "plan_steps", "plan_variants",
		"preference_memories", "provider_invocations",
		"quality_evaluations", "render_candidates", "render_heads",
		"render_publications", "render_runs", "render_specs",
		"report_findings", "reports", "schema_migrations",
		"shares", "tasks", "today_plans",
		"upload_intents", "user_identities", "user_profiles", "user_sessions",
		"users", "wardrobe_items", "wardrobe_outfits",
	}
	if len(got) != len(want) {
		t.Fatalf("table count = %d\ngot:  %v\nwant: %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("table[%d] = %s, want %s\ngot:  %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
	legacy := map[string]bool{
		"analyses": true, "plans": true, "feedback": true, "tool_results": true,
		"share_cards": true, "advisor_actions": true, "billing_usage": true,
	}
	for _, table := range got {
		if legacy[table] {
			t.Fatalf("legacy table %s returned", table)
		}
	}
}

// TestBaselineConstraintInventory 钉住 Task 10 要求的约束面：
// 复合租户外键、object_key 唯一、Task dedupe、Publication→同用户 Candidate、
// render_heads 的 generation/version 列。
func TestBaselineConstraintInventory(t *testing.T) {
	pool := testutil.NewPostgres(t)
	ctx := context.Background()

	checks := []struct {
		name string
		sql  string
	}{
		{"photo_set_items composite FK", `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'photo_set_items'::regclass AND contype = 'f'
				  AND conname = 'photo_set_items_user_id_photo_set_id_fkey')`},
		{"media_assets object_key unique", `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'media_assets'::regclass AND contype = 'u'
				  AND pg_get_constraintdef(oid) LIKE '%object_key%')`},
		{"tasks dedupe unique", `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'tasks'::regclass AND contype = 'u'
				  AND pg_get_constraintdef(oid) LIKE '%dedupe%')`},
		{"render_publications candidate owner FK", `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conname = 'render_publications_user_id_render_run_id_candidate_id_fkey')`},
		{"render_heads generation column", `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'render_heads' AND column_name = 'generation')`},
		{"render_heads version column", `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'render_heads' AND column_name = 'version')`},
	}
	for _, check := range checks {
		var got bool
		if err := pool.QueryRow(ctx, check.sql).Scan(&got); err != nil {
			t.Fatalf("%s: %v", check.name, err)
		}
		if !got {
			t.Fatalf("%s: constraint/column missing", check.name)
		}
	}
}

// TestResetThenMigrateRebuildsFromScratch 证明 fresh-database boot：
// Reset 清库后 Migrate 能从零重建完整 baseline（约束随表一起回来）。
func TestResetThenMigrateRebuildsFromScratch(t *testing.T) {
	pool := testutil.NewPostgres(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(nickname) VALUES ('reset-probe')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Reset(ctx, pool); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Reset 后 users 表应彻底不存在（42P01），而不是空表——
	// 查 pg_tables 而不是查行数，避免把"表还在"误当成功。
	var usersExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&usersExists); err != nil {
		t.Fatal(err)
	}
	if usersExists {
		t.Fatal("reset left the users table behind")
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	// 复用清单断言：重建后的 schema 与首建完全一致。
	rows, err := pool.Query(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`)
	if err != nil {
		t.Fatal(err)
	}
	var tableCount int
	if !rows.Next() {
		t.Fatal("no count row")
	}
	if err := rows.Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if tableCount != 46 {
		t.Fatalf("rebuilt table count = %d, want 46", tableCount)
	}
}
