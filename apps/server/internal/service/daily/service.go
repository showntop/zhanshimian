package daily

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	// generateTotalTimeout 客户端动画硬超时对齐（方案 §2.6）。
	generateTotalTimeout = 6 * time.Second
	// llmTimeout 留 1s 给校验与落库。
	llmTimeout = 5 * time.Second
	// pickTTL 选品快照有效期。
	pickTTL = 5 * time.Minute
	// seenWindowDays 历史去重窗口。
	seenWindowDays = 30

	// ScenarioFallback 未覆盖场景统一回落兜底动画（方案 §6）。
	ScenarioFallback = "fallback"
)

// allCategories 手册七格（方案 §daily-collection-schema）。
var allCategories = []string{"color", "fit", "proportion", "fabric", "occasion", "howto", "outfit"}

type Service struct {
	reader      Reader
	knowledge   KnowledgeStore
	content     ContentStore
	runs        RunStore
	collections CollectionStore
	planner     ContentPlanner
	weather     WeatherProvider
	clock       Clock
	picks       *pickStore
	zone        *time.Location
}

// New 组装每日内容服务。planner/weather 可后装（未装时生成直接走兜底）。
func New(reader Reader, knowledge KnowledgeStore, content ContentStore, runs RunStore, collections CollectionStore, clock Clock) *Service {
	return &Service{
		reader: reader, knowledge: knowledge, content: content, runs: runs, collections: collections,
		clock: clock, picks: newPickStore(pickTTL, 512),
		zone: time.FixedZone("Asia/Shanghai", 8*60*60),
	}
}

func (s *Service) WithPlanner(planner ContentPlanner) *Service {
	s.planner = planner
	return s
}

func (s *Service) WithWeather(weather WeatherProvider) *Service {
	s.weather = weather
	return s
}

// today 生成日期一律按 Asia/Shanghai：跨零点时用 UTC 会把「今天」算成前一天。
func (s *Service) today() string {
	now := s.clock.Now()
	if s.zone != nil {
		now = now.In(s.zone)
	}
	return now.Format("2006-01-02")
}

// Generate 生成今日内容：永远返回内容（generated 或 fallback），
// 真正的错误（鉴权、DB 不可用）才返回 error。
func (s *Service) Generate(ctx context.Context, userID string, pickToken string) (GenerateResult, error) {
	genDate := s.today()

	// ① 幂等：当天已生成过就直接返回（(user_id, gen_date) 唯一约束是硬保证）。
	if content, err := s.content.TodayContent(ctx, userID, genDate); err == nil {
		return GenerateResult{Source: content.Source, Content: content}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, generateTotalTimeout)
	defer cancel()

	// ② 取选品快照；token 失效/缺失（网络抖动后重试）时按当前条件重建一次。
	snapshot, ok := s.picks.take(pickToken, userID)
	if !ok {
		snapshot = s.selectFacts(runCtx, userID, genDate, "")
	}
	if len(snapshot.Facts) == 0 || s.planner == nil {
		return s.fallback(ctx, userID, genDate, &snapshot, reasonNoFacts)
	}

	// ③ LLM 生成（5s）+ 自动校验；校验失败带修正提示重试一次。
	output, err := s.callPlanner(runCtx, snapshot, "")
	if err == nil {
		if problems := validateOutput(output, len(snapshot.Facts)); len(problems) > 0 {
			s.record(runCtx, snapshot, output, problems, outcomeRejected, "")
			retry, retryErr := s.callPlanner(runCtx, snapshot, retryHint(problems))
			if retryErr != nil {
				return s.fallback(ctx, userID, genDate, &snapshot, reasonRetryFailed)
			}
			if retryProblems := validateOutput(retry, len(snapshot.Facts)); len(retryProblems) > 0 {
				s.record(runCtx, snapshot, retry, retryProblems, outcomeRejected, "")
				return s.fallback(ctx, userID, genDate, &snapshot, reasonRejected)
			}
			output = retry
		}
	} else {
		s.record(runCtx, snapshot, ContentOutput{}, nil, outcomeFallback, err.Error())
		return s.fallback(ctx, userID, genDate, &snapshot, reasonLLMFailed)
	}

	// ④ 落库并返回（同一天第二次调用会命中幂等分支）。
	content := buildContent(userID, genDate, snapshot, output)
	saved, err := s.content.SaveContent(ctx, content)
	if err != nil {
		// 并发写撞唯一约束：读回已生成的那条，不把它当失败。
		if existing, readErr := s.content.TodayContent(ctx, userID, genDate); readErr == nil {
			return GenerateResult{Source: existing.Source, Content: existing}, nil
		}
		s.record(runCtx, snapshot, output, nil, outcomeFallback, err.Error())
		return s.fallback(ctx, userID, genDate, &snapshot, reasonSaveFailed)
	}
	s.record(runCtx, snapshot, output, nil, outcomeAccepted, "")
	return GenerateResult{Source: SourceGenerated, Content: saved}, nil
}

func (s *Service) callPlanner(ctx context.Context, snapshot pickSnapshot, hint string) (ContentOutput, error) {
	llmCtx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()
	request := ContentRequest{
		GeneText:    snapshot.GeneText,
		WeatherText: contextText(snapshot.GenDate, snapshot.Weather),
		ContextText: snapshot.ContextText,
		HistoryText: snapshot.HistoryText,
		Angle:       snapshot.Angle,
		Category:    snapshot.Category,
		RetryHint:   hint,
	}
	for index, fact := range snapshot.Facts {
		request.Facts = append(request.Facts, FactPrompt{
			Index: index + 1, Domain: fact.Domain, Fact: fact.Fact, Boundary: fact.Boundary,
		})
	}
	return s.planner.Generate(llmCtx, request)
}

// buildContent 把校验通过的生成结果落成 daily_content 行。
// dedupe_key 带日期：同一用户同一天只生成一条，跨天可以再推同类内容。
func buildContent(userID string, genDate string, snapshot pickSnapshot, output ContentOutput) domain.DailyContent {
	visual, _ := buildVisual(output.Visual)
	return domain.DailyContent{
		UserID:    userID,
		GenDate:   genDate,
		Category:  snapshot.Category,
		Topic:     output.Topic,
		Lead:      output.Lead,
		FitText:   output.Fit,
		Why:       output.Why,
		Visual:    visual,
		FactIDs:   snapshot.FactIDs,
		Source:    SourceGenerated,
		ModelKey:  output.ModelKey,
		DedupeKey: "gen:" + genDate + ":" + snapshot.Category,
	}
}
