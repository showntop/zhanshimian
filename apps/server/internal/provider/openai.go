package provider

// 形象分析契约层：提示词、JSON Schema、输出校验与锚点整理。
// 供能力路由（ai_routed.go）复用；供应商 HTTP 细节在 ai_runtime.go 的协议层实现。
import (
	"encoding/base64"
	"math"
	"strings"
)

type analysisDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// openai_responses 协议的响应形状（ai_runtime.go 协议层与历史 responses 端点共用）
type responseContent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

type responsesAPIResponse struct {
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Type    string            `json:"type"`
		Content []responseContent `json:"content"`
	} `json:"output"`
}

func responseText(response responsesAPIResponse) (string, string) {
	for _, item := range response.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "refusal" && content.Refusal != "" {
				return "", content.Refusal
			}
			if content.Type == "output_text" && content.Text != "" {
				return content.Text, ""
			}
		}
	}
	return "", ""
}

type analysisStep struct {
	Category string           `json:"category"`
	Title    string           `json:"title"`
	Summary  string           `json:"summary"`
	Details  []analysisDetail `json:"details"`
}

type analysisPlan struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Recommended    bool           `json:"recommended"`
	Descriptor     string         `json:"descriptor"`
	Why            string         `json:"why"`
	OutcomeTags    []string       `json:"outcome_tags"`
	DifferenceTags []string       `json:"difference_tags"`
	Steps          []analysisStep `json:"steps"`
}

// decodeAnalysisPayload 是分析输出的唯一解码入口：先做形状归一化再反序列化。
// json_object 模式只保证合法 JSON，不约束结构（json_schema 才约束）；
// 模型偶发把字符串数组输出成嵌套数组（如 impression_tags: [["自然亲和"]]，
// 见 kimi-k3 线上报错），归一化拍平后仍可strict校验通过，避免整次调用作废。
// 第二个返回值表示是否发生了形状修正（用于日志观测模型行为）。

// collapseNestedStringArrays 把「数组元素是数组」的嵌套拍平成一层
// （[["a"],["b"]] / [["a","b"]] → ["a","b"]），对整棵 JSON 树递归。
// 分析 Schema 中所有数组都是字符串/对象数组，不存在合法嵌套，拍平是安全的。
func collapseNestedStringArrays(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = collapseNestedStringArrays(item)
		}
		return typed
	case []any:
		items := make([]any, 0, len(typed))
		nested := false
		for _, item := range typed {
			if inner, ok := item.([]any); ok {
				nested = true
				items = append(items, inner...)
				continue
			}
			items = append(items, collapseNestedStringArrays(item))
		}
		if nested {
			return collapseNestedStringArrays(items)
		}
		return items
	default:
		return value
	}
}

func photoKindName(kind string) string {
	names := map[string]string{"face": "正脸", "side": "侧脸", "body": "全身"}
	if value := names[kind]; value != "" {
		return value
	}
	return kind
}

func dataURL(mimeType string, data []byte) string {
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// preview trims model copy for log messages so validation errors stay one line.
func preview(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return string(runes)
}

// minAnchorGap is the smallest normalized distance wanted between two anchors
// on the same photo: below it the report hero card renders dots and chips on
// top of each other. Models tend to cluster coordinates near the frame center,
// so near-duplicates are nudged apart deterministically instead of failing the
// whole analysis over a cosmetic issue.
const minAnchorGap = 0.12

// separateAnchors pushes same-photo anchors that sit closer than minAnchorGap
// downward (wrapping to the top and shifting right when running out of room)
// until every placed anchor is sufficiently far away. The pass is bounded and
// keeps values inside [0.03, 0.97].

func clampAnchor(value float64) float64 {
	if value < 0.03 {
		return 0.03
	}
	if value > 0.97 {
		return 0.97
	}
	return value
}

func anchorDistance(x1, y1, x2, y2 float64) float64 {
	return math.Hypot(x1-x2, y1-y2)
}

func safeText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 160 {
		return false
	}
	for _, forbidden := range []string{"颜值", "丑", "身材", "评分", "肥胖", "缺陷", "整容", "种族", "族裔", "疾病", "诊断"} {
		if strings.Contains(value, forbidden) {
			return false
		}
	}
	return true
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
