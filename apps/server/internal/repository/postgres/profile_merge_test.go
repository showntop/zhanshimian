package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/testutil"
)

// /v1/me/profile 全量保存把体重/三围打进 preferences 补丁：仓储必须合并进
// preferences JSONB —— 未提供的测量键删除，feedback_memory 等其他键保留；
// GetUserProfile 必须把 preferences 带回来供 GET 投影。第 15 轮 E2E 前实测：
// 旧实现不写不读 preferences，测量项落库即丢。
func TestSaveUserProfileMergesMeasurementsPreservesOtherKeys(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('profile-merge') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	// 预置：评估链路的建档回填行 + 反馈记忆 + 旧测量值。
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO user_profiles(user_id, preferences)
		VALUES($1::uuid, '{"feedback_memory":[{"key":"x"}],"weight_kg":60}'::jsonb)`, userID); err != nil {
		t.Fatal(err)
	}

	saved, err := store.SaveUserProfile(ctx, userID, domain.UserProfile{
		Role: "产品经理", HeightCM: 165, Budget: "500-1500",
		Preferences: json.RawMessage(`{"bust_cm":88}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProfilePreferences(t, saved.Preferences)

	got, err := store.GetUserProfile(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "产品经理" || got.HeightCM != 165 || got.Budget != "500-1500" {
		t.Fatalf("profile = %+v", got)
	}
	assertProfilePreferences(t, got.Preferences)
}

func assertProfilePreferences(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &prefs); err != nil {
		t.Fatalf("preferences: %v raw=%s", err, raw)
	}
	if _, ok := prefs["feedback_memory"]; !ok {
		t.Fatalf("feedback_memory must be preserved, prefs=%s", raw)
	}
	var bust float64
	if err := json.Unmarshal(prefs["bust_cm"], &bust); err != nil || bust != 88 {
		t.Fatalf("bust_cm = %s, want 88", prefs["bust_cm"])
	}
	if _, ok := prefs["weight_kg"]; ok {
		t.Fatalf("unprovided measurement key must be dropped, prefs=%s", raw)
	}
}
