package daily

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/zhanshimian/server/internal/domain"
)

// 自动校验三道闸（方案 §4）：黑名单 → 结构 → 去重。全部毫秒级，
// 拦截 → 带修正提示重试一次 → 仍败走兜底并留痕。
//
// 禁则词表与报告草稿校验共用 domain.BannedCopyPattern 这一份词源，
// 只在日报场景额外叠加「显胖/显瘦/肥胖」这类评判词。

var dailyBanned = regexp.MustCompile(`显胖|显瘦|肥胖|整容|丑|种族|品牌|价格|竞品`)

var (
	errStructure = errors.New("structure_invalid")
	errBlacklist = errors.New("blacklist_hit")
	errCategory  = errors.New("category_invalid")
	errDuplicate = errors.New("topic_duplicate")
)

const (
	maxTopicRunes = 14
	maxLeadRunes  = 44
	maxFitRunes   = 66
	maxWhyRunes   = 44
	maxAltRunes   = 60
)

// validateOutput 返回问题清单（空 = 通过）。
// recentTopics 是近 14 天已推 topic：去重闸的第二道防线（第一道在 prompt）。
func validateOutput(output ContentOutput, recentTopics []string) []error {
	var problems []error
	if err := validateStructure(output); err != nil {
		problems = append(problems, err)
	}
	if err := validateBlacklist(output); err != nil {
		problems = append(problems, err)
	}
	if err := validateCategory(output); err != nil {
		problems = append(problems, err)
	}
	if err := validateDuplicate(output, recentTopics); err != nil {
		problems = append(problems, err)
	}
	if _, err := buildVisual(output.Visual); err != nil {
		problems = append(problems, err)
	}
	return problems
}

func validateStructure(output ContentOutput) error {
	for field, value := range map[string]string{
		"topic": output.Topic, "lead": output.Lead, "fit": output.Fit, "why": output.Why,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s 为空", errStructure, field)
		}
	}
	if runes(output.Topic) > maxTopicRunes {
		return fmt.Errorf("%w: topic 超长", errStructure)
	}
	if runes(output.Lead) > maxLeadRunes {
		return fmt.Errorf("%w: lead 超长", errStructure)
	}
	if runes(output.Fit) > maxFitRunes {
		return fmt.Errorf("%w: fit 超长", errStructure)
	}
	if runes(output.Why) > maxWhyRunes {
		return fmt.Errorf("%w: why 超长", errStructure)
	}
	return nil
}

func validateBlacklist(output ContentOutput) error {
	fields := []string{output.Topic, output.Lead, output.Fit, output.Why, output.Visual.Alt}
	for _, value := range fields {
		if domain.ContainsBannedCopy(value) {
			return fmt.Errorf("%w: 命中共享禁则词表", errBlacklist)
		}
		if dailyBanned.MatchString(value) {
			return fmt.Errorf("%w: 命中每日内容禁则词表", errBlacklist)
		}
	}
	for _, item := range output.Visual.Items {
		if domain.ContainsBannedCopy(item.Label) || dailyBanned.MatchString(item.Label) {
			return fmt.Errorf("%w: 视觉标签命中禁则", errBlacklist)
		}
	}
	return nil
}

// validateCategory 自报分类必须在七格 + general 内（手册归类的合法域）。
func validateCategory(output ContentOutput) error {
	for _, category := range allCategories {
		if output.Category == category {
			return nil
		}
	}
	return fmt.Errorf("%w: 分类 %q 不在七格与 general 内", errCategory, output.Category)
}

// validateDuplicate topic 去重闸：与近期已推 topic 规范化比对
// （去空白与标点，防「换措辞重复」的最廉价形态）。
func validateDuplicate(output ContentOutput, recentTopics []string) error {
	topic := normalizeTopic(output.Topic)
	if topic == "" {
		return nil // 空topic由结构闸负责
	}
	for _, recent := range recentTopics {
		if topic == normalizeTopic(recent) {
			return fmt.Errorf("%w: topic 与近 14 天已推的「%s」重复", errDuplicate, recent)
		}
	}
	return nil
}

