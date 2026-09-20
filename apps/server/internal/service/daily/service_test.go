package daily_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/daily"
)

// ---- fakes（编译期钉住接口签名，与 service/today 的测试同一做法） ----

type fakeReader struct{ grounding daily.Grounding }

func (f fakeReader) ReadDailyGrounding(context.Context, string) (daily.Grounding, error) {
	return f.grounding, nil
}

type fakeKnowledge struct {
	facts []domain.KnowledgeFact
}

func (f fakeKnowledge) ListReviewedFacts(_ context.Context, excludeIDs []string, season string) ([]domain.KnowledgeFact, error) {
	excluded := map[string]bool{}
	for _, id := range excludeIDs {
		excluded[id] = true
	}
	out := []domain.KnowledgeFact{}
	for _, fact := range f.facts {
		if excluded[fact.ID] {
			continue
		}
		if season != "" && len(fact.Season) > 0 && !containsString(fact.Season, season) {
			continue
		}
		out = append(out, fact)
	}
	return out, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type fakeContent struct {
	mu          sync.Mutex
	today       map[string]domain.DailyContent
	pool        []domain.DailyContent
	factIDs     []string
	contentKeys []string
	saved       []domain.DailyContent
}

func newFakeContent(pool []domain.DailyContent) *fakeContent {
	return &fakeContent{today: map[string]domain.DailyContent{}, pool: pool}
}

func (f *fakeContent) TodayContent(_ context.Context, userID string, genDate string) (domain.DailyContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if content, ok := f.today[userID+"|"+genDate]; ok {
		return content, nil
	}
	return domain.DailyContent{}, repository.ErrNotFound
}

func (f *fakeContent) ContentByID(_ context.Context, userID string, id string) (domain.DailyContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, content := range f.saved {
		if content.ID == id && (content.UserID == userID) {
			return content, nil
		}
	}
	for _, content := range f.pool {
		if content.ID == id {
			return content, nil
		}
	}
	return domain.DailyContent{}, repository.ErrNotFound
}

func (f *fakeContent) FallbackPool(context.Context) ([]domain.DailyContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.DailyContent{}, f.pool...), nil
}

func (f *fakeContent) RecentFactIDs(context.Context, string, time.Time) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.factIDs...), nil
}

func (f *fakeContent) RecentContentKeys(context.Context, string, time.Time) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.contentKeys...), nil
}

func (f *fakeContent) SaveContent(_ context.Context, content domain.DailyContent) (domain.DailyContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if content.ID == "" {
		content.ID = fmt.Sprintf("id-%d", len(f.saved)+1)
	}
	if existing, ok := f.today[content.UserID+"|"+content.GenDate]; ok {
		return existing, nil
	}
	f.today[content.UserID+"|"+content.GenDate] = content
	f.saved = append(f.saved, content)
	return content, nil
}

type fakeRuns struct {
	mu   sync.Mutex
	runs []domain.GenerationRun
}

func (f *fakeRuns) RecordRun(_ context.Context, run domain.GenerationRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, run)
	return nil
}

type fakeCollections struct {
	mu   sync.Mutex
	next int
}

func (f *fakeCollections) CreateCollection(_ context.Context, item domain.DailyCollection) (domain.DailyCollection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	item.ID = fmt.Sprintf("col-%d", f.next)
	item.Assets = append([]domain.CollectionAsset{}, item.Assets...)
	return item, nil
}

func (f *fakeCollections) ListCollections(_ context.Context, userID string, category string, limit int) ([]domain.DailyCollection, error) {
	return nil, nil
}

func (f *fakeCollections) CollectionByID(context.Context, string, string) (domain.DailyCollection, error) {
	return domain.DailyCollection{}, repository.ErrNotFound
}

func (f *fakeCollections) UpdateCollection(_ context.Context, userID string, id string, status string, note string) (domain.DailyCollection, error) {
	return domain.DailyCollection{}, repository.ErrNotFound
}

