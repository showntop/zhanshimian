package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/testutil"
)

// 注销清除必须真跑通——既有覆盖只是字符串静态比对,第 16 轮全量 E2E 前的
// 实跑审计发现 deleteUserDataQueries 引用被冻结清单禁止的 analyses 与
// baseline 漏折的 product_events,真执行 DELETE /v1/me/data 会 42P01 500。
// 同时钉住:sms_codes/billing_usage 也在清除面内(手机号是 PII,用量计数是
// 用户业务数据),且都在冻结表清单里。
func TestDeleteUserDataExecutesAgainstBaseline(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('erase-me') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	phone := "13800000002"
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO user_identities(user_id, provider, identifier) VALUES($1::uuid, 'phone', $2)`, userID, phone); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSmsCode(ctx, phone, []byte("digest"), time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO billing_usage(user_id, bucket, action, count) VALUES($1::uuid, $2, 'analysis', 1)`,
		userID, time.Now().Truncate(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.TrackProductEventRow(ctx, userID, domain.ProductEventInput{Name: "erase_probe"}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.DeleteUserData(ctx, userID); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}

	count := func(query string, args ...any) int {
		var n int
		if err := store.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM users WHERE id=$1`, userID); n != 0 {
		t.Fatalf("users rows remain after erase: %d", n)
	}
	if n := count(`SELECT count(*) FROM sms_codes WHERE phone=$1`, phone); n != 0 {
		t.Fatalf("sms_codes rows remain after erase: %d", n)
	}
	if n := count(`SELECT count(*) FROM billing_usage WHERE user_id=$1`, userID); n != 0 {
		t.Fatalf("billing_usage rows remain after erase: %d", n)
	}
}

// 每日免费额度(daily_remaining)是冻结 OpenAPI BillingSummary 的必填字段,
// 计数表 billing_usage 必须在 baseline 里——第 16 轮 E2E 的 GET /v1/billing/me
// 依赖它(既存货表清单测试误把它列进 legacy 禁止名单,与契约冲突)。
func TestBillingUsageRoundTrip(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('usage') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, delta := range []int{1, 2} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO billing_usage(user_id, bucket, action, count) VALUES($1::uuid, $2, 'analysis', $3)
			ON CONFLICT (user_id, bucket, action) DO UPDATE SET count=billing_usage.count+$3`,
			userID, now.Truncate(24*time.Hour), delta); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	day, hour, minute := now.Truncate(24*time.Hour), now.Truncate(time.Hour), now.Truncate(time.Minute)
	analysis, _, _, _, _, _, err := store.GetBillingUsage(ctx, userID, day, hour, minute)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if analysis != 3 {
		t.Fatalf("analysis = %d, want 3", analysis)
	}
}