// normalizeTopic 只留字母数字（汉字是字母）：空白与标点一律剥掉。
func normalizeTopic(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// buildVisual 把模型给的视觉提示落成客户端能直接画的 ContentVisual。
// 解析不出来就报错（由调用方走兜底），绝不猜。
func buildVisual(draft VisualDraft) (domain.ContentVisual, error) {
	alt := strings.TrimSpace(draft.Alt)
	switch draft.Modality {
	case "swatch":
		if len(draft.Items) < 2 {
			return domain.ContentVisual{}, fmt.Errorf("%w: 色卡至少需要两项", errStructure)
		}
		items := make([]map[string]any, 0, len(draft.Items))
		for _, item := range draft.Items {
			if item.Tone == "" || item.Label == "" {
				return domain.ContentVisual{}, fmt.Errorf("%w: 色项缺色值或标签", errStructure)
			}
			if !isHexColor(item.Tone) {
				return domain.ContentVisual{}, fmt.Errorf("%w: 色值不是 #RRGGBB", errStructure)
			}
			entry := map[string]any{"tone": item.Tone, "label": item.Label}
			if item.State == "pick" || item.State == "drop" {
				entry["state"] = item.State
			}
			items = append(items, entry)
		}
		if alt == "" {
			alt = "色卡示意"
		}
		return domain.ContentVisual{
			Modality: "swatch",
			Spec:     map[string]any{"items": items},
			Alt:      truncate(alt, maxAltRunes),
		}, nil
	case "compare":
		if draft.Left == nil || draft.Right == nil || draft.Left.Label == "" || draft.Right.Label == "" {
			return domain.ContentVisual{}, fmt.Errorf("%w: 对比缺一侧或标签", errStructure)
		}
		spec := map[string]any{
			"left":  sideSpec(draft.Left),
			"right": sideSpec(draft.Right),
		}
		if strings.TrimSpace(draft.Marker) != "" {
			spec["marker"] = truncate(strings.TrimSpace(draft.Marker), 8)
		}
		if alt == "" {
			alt = draft.Left.Label + " 与 " + draft.Right.Label + " 的对比"
		}
		return domain.ContentVisual{Modality: "compare", Spec: spec, Alt: truncate(alt, maxAltRunes)}, nil
	case "diagram":
		kind := draft.Kind
		if kind != "body" && kind != "scale" {
			return domain.ContentVisual{}, fmt.Errorf("%w: diagram kind 非法", errStructure)
		}
		if len(draft.Items) < 2 {
			return domain.ContentVisual{}, fmt.Errorf("%w: 示意图至少需要两项", errStructure)
		}
		items := make([]map[string]any, 0, len(draft.Items))
		for _, item := range draft.Items {
			if item.Label == "" {
				return domain.ContentVisual{}, fmt.Errorf("%w: 示意图项缺标签", errStructure)
			}
			entry := map[string]any{"label": item.Label}
			if item.State == "pick" || item.State == "avoid" {
				entry["state"] = item.State
			}
			items = append(items, entry)
		}
		if alt == "" {
			alt = "位置示意"
		}
		return domain.ContentVisual{
			Modality: "diagram",
			Spec:     map[string]any{"kind": kind, "items": items},
			Alt:      truncate(alt, maxAltRunes),
		}, nil
	}
	return domain.ContentVisual{}, fmt.Errorf("%w: 不支持的 modality %q", errStructure, draft.Modality)
}

func sideSpec(side *VisualSide) map[string]any {
	out := map[string]any{"label": side.Label}
	if side.Tone != "" && isHexColor(side.Tone) {
		out["tone"] = side.Tone
	}
	if side.Layered {
		out["layered"] = true
	}
	return out
}

func isHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// retryHint 把问题翻译成模型能懂的修正提示（只重试一次）。
func retryHint(problems []error) string {
	parts := make([]string, 0, len(problems)+1)
	parts = append(parts, "上一次输出被自动校验拦截，请修正后重新输出：")
	for _, problem := range problems {
		switch {
		case errors.Is(problem, errBlacklist):
			parts = append(parts, "- 出现了评判性或营销性词汇（不评分、不评判身材、不提品牌价格），请换中性表达。")
		case errors.Is(problem, errDuplicate):
			parts = append(parts, "- topic 与近期已推的重复，请换一个全新的主题。")
		case errors.Is(problem, errCategory):
			parts = append(parts, "- category 必须是 color/fit/proportion/fabric/occasion/howto/outfit/general 之一。")
		case errors.Is(problem, errStructure):
			parts = append(parts, "- 结构不合法："+problem.Error()+"（字段必填、长度不超限、视觉参数完整）。")
		default:
			parts = append(parts, "- "+problem.Error())
		}
	}
	return strings.Join(parts, "\n")
}

func runes(value string) int { return len([]rune(value)) }

func truncate(value string, limit int) string {
	if runes(value) <= limit {
		return value
	}
	chars := []rune(value)
	return string(chars[:limit])
}
