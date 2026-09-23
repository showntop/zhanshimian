package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

// 真库集成测试（TEST_DATABASE_URL）：幂等键、越权 404、公共兜底池、生命周期。

func dailyTestUser(t *testing.T, store *Store, nickname string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES($1) RETURNING id::text`, nickname).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func TestSaveDailyContentIsIdempotentPerDay(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	userID := dailyTestUser(t, store, "daily-idempotent")

	first, err := store.SaveContent(ctx, dailyContentFixture(userID, "2026-09-20", "gen:2026-09-20:color"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	second, err := store.SaveContent(ctx, dailyContentFixture(userID, "2026-09-20", "gen:2026-09-20:fit"))
	if err != nil {
		t.Fatalf("save again: %v", err)
	}
	if first.ID != second.ID || second.Category != first.Category {
		t.Fatalf("idempotency broken: %s vs %s", first.ID, second.ID)
	}
	// 跨天不算重复。
	nextDay, err := store.SaveContent(ctx, dailyContentFixture(userID, "2026-09-21", "gen:2026-09-21:color"))
	if err != nil {
		t.Fatalf("save next day: %v", err)
	}
	if nextDay.ID == first.ID {
		t.Fatal("next day should be a new row")
	}
}

func dailyContentFixture(userID, genDate, dedupeKey string) domain.DailyContent {
	return domain.DailyContent{
		UserID: userID, GenDate: genDate, Category: "color",
		Topic: "冬天的白", Lead: "导语", FitText: "适配", Why: "原理",
		Visual:  domain.ContentVisual{Modality: "swatch", Spec: map[string]any{"items": []any{}}, Alt: "色卡"},
		FactIDs: []string{}, Source: "generated", DedupeKey: dedupeKey,
	}
}

func TestContentByIDAllowsOwnAndPoolRowsOnly(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	userA := dailyTestUser(t, store, "daily-a")
	userB := dailyTestUser(t, store, "daily-b")

	own, err := store.SaveContent(ctx, dailyContentFixture(userA, "2026-09-20", "gen:a"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dailyPoolRow(t, store, "color.white.tone")
	other, err := store.SaveContent(ctx, dailyContentFixture(userB, "2026-09-20", "gen:b"))
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{own.ID, pool.ID} {
		if _, err := store.ContentByID(ctx, userA, id); err != nil {
			t.Fatalf("user A should read %s: %v", id, err)
		}
	}
	if _, err := store.ContentByID(ctx, userA, other.ID); err != repository.ErrNotFound {
		t.Fatalf("cross-user read should be 404, got %v", err)
	}
}

func dailyPoolRow(t *testing.T, store *Store, dedupeKey string) domain.DailyContent {
	t.Helper()
	ctx := context.Background()
	visual, err := json.Marshal(domain.ContentVisual{Modality: "swatch", Spec: map[string]any{}, Alt: "兜底"})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO daily_content(user_id, gen_date, category, topic, lead, fit_text, why, visual, fact_ids, source, model_key, dedupe_key)
		VALUES (NULL, $1::date, $2, $3, $4, $5, $6, $7, '{}', 'fallback', '', $8)
		RETURNING id::text`,
		"1970-01-01", "color", "兜底", "兜底导语", "兜底适配", "兜底原理", visual, dedupeKey,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return domain.DailyContent{ID: id, DedupeKey: dedupeKey}
}

