package daily

import (
	"math"

	"github.com/zhanshimian/server/internal/domain"
)

// 动画表演脚本（契约见 contracts/openapi.yaml 的 DailyPresentation）。
//
// 分工纪律：服务端只做语义决策——今天讲什么、哪套是答案（target）、哪套是
// 反例（counter）、节奏用哪一档。视觉空间（有几个变体、长什么样）属于客户端
// 的 form，两边靠语义值对接。于是新增一种动画 = 注册一个 kind/form，
// 服务端与播放器都不用改。
//
// 用户色板（gene）由客户端渲染时注入：脚本是通用的、可缓存的，不携带任何
// 用户隐私数据。

const presentationVersion = 1

// roamThemeMS 巡游每个主题的停留时长（原型调参：4s/主题，7 主题约 28s 一轮）。
const roamThemeMS = 4000

type MotionAsset struct {
	URL    string `json:"url"`
	Kind   string `json:"kind"`
	Frames int    `json:"frames,omitempty"`
	FPS    int    `json:"fps,omitempty"`
	W      int    `json:"w,omitempty"`
	H      int    `json:"h,omitempty"`
}

type Stage struct {
	Phase      string         `json:"phase"`
	Kind       string         `json:"kind"`
	Form       string         `json:"form,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	Asset      *MotionAsset   `json:"asset,omitempty"`
	DurationMS int            `json:"duration_ms,omitempty"`
	Repeat     int            `json:"repeat,omitempty"`
	Label      string         `json:"label,omitempty"`
}

type Presentation struct {
	Version int     `json:"version"`
	Stages  []Stage `json:"stages"`
}

// roamPresentation 等待期脚本：把几个方向都过一遍。
//
// 「等待期不知道今天讲什么」本是技术限制（选题在 generate 的 LLM 调用里），
// 这里把它转成产品语义——顾问在几个方向里权衡。巡游因此与用户无关、
// 可按 version 长期缓存，运营改顺序/文案都不需要发版。
func roamPresentation() Presentation {
	themes := []struct {
		theme string
		form  string
		label string
	}{
		{"color", "swatch_bars", "在看颜色"},
		{"fit", "silhouette_shape", "在看版型"},
		{"proportion", "ratio_blocks", "在看比例"},
		{"fabric", "texture_lines", "在看面料"},
		{"occasion", "scene_panel", "在看场合"},
		{"howto", "fold_lines", "在看穿法"},
		{"outfit", "outfit_blocks", "在看搭配"},
	}
	items := make([]map[string]any, 0, len(themes))
	for _, t := range themes {
		items = append(items, map[string]any{"theme": t.theme, "form": t.form, "label": t.label})
	}
	return Presentation{
		Version: presentationVersion,
		Stages: []Stage{{
			Phase:      "roam",
			Kind:       "roam_tour",
			Params:     map[string]any{"themes": items, "per_ms": roamThemeMS},
			DurationMS: roamThemeMS * len(themes),
		}},
	}
}

// axisSpec 一个收敛轴：变什么、哪一刻锁死、答案与反例。
//
// target/counter 是语义值（"cropped" / 0.38 / "harmony"）而不是索引——变体有
// 几个、长什么样由客户端 form 决定，服务端不假设它的视觉空间。
type axisSpec struct {
	ID      string  `json:"id"`
	BrakeAt float64 `json:"brake_at"`
	Target  any     `json:"target"`
	Counter any     `json:"counter"`
}

// formOfCategory 分类 → 形态渲染器（客户端注册表里的名字）。
func formOfCategory(category string) string {
	switch category {
	case "fit":
		return "silhouette_shape"
	case "proportion":
		return "ratio_blocks"
	case "color":
		return "swatch_bars"
	case "fabric":
		return "texture_lines"
	case "occasion":
		return "scene_panel"
	case "howto":
		return "fold_lines"
	default:
		// outfit 与 general 共用块的构图：上/下长度比本身就讲比例。
		return "outfit_blocks"
	}
}

// dimsOfCategory 各收敛维度的权重：让分类有性格，而调度器保持通用。
//
// drift=聚拢（粒子感）　blur=对焦感　survive=淘汰感　palette=色彩收敛
func dimsOfCategory(category string) map[string]any {
	switch category {
	case "color":
		return map[string]any{"drift": 1.0, "blur": 0.5, "survive": 0.6, "palette": 1.0}
	case "fit":
		return map[string]any{"drift": 0.4, "blur": 1.0, "survive": 0.7, "palette": 0.5}
	case "fabric", "occasion":
		return map[string]any{"drift": 0.6, "blur": 0.8, "survive": 0.5, "palette": 0.9}
	default:
		return map[string]any{"drift": 0.8, "blur": 1.0, "survive": 0.7, "palette": 1.0}
	}
}

// axesOf 分类的收敛轴。target 尽量取自 content.visual.spec——
// 定格的那一套必须就是建议本身，而不是「随机停在哪一套」。
func axesOf(content domain.DailyContent) []axisSpec {
	spec := content.Visual.Spec
	switch content.Category {
	case "proportion":
		return []axisSpec{
			{"waist", 1.0, numFromSpec(spec, "ratio", 0.38), 0.58},
			{"volume", 0.85, "balanced", "heavy"},
			{"palette", 0.7, "harmony", "clash"},
		}
	case "color":
		return []axisSpec{
			{"hue", 1.0, strFromSpec(spec, "family", "harmony"), "clash"},
			{"order", 0.86, "ascending", "shuffled"},
			{"lift", 0.7, "normal", "muted"},
		}
	case "fit":
		return []axisSpec{
			{"shoulder", 1.0, strFromSpec(spec, "shoulder", "natural"), "wide"},
			{"waist", 0.85, strFromSpec(spec, "waist", "straight"), "cinched"},
			{"hem", 0.72, strFromSpec(spec, "hem", "straight"), "flared"},
		}
	case "fabric":
		return []axisSpec{
			{"density", 1.0, strFromSpec(spec, "density", "medium"), "dense"},
			{"drape", 0.85, strFromSpec(spec, "drape", "fluid"), "stiff"},
			{"tone", 0.7, "muted", "mixed"},
		}
	case "occasion":
		return []axisSpec{
			{"temp", 1.0, strFromSpec(spec, "tone", "warm"), "cold"},
			{"light", 0.85, "soft", "harsh"},
			{"shape", 0.7, "balanced", "busy"},
		}
	case "howto":
		return []axisSpec{
			{"fold", 1.0, strFromSpec(spec, "folds", "two"), "none"},
			{"position", 0.85, strFromSpec(spec, "position", "above_elbow"), "wrist"},
			{"tilt", 0.7, "neutral", "tilted"},
		}
	default: // outfit / general
		return []axisSpec{
			{"top", 1.0, strFromSpec(spec, "top", "cropped"), "long"},
			{"bottom", 0.85, strFromSpec(spec, "bottom", "long"), "wide"},
			{"palette", 0.7, "harmony", "clash"},
		}
	}
}

func strFromSpec(spec map[string]any, key string, fallback string) string {
	if spec == nil {
		return fallback
	}
	if v, ok := spec[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func numFromSpec(spec map[string]any, key string, fallback float64) float64 {
	if spec == nil {
		return fallback
	}
	switch v := spec[key].(type) {
	case float64:
		if !math.IsNaN(v) {
			return v
		}
	case int:
		return float64(v)
	}
	return fallback
}

// tempoFor 节奏档位：等待越久，揭晓越隆重（期待值不同，回报的分量也不同）。
// 数值来自原型调参；加长不靠单纯放慢间隔，靠戏剧停顿（客户端侧的三个节奏点）。
func tempoFor(elapsedMS int) map[string]any {
	switch {
	case elapsedMS < 3000:
		return map[string]any{"steps": 13, "interval": []int{85, 270}}
	case elapsedMS < 15000:
		return map[string]any{"steps": 16, "interval": []int{95, 330}}
	default:
		return map[string]any{"steps": 18, "interval": []int{100, 380}}
	}
}

// settlePresentation 内容到位后的收敛 + 揭晓。
//
// elapsedMS 是本次生成实际耗时——服务端自己知道，客户端不必上报。
func settlePresentation(content domain.DailyContent, elapsedMS int) Presentation {
	axes := axesOf(content)
	brakes := make([]float64, 0, len(axes))
	for _, axis := range axes {
		brakes = append(brakes, axis.BrakeAt)
	}
	// 等得久，揭晓值得慢一点：显影比光泽扫过更有「浮现」的仪式感。
	reveal := "sweep"
	if elapsedMS >= 15000 {
		reveal = "develop"
	}
	return Presentation{
		Version: presentationVersion,
		Stages: []Stage{
			{
				Phase: "settle",
				Kind:  "converge",
				Form:  formOfCategory(content.Category),
				Params: map[string]any{
					"axes":       axes,
					"dims":       dimsOfCategory(content.Category),
					"tempo":      tempoFor(elapsedMS),
					"brakes":     brakes,
					"overshoot":  true,
					"theatrical": true,
				},
			},
			{Phase: "reveal", Kind: reveal},
		},
	}
}
