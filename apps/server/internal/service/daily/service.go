package daily

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	// generateTotalTimeout 整条生成链路的上界：LLM + 校验 +（失败重试一次）+ 落库。
	// 客户端等待动画是循环的、接口返回才落位，宁可慢也不落兜底。
	generateTotalTimeout = 48 * time.Second
	// llmTimeout 单次调用上限（模型侧 timeout 是 120s，这里是业务侧更早的闸）。
	llmTimeout = 20 * time.Second
	// seenWindowDays 历史去重窗口（prompt 历史 + topic 去重闸共用）。
	seenWindowDays = 14
	// historyLimit prompt 里带的历史条数上限。
	historyLimit = 14
	// referenceCount 检索注入的参考事实条数。
	referenceCount = 6

	// ScenarioFallback 未覆盖场景统一回落兜底动画（方案 §6）。
	ScenarioFallback = "fallback"
)

// CategoryGeneral 归不进其余各格的综合内容（LLM 自报 + 手册分格都用它）。
const CategoryGeneral = "general"

// allCategories 手册的全部分格：方案 §6 的七格 + 2026-09-21 扩充的
// 发型/妆容/配饰（补齐形象顾问的三条主线）+ general。
//
// 一个集合同时供三处使用——手册计数/过滤、LLM 自报分类的合法取值、
// 收藏兴趣分布。分开定义就会出现「统计不认 general、列表却收得到」这类
// 半边漏，所以这里刻意只留一份。
var allCategories = []string{"color", "fit", "proportion", "fabric", "occasion", "howto", "outfit", "hair", "makeup", "accessory", CategoryGeneral}

type Service struct {
	reader      Reader
	knowledge   KnowledgeStore
	content     ContentStore
	runs        RunStore
	collections CollectionStore
	planner     ContentPlanner
	weather     WeatherProvider
	clock       Clock
	zone        *time.Location
	// assetBase 素材基地址（PUBLIC_BASE_URL）：序列帧等静态素材的 URL 前缀。
	// 空串 = 不下发帧脚本，收敛退回 CSS 形态（素材缺席不能拿掉全部动画）。
	assetBase string
	// forceRegen 调试开关（DAILY_FORCE_REGEN，非生产）：跳过当日幂等，
	// 每次都实时重生成 + 覆盖当天记录。
	forceRegen bool
	// motionVariantOverride 动画方案覆盖（DAILY_MOTION_VARIANT）：
	// sketch=灰度关停回旧线；dress=全量放量；空串=按 hash(uid+date) 分流。
	motionVariantOverride string
}