func TestDailyCollectionLifecycle(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	userID := dailyTestUser(t, store, "daily-col")

	content := dailyContentFixture(userID, "2026-09-20", "gen:2026-09-20:color")
	saved, err := store.SaveContent(ctx, content)
	if err != nil {
		t.Fatal(err)
	}

	item := domain.DailyCollection{
		UserID: userID, ContentID: saved.ID, ContentKey: saved.DedupeKey,
		Snapshot: domain.ContentSnapshot{
			ID: saved.ID, Type: "color", Topic: saved.Topic, Lead: saved.Lead,
			FitText: saved.FitText, Why: saved.Why, Visual: saved.Visual, Category: "color",
		},
		Title: saved.Topic, Summary: saved.Lead, Category: "color",
		Assets:  []domain.CollectionAsset{{Kind: "colors", Colors: []string{"#FBFBF8"}}},
		Context: domain.SavedContext{Season: "autumn"},
		Gene:    map[string]string{"heightBand": "petite"},
		Status:  domain.CollectionStatusSaved,
	}
	created, err := store.CreateCollection(ctx, item)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.Snapshot.Topic != saved.Topic || len(created.Assets) != 1 {
		t.Fatalf("created = %#v", created)
	}
	// 幂等：同 content_key 再收下返回同一条。
	again, err := store.CreateCollection(ctx, item)
	if err != nil {
		t.Fatalf("create again: %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("collection idempotency broken: %s vs %s", created.ID, again.ID)
	}

	updated, err := store.UpdateCollection(ctx, userID, created.ID, domain.CollectionStatusKept, "真有用")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != domain.CollectionStatusKept || updated.Note != "真有用" {
		t.Fatalf("updated = %#v", updated)
	}

	counts, err := store.CountCollections(ctx, userID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if counts["color"] != 1 {
		t.Fatalf("counts = %#v", counts)
	}

	listed, err := store.ListCollections(ctx, userID, "color", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Snapshot.Category != "color" {
		t.Fatalf("listed = %#v", listed)
	}

	// 越权：别人看不到。
	other := dailyTestUser(t, store, "daily-col-2")
	if _, err := store.CollectionByID(ctx, other, created.ID); err != repository.ErrNotFound {
		t.Fatalf("cross-user read = %v", err)
	}
	if err := store.DeleteCollection(ctx, userID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteCollection(ctx, userID, created.ID); err != repository.ErrNotFound {
		t.Fatalf("second delete = %v", err)
	}
}

func TestListReviewedFactsExcludesSeenAndFiltersSeason(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	dailySeedFact(t, store, "color", "事实一", domain.GeneCondition{}, []string{"autumn"})
	dailySeedFact(t, store, "fit", "事实二", domain.GeneCondition{}, []string{"summer"})
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO knowledge_fact(domain, fact, boundary, gene_fit, season, source, reviewed)
		VALUES ('color', '未确认事实', '边界', '{}'::jsonb, '{}', '测试', false)`); err != nil {
		t.Fatal(err)
	}

	// 种子迁移也写入了一批事实：断言只看本测试种的这批（按 fact 文本过滤）。
	all, err := store.ListReviewedFacts(ctx, nil, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byText := map[string]domain.KnowledgeFact{}
	for _, fact := range all {
		byText[fact.Fact] = fact
	}
	if _, ok := byText["未确认事实"]; ok {
		t.Fatal("unreviewed fact leaked")
	}
	first, ok := byText["事实一"]
	second, ok2 := byText["事实二"]
	if !ok || !ok2 {
		t.Fatalf("seeded facts missing: %#v", byText)
	}

	autumn, err := store.ListReviewedFacts(ctx, nil, "autumn")
	if err != nil {
		t.Fatalf("list autumn: %v", err)
	}
	foundAutumn := false
	for _, fact := range autumn {
		switch fact.Fact {
		case "事实一":
			foundAutumn = true
		case "事实二":
			t.Fatalf("summer fact leaked into autumn: %#v", autumn)
		}
	}
	if !foundAutumn {
		t.Fatalf("autumn fact missing: %#v", autumn)
	}

	excluded, err := store.ListReviewedFacts(ctx, []string{first.ID, second.ID}, "")
	if err != nil {
		t.Fatalf("list exclude: %v", err)
	}
	for _, fact := range excluded {
		if fact.ID == first.ID || fact.ID == second.ID {
			t.Fatalf("excluded fact leaked: %#v", fact)
		}
	}
}

func dailySeedFact(t *testing.T, store *Store, domainName, factText string, geneFit domain.GeneCondition, season []string) {
	t.Helper()
	ctx := context.Background()
	gene, err := json.Marshal(geneFit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO knowledge_fact(domain, fact, boundary, gene_fit, season, source, reviewed)
		VALUES ($1, $2, '边界', $3::jsonb, $4, '测试', true)`,
		domainName, factText, gene, season); err != nil {
		t.Fatal(err)
	}
}

func TestRecentContentsAndContentKeys(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	userID := dailyTestUser(t, store, "daily-recent")

	if _, err := store.SaveContent(ctx, dailyContentFixture(userID, "2026-09-19", "gen:old")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveContent(ctx, dailyContentFixture(userID, "2026-09-20", "gen:recent")); err != nil {
		t.Fatal(err)
	}

	contents, err := store.RecentContents(ctx, userID, time.Now().AddDate(0, 0, -30), 10)
	if err != nil {
		t.Fatalf("recent contents: %v", err)
	}
	if len(contents) != 2 || contents[0].DedupeKey != "gen:recent" || contents[1].DedupeKey != "gen:old" {
		t.Fatalf("contents = %#v (want 新到旧)", contents)
	}
	limited, err := store.RecentContents(ctx, userID, time.Now().AddDate(0, 0, -30), 1)
	if err != nil {
		t.Fatalf("recent contents limited: %v", err)
	}
	if len(limited) != 1 || limited[0].DedupeKey != "gen:recent" {
		t.Fatalf("limited = %#v", limited)
	}
	keys, err := store.RecentContentKeys(ctx, userID, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("recent keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %#v", keys)
	}
}

// grounding：无资料行返回零值而不是错误（生成语境用中性基因继续）。
func TestReadDailyGroundingWithoutProfile(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	userID := dailyTestUser(t, store, "daily-grounding")

	grounding, err := store.ReadDailyGrounding(ctx, userID)
	if err != nil {
		t.Fatalf("grounding: %v", err)
	}
	if grounding.HeightCM != 0 || grounding.WeightKG != nil {
		t.Fatalf("grounding = %#v", grounding)
	}

	if _, err := store.pool.Exec(ctx, `
		INSERT INTO user_profiles(user_id, height_cm, preferences)
		VALUES ($1::uuid, 155, '{"weight_kg":45}'::jsonb)`, userID); err != nil {
		t.Fatal(err)
	}
	grounding, err = store.ReadDailyGrounding(ctx, userID)
	if err != nil {
		t.Fatalf("grounding: %v", err)
	}
	if grounding.HeightCM != 155 || grounding.WeightKG == nil || *grounding.WeightKG != 45 {
		t.Fatalf("grounding = %#v", grounding)
	}
}