func (f *fakeCollections) DeleteCollection(context.Context, string, string) error { return nil }

func (f *fakeCollections) CountCollections(context.Context, string) (map[string]int, error) {
	return map[string]int{}, nil
}

type fakePlanner struct {
	mu      sync.Mutex
	outputs []daily.ContentOutput
	err     error
	calls   int
}

func (f *fakePlanner) Generate(context.Context, daily.ContentRequest) (daily.ContentOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return daily.ContentOutput{}, f.err
	}
	if len(f.outputs) == 0 {
		return daily.ContentOutput{}, errors.New("no scripted output")
	}
	output := f.outputs[0]
	f.outputs = f.outputs[1:]
	return output, nil
}

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

// ---- fixtures ----

var testNow = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

func fact(id, domainName, factText string, geneFit domain.GeneCondition) domain.KnowledgeFact {
	return domain.KnowledgeFact{
		ID: id, Domain: domainName, Fact: factText, Boundary: "测试边界",
		GeneFit: geneFit, Season: []string{"autumn"}, Source: "测试", Reviewed: true,
	}
}

func goodOutput(refs ...int) daily.ContentOutput {
	return daily.ContentOutput{
		Topic: "冬天的白，不止一种",
		Lead:  "本白、米白、奶油白，上身差很多。",
		Fit:   "你是冷调肤色，本白贴着皮肤气色往上走。",
		Why:   "白色也有色温，先看冷暖再看明度。",
		Visual: daily.VisualDraft{
			Modality: "swatch", Alt: "四种白色并排",
			Items: []daily.VisualItem{
				{Label: "本白", Tone: "#FBFBF8", State: "pick"},
				{Label: "米白", Tone: "#EFEADC", State: "drop"},
			},
		},
		Refs: refs,
	}
}

func poolItem(id, category, dedupeKey string) domain.DailyContent {
	return domain.DailyContent{
		ID: id, GenDate: "1970-01-01", Category: category,
		Topic: "兜底 " + category, Lead: "兜底导语", FitText: "兜底适配", Why: "兜底原理",
		Visual:    domain.ContentVisual{Modality: "swatch", Spec: map[string]any{}, Alt: "兜底"},
		FactIDs:   []string{},
		Source:    "fallback",
		DedupeKey: dedupeKey,
	}
}

func newService(t *testing.T, reader daily.Reader, knowledge daily.KnowledgeStore, content daily.ContentStore, runs daily.RunStore, collections daily.CollectionStore, planner daily.ContentPlanner) *daily.Service {
	t.Helper()
	service := daily.New(reader, knowledge, content, runs, collections, fixedClock{now: testNow}).WithPlanner(planner)
	return service
}

// ---- prepare ----

func TestPrepareCacheHitWhenTodayContentExists(t *testing.T) {
	content := newFakeContent(nil)
	content.today["user-1|2026-09-20"] = poolItem("gen-1", "color", "gen:2026-09-20:color")
	service := newService(t, fakeReader{}, fakeKnowledge{}, content, &fakeRuns{}, &fakeCollections{}, &fakePlanner{})

	result, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !result.CacheHit || result.PickToken != "" || result.Scenario != "color" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPrepareFiltersFactsByGeneAndSeen(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{
		fact("f-small", "fit", "骨架小的事实", domain.GeneCondition{"frame": {"small"}}),
		fact("f-large", "fit", "骨架大的事实", domain.GeneCondition{"frame": {"large"}}),
		fact("f-any", "color", "不挑人的事实", nil),
	}}
	// 身高 155 + 体重 45 → petite/small。
	reader := fakeReader{grounding: daily.Grounding{HeightCM: 155, WeightKG: ptrFloat(45)}}
	content := newFakeContent(nil)
	content.factIDs = []string{"f-any"} // 近期已推过
	service := newService(t, reader, facts, content, &fakeRuns{}, &fakeCollections{}, &fakePlanner{})

	result, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if result.CacheHit {
		t.Fatalf("unexpected cache hit: %#v", result)
	}
	if result.Scenario == "" {
		t.Fatalf("scenario empty: %#v", result)
	}
}

