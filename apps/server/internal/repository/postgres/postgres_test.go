package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsForeignKeyViolation(t *testing.T) {
	fk := &pgconn.PgError{Code: "23503", ConstraintName: "reports_analysis_id_fkey"}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "matching constraint", err: fk, want: true},
		{name: "wrapped", err: fmt.Errorf("insert report: %w", fk), want: true},
		{name: "other constraint", err: &pgconn.PgError{Code: "23503", ConstraintName: "plans_report_id_fkey"}, want: false},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505", ConstraintName: "reports_analysis_id_fkey"}, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isForeignKeyViolation(test.err, "reports_analysis_id_fkey"); got != test.want {
				t.Fatalf("isForeignKeyViolation(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

// 注销账号必须清掉发型预览、反馈和会话，并最后删除 users 行——迁移里的
// ON DELETE CASCADE 只在 users 被删除时触发，漏删 users 会让旧 token 继续有效。
func TestDeleteUserDataCoversAllUserTables(t *testing.T) {
	// 注销账号必须清掉全部业务行，并最后删除 users 行——迁移里的
	// ON DELETE CASCADE 只在 users 被删除时触发，漏删 users 会让旧 token 继续有效。
	// 表集合与 001_baseline.sql 对齐。
	for _, table := range []string{
		"billing_ledger", "billing_reservations", "billing_orders", "billing_wallets",
		"billing_usage", "sms_codes",
		"generation_feedback", "execution_feedback", "preference_memories",
		"execution_events", "execution_steps", "executions", "plan_selections",
		"shares", "today_plans", "wardrobe_outfits", "wardrobe_items",
		"advisor_messages", "advisor_conversations", "diagnostics",
		"hair_previews", "product_events", "operations", "analysis_runs",
		"reports", "photo_sets", "plan_sets", "render_runs",
		"media_assets", "user_identities", "user_profiles", "user_sessions", "users",
		"body_presentations",
	} {
		found := false
		for _, query := range deleteUserDataQueries {
			if strings.Contains(query, "DELETE FROM "+table+" ") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("DeleteUserData misses table %s", table)
		}
	}
	last := deleteUserDataQueries[len(deleteUserDataQueries)-1]
	if !strings.Contains(last, "DELETE FROM users ") {
		t.Fatalf("users must be deleted last so sessions are purged before the row disappears: %q", last)
	}
}