// New 组装每日内容服务。planner/weather 可后装（未装时生成直接走兜底）。
func New(reader Reader, knowledge KnowledgeStore, content ContentStore, runs RunStore, collections CollectionStore, clock Clock) *Service {
	return &Service{
		reader: reader, knowledge: knowledge, content: content, runs: runs, collections: collections,
		clock: clock,
		zone:  time.FixedZone("Asia/Shanghai", 8*60*60),
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

// WithAssetBase 配置静态素材基地址（PUBLIC_BASE_URL）：序列帧揭晓依赖它。
func (s *Service) WithAssetBase(base string) *Service {
	s.assetBase = base
	return s
}

// WithForceRegen 打开调试实时重生成（仅非生产环境接线）。
func (s *Service) WithForceRegen(enabled bool) *Service {
	s.forceRegen = enabled
	return s
}

// WithMotionVariant 配置动画方案覆盖（DAILY_MOTION_VARIANT，灰度/紧急开关）。
func (s *Service) WithMotionVariant(override string) *Service {
	s.motionVariantOverride = override
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
// 选题与成文是同一次 LLM 调用：语境在 buildContext 里聚齐。
func (s *Service) Generate(ctx context.Context, userID string, city string) (result GenerateResult, err error) {
	genDate := s.today()
	started := time.Now()
	// 收敛脚本由「内容 + 本次实际耗时」反推，所有返回路径（含兜底）都要带上，
	// 所以放在 defer 里统一补——漏掉兜底分支就会出现「内容有了但没动画」。
	defer func() {
		if err != nil || result.Content.ID == "" || result.Presentation != nil {
			return
		}
		elapsed := int(time.Since(started).Milliseconds())
		// variant=dress 且素材基地址就绪 → 换装洗牌收敛（dress_lock）。
		// assetBase 为空时 dress 素材必然加载不出，回落旧线（不空转）。
		if motionVariant(result.Content.UserID, genDate, s.motionVariantOverride) == MotionVariantDress && s.assetBase != "" {
			presentation := dressLockPresentation(result.Content, elapsed, s.assetBase)
			result.Presentation = &presentation
			return
		}
		presentation := settlePresentation(result.Content, elapsed, s.assetBase)
		result.Presentation = &presentation
	}()

	// ① 幂等：当天已生成过就直接返回（(user_id, gen_date) 唯一约束是硬保证）。
	// 调试开关下跳过：每次都真的重生成一遍。
	if !s.forceRegen {
		if content, err := s.content.TodayContent(ctx, userID, genDate); err == nil {
			return GenerateResult{Source: content.Source, Content: content}, nil
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, generateTotalTimeout)
	defer cancel()

	// ② 聚齐语境：天气/画像/近推历史/收藏信号/参考事实。
	gctx := s.buildContext(runCtx, userID, genDate, city)
	if s.planner == nil {
		return s.fallback(ctx, userID, genDate, "", reasonNoPlanner)
	}

	// ③ LLM 一次成文 + 自动校验；校验失败带修正提示重试一次。
	output, err := s.callPlanner(runCtx, gctx, "")
	if err == nil {
		if problems := validateOutput(output, gctx.recentTopics); len(problems) > 0 {
			s.record(runCtx, gctx, output, problems, outcomeRejected, "")
			retry, retryErr := s.callPlanner(runCtx, gctx, retryHint(problems))
			if retryErr != nil {
				return s.fallback(ctx, userID, genDate, output.Category, reasonRetryFailed)
			}
			if retryProblems := validateOutput(retry, gctx.recentTopics); len(retryProblems) > 0 {
				s.record(runCtx, gctx, retry, retryProblems, outcomeRejected, "")
				return s.fallback(ctx, userID, genDate, retry.Category, reasonRejected)
			}
			output = retry
		}
	} else {
		s.record(runCtx, gctx, ContentOutput{}, nil, outcomeFallback, err.Error())
		return s.fallback(ctx, userID, genDate, "", reasonLLMFailed)
	}

	// ④ 落库并返回（同一天第二次调用会命中幂等分支）。
	// 调试开关下覆盖当天记录：否则 upsert 被唯一约束挡住，调试永远只看得到第一条。
	content := buildContent(userID, genDate, gctx, output)
	saved, err := func() (domain.DailyContent, error) {
		if s.forceRegen {
			return s.content.ReplaceContent(ctx, content)
		}
		return s.content.SaveContent(ctx, content)
	}()
	if err != nil {
		// 并发写撞唯一约束：读回已生成的那条，不把它当失败。
		if existing, readErr := s.content.TodayContent(ctx, userID, genDate); readErr == nil {
			return GenerateResult{Source: existing.Source, Content: existing}, nil
		}
		s.record(runCtx, gctx, output, nil, outcomeFallback, err.Error())
		return s.fallback(ctx, userID, genDate, output.Category, reasonSaveFailed)
	}
	s.record(runCtx, gctx, output, nil, outcomeAccepted, "")
	return GenerateResult{Source: SourceGenerated, Content: saved}, nil
}

func (s *Service) callPlanner(ctx context.Context, gctx generateContext, hint string) (ContentOutput, error) {
	llmCtx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()
	request := ContentRequest{
		ContextText:    gctx.ContextText,
		GeneText:       gctx.GeneText,
		HistoryText:    gctx.HistoryText,
		InterestText:   gctx.InterestText,
		FindingsText:   gctx.FindingsText,
		ReferenceFacts: gctx.ReferenceFacts,
		RetryHint:      hint,
	}
	return s.planner.Generate(llmCtx, request)
}

// buildContent 把校验通过的生成结果落成 daily_content 行。
// dedupe_key 带日期：同一用户同一天只生成一条，跨天可以再推同类内容。
func buildContent(userID string, genDate string, gctx generateContext, output ContentOutput) domain.DailyContent {
	visual, _ := buildVisual(output.Visual)
	lock := output.Lock
	return domain.DailyContent{
		UserID:    userID,
		GenDate:   genDate,
		Category:  output.Category,
		Topic:     output.Topic,
		Lead:      output.Lead,
		FitText:   output.Fit,
		Why:       output.Why,
		Visual:    visual,
		Lock:      &lock,
		FactIDs:   gctx.referenceIDs,
		Source:    SourceGenerated,
		ModelKey:  output.ModelKey,
		DedupeKey: "gen:" + genDate + ":" + output.Category,
	}
}