func ptrFloat(value float64) *float64 { return &value }

// ---- generate ----

func TestGenerateSuccessPersistsContentAndRun(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent(nil)
	runs := &fakeRuns{}
	planner := &fakePlanner{outputs: []daily.ContentOutput{goodOutput(1)}}
	service := newService(t, fakeReader{}, facts, content, runs, &fakeCollections{}, planner)

	prepare, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepare.CacheHit || prepare.PickToken == "" {
		t.Fatalf("prepare = %#v", prepare)
	}
	result, err := service.Generate(context.Background(), "user-1", prepare.PickToken)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceGenerated || result.Content.Topic == "" || result.Content.DedupeKey == "" {
		t.Fatalf("result = %#v", result)
	}
	if content.today["user-1|2026-09-20"].Topic != result.Content.Topic {
		t.Fatalf("content not persisted: %#v", content.today)
	}
	if len(runs.runs) != 1 || runs.runs[0].Outcome != "accepted" {
		t.Fatalf("runs = %#v", runs.runs)
	}
}

func TestGenerateIsIdempotentPerDay(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent(nil)
	planner := &fakePlanner{outputs: []daily.ContentOutput{goodOutput(1)}}
	service := newService(t, fakeReader{}, facts, content, &fakeRuns{}, &fakeCollections{}, planner)

	first, err := service.Generate(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	second, err := service.Generate(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("generate again: %v", err)
	}
	if first.Content.Topic != second.Content.Topic || second.Source != first.Source {
		t.Fatalf("idempotency broken: %#v vs %#v", first, second)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
}

func TestGenerateFallsBackWhenPlannerFails(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent([]domain.DailyContent{poolItem("pool-1", "color", "color.white.tone")})
	runs := &fakeRuns{}
	planner := &fakePlanner{err: errors.New("llm down")}
	service := newService(t, fakeReader{}, facts, content, runs, &fakeCollections{}, planner)

	result, err := service.Generate(context.Background(), "user-1", "bad-token")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceFallback || result.Content.ID != "pool-1" {
		t.Fatalf("result = %#v", result)
	}
	foundFallback := false
	for _, run := range runs.runs {
		if run.Outcome == "fallback" && run.Validation["reason"] == "llm_failed" {
			foundFallback = true
		}
	}
	if !foundFallback {
		t.Fatalf("runs = %#v", runs.runs)
	}
}

func TestGenerateFallsBackWhenNoFacts(t *testing.T) {
	content := newFakeContent([]domain.DailyContent{poolItem("pool-1", "fit", "fit.x")})
	runs := &fakeRuns{}
	service := newService(t, fakeReader{}, fakeKnowledge{}, content, runs, &fakeCollections{}, &fakePlanner{})

	result, err := service.Generate(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceFallback {
		t.Fatalf("source = %s", result.Source)
	}
	if len(runs.runs) != 1 || runs.runs[0].Validation["reason"] != "no_facts" {
		t.Fatalf("runs = %#v", runs.runs)
	}
}

func TestGenerateRetriesOnceOnValidationFailureThenAccepts(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent(nil)
	planner := &fakePlanner{outputs: []daily.ContentOutput{
		{Topic: "你的颜值亮点", Lead: "导语", Fit: "适配", Why: "原理", Visual: goodOutput(1).Visual, Refs: []int{1}},
		goodOutput(1),
	}}
	service := newService(t, fakeReader{}, facts, content, &fakeRuns{}, &fakeCollections{}, planner)

	result, err := service.Generate(context.Background(), "user-1", "token")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceGenerated {
		t.Fatalf("source = %s", result.Source)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", planner.calls)
	}
}

func TestGenerateFallsBackAfterSecondRejection(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent([]domain.DailyContent{poolItem("pool-1", "color", "color.white.tone")})
	runs := &fakeRuns{}
	bad := daily.ContentOutput{Topic: "显胖预警", Lead: "导语", Fit: "适配", Why: "原理", Refs: []int{1}}
	planner := &fakePlanner{outputs: []daily.ContentOutput{bad, bad}}
	service := newService(t, fakeReader{}, facts, content, runs, &fakeCollections{}, planner)

	result, err := service.Generate(context.Background(), "user-1", "token")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceFallback || planner.calls != 2 {
		t.Fatalf("source=%s calls=%d", result.Source, planner.calls)
	}
	if len(runs.runs) < 3 {
		t.Fatalf("expected rejected + fallback runs, got %#v", runs.runs)
	}
}

func TestGenerateFallsBackToStaticWhenPoolEmpty(t *testing.T) {
	facts := fakeKnowledge{facts: []domain.KnowledgeFact{fact("f-1", "color", "不挑人的事实", nil)}}
	content := newFakeContent(nil)
	service := newService(t, fakeReader{}, facts, content, &fakeRuns{}, &fakeCollections{}, &fakePlanner{err: errors.New("llm down")})

	result, err := service.Generate(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Source != daily.SourceFallback || result.Content.Topic == "" {
		t.Fatalf("result = %#v", result)
	}
}

// ---- collection ----

func TestCreateCollectionBuildsSnapshotAndColorAssets(t *testing.T) {
	pool := poolItem("pool-1", "color", "color.white.tone")
	pool.Visual = domain.ContentVisual{
		Modality: "swatch",
		Spec:     map[string]any{"items": []map[string]any{{"tone": "#FBFBF8"}, {"tone": "#EFEADC"}}},
		Alt:      "色卡",
	}
	content := newFakeContent([]domain.DailyContent{pool})
	collections := &fakeCollections{}
	service := newService(t, fakeReader{}, fakeKnowledge{}, content, &fakeRuns{}, collections, &fakePlanner{})

	item, err := service.CreateCollection(context.Background(), "user-1", "pool-1", "先留着")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if item.ContentKey != "color.white.tone" || item.Category != "color" || item.Note != "先留着" {
		t.Fatalf("item = %#v", item)
	}
	if item.Snapshot.FitText != pool.FitText || item.Snapshot.ID != "pool-1" {
		t.Fatalf("snapshot = %#v", item.Snapshot)
	}
	if len(item.Assets) != 1 || item.Assets[0].Kind != "colors" || len(item.Assets[0].Colors) != 2 {
		t.Fatalf("assets = %#v", item.Assets)
	}
}

func TestCreateCollectionRejectsUnknownContent(t *testing.T) {
	content := newFakeContent(nil)
	service := newService(t, fakeReader{}, fakeKnowledge{}, content, &fakeRuns{}, &fakeCollections{}, &fakePlanner{})

	if _, err := service.CreateCollection(context.Background(), "user-1", "missing", ""); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := service.CreateCollection(context.Background(), "user-1", "", ""); !errors.Is(err, daily.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateCollectionRejectsInvalidStatus(t *testing.T) {
	service := newService(t, fakeReader{}, fakeKnowledge{}, newFakeContent(nil), &fakeRuns{}, &fakeCollections{}, &fakePlanner{})
	if _, err := service.UpdateCollection(context.Background(), "user-1", "col-1", "loved", ""); !errors.Is(err, daily.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectionStatsNormalizesSevenBuckets(t *testing.T) {
	service := newService(t, fakeReader{}, fakeKnowledge{}, newFakeContent(nil), &fakeRuns{}, &fakeCollections{}, &fakePlanner{})
	stats, err := service.CollectionStats(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.Counts) != 7 || stats.Total != 0 {
		t.Fatalf("stats = %#v", stats)
	}
}
