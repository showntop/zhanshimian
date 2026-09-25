package daily

import (
	"fmt"

	"github.com/zhanshimian/server/internal/domain"
)

// 内容视觉的插画装饰：给当日内容挂一张内置时装插画当主视觉。
//
// 背景：LLM 生成的 visual 只有 swatch/compare/diagram 三种程序化形态
// （色票/对比/示意是信息图，但每天都是色块，观感就是"没画完"）。
// 仓库里其实有一套时装插画（assets/daily/dress/color/，换装洗牌同源，
// 经 /assets/ 静态路由下发），只是从没接进 content.visual——这里接上。
//
// 纪律：
//   - modality 不变。程序化视觉仍是该内容的信息图（色票的 pick/drop、
//     对比的两侧），插画是额外的氛围层，客户端各画各的；
//   - 只加 spec 字段（契约里 spec 是 additionalProperties: true，免改契约）；
//   - source_kind=bundled_reference：这是内置插画素材，不是用户照片、
//     也不是 AI 生成效果图。数据层带来源（强类型/埋点依赖它），
//     上屏不叠角标（2026-09-16 决策：角标不再上屏）；
//   - 没有映射的分类（makeup/accessory/fabric/item/general）保持原样，
//     宁可程序化视觉也不硬凑不相关的图；
//   - assetBase 为空（素材基地址未配置）时原样返回：素材缺席不造 URL。

// visualArtByCategory 内容分类 → 插画文件（assets/daily/dress/color/ 下）。
// 多个文件的分类用稳定抽样：同一天固定，跨天会换（与 variant.go 同哲学）。
var visualArtByCategory = map[string][]string{
	"outfit":     {"outfit.png"},
	"fit":        {"fit.png"},
	"proportion": {"ratio.png"},
	"occasion":   {"occasion.png"},
	"howto":      {"card-rule-v2.jpg"},
	// 颜色/发型没有专属单图：用 look / 发型系列稳定抽一张
	"color": {"look-coat.png", "look-trench.png", "look-shirt-skirt.png", "look-hoodie-jeans.png"},
	"hair":  {"outfit-bob.png", "outfit-bun.png", "outfit-wave.png"},
}

// decorateVisual 给内容视觉挂插画主图。幂等：已带 image 的 spec 不重复装饰
//（历史数据回读会再次走到这里）。所有 Generate 返回路径（含幂等读、兜底）
// 都经过它，所以是唯一挂点。
func decorateVisual(content domain.DailyContent, assetBase string) domain.DailyContent {
	if assetBase == "" {
		return content
	}
	files, ok := visualArtByCategory[content.Category]
	if !ok || len(files) == 0 {
		return content
	}
	if content.Visual.Spec == nil {
		content.Visual.Spec = map[string]any{}
	}
	if _, exists := content.Visual.Spec["image"]; exists {
		return content
	}
	file := pickStable(files, content.UserID+"|"+content.GenDate+"|art")
	content.Visual.Spec["image"] = fmt.Sprintf("%s/assets/daily/dress/color/%s", assetBase, file)
	content.Visual.Spec["source_kind"] = "bundled_reference"
	return content
}
