package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

// 短信验证码建表必须落在 baseline——第 16 轮全量 E2E 实测:POST
// /v1/auth/sms/request 500(42P01 relation "sms_codes" does not exist),
// 重建基线折表时漏折了 legacy 014_sms_codes.sql。登录回归链(SMS 请求→
// dev_code→校验→会话)依赖这张表,钉住整个往返。
func TestSmsCodeRoundTrip(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	phone := "13800000001"
	digest := []byte("digest-1")

	if err := store.CreateSmsCode(ctx, phone, digest, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("create: %v", err)
	}
	latest, err := store.LatestSmsCode(ctx, phone)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Phone != phone || string(latest.Digest) != string(digest) || latest.UsedAt != nil {
		t.Fatalf("latest = %+v", latest)
	}
	count, err := store.CountSmsCodesSince(ctx, phone, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if err := store.ConsumeSmsCode(ctx, latest.ID); err != nil {
		t.Fatalf("consume: %v", err)
	}
	// 已消费的不得再次消费。
	if err := store.ConsumeSmsCode(ctx, latest.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("re-consume must be ErrNotFound, got %v", err)
	}
}
