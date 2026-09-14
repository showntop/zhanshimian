package postgres

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/testutil"
)

// baseline 的 users 只有 id/nickname/created_at——身份唯一性全部由
// user_identities(provider,identifier) 承担。旧实现还在写 users.open_id，
// 在 baseline 上直接 42703；这些测试钉住新 schema 下的登录写入路径。
func TestEnsureUserByIdentityCreatesUserWithoutOpenID(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()

	user, err := store.EnsureUserByIdentity(ctx, domain.ProviderPhone, "13800000001", "首登")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if user.ID == "" || user.Nickname != "首登" {
		t.Fatalf("unexpected user: %#v", user)
	}

	// 同身份再登：同一用户，昵称不被入参覆盖。
	again, err := store.EnsureUserByIdentity(ctx, domain.ProviderPhone, "13800000001", "改名尝试")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if again.ID != user.ID || again.Nickname != "首登" {
		t.Fatalf("identity rebound: got %#v want id=%s nickname=首登", again, user.ID)
	}

	identities, err := store.ListIdentities(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 1 || identities[0].Provider != domain.ProviderPhone || identities[0].Identifier != "13800000001" {
		t.Fatalf("identities = %#v", identities)
	}
}

func TestEnsureUserByIdentitySeparatesProviders(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()

	phoneUser, err := store.EnsureUserByIdentity(ctx, domain.ProviderPhone, "same-id", "")
	if err != nil {
		t.Fatal(err)
	}
	wechatUser, err := store.EnsureUserByIdentity(ctx, domain.ProviderWeChatMiniApp, "same-id", "")
	if err != nil {
		t.Fatal(err)
	}
	if phoneUser.ID == wechatUser.ID {
		t.Fatal("different providers must not share a user")
	}
}

func TestCreateDevUserWorksOnBaseline(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	user, err := store.CreateDevUser(context.Background(), "开发登录")
	if err != nil {
		t.Fatalf("dev login insert: %v", err)
	}
	if user.ID == "" || user.Nickname != "开发登录" {
		t.Fatalf("unexpected dev user: %#v", user)
	}
}
