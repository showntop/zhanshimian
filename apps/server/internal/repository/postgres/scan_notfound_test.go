package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

// 单行扫描必须把 pgx.ErrNoRows 映射为 repository.ErrNotFound —— handler 据此
// 回 404/契约空态,裸 pgx 错误一律 500。第 14 轮全量 E2E 实测:分享撤销后公开
// 读取应 404,实际 500("no rows in result set");audit 发现 scanShare/
// scanDiagnostic/scanHairPreview 三处同类缺口(scanTodayPlan 为 item 24 同款)。
func TestSingleRowScansMapNoRowsToNotFound(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('scan-404') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	missing := "00000000-0000-0000-0000-000000000000"

	if _, err := store.GetPublicShareByToken(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown share token must map to ErrNotFound, got %v", err)
	}
	if _, err := store.GetDiagnosticByID(ctx, userID, missing); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing diagnostic must map to ErrNotFound, got %v", err)
	}
	if _, err := store.GetLatestDiagnosticByKind(ctx, userID, "outfit"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("empty diagnostic kind must map to ErrNotFound, got %v", err)
	}
	if _, err := store.GetHairPreviewRow(ctx, userID, missing); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing hair preview must map to ErrNotFound, got %v", err)
	}
}
