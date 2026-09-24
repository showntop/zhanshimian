package daily

import (
	"github.com/zhanshimian/server/internal/domain"
)

// 换装洗牌 variant（spec 2026-09-23-dress-shuffle §1）。
//
// hash(uid+date) % 2 稳定二选一：同一天同一人只看同一套，两天见两套。
// 服务端选 variant、客户端按 settle 脚本的 kind 分发——旧客户端不认识
// dress_lock，按既有回落纪律退静态形态 + 立即揭晓，不会空白，所以
// variant 对旧客户端安全，无需版本门槛。
//
// override 是灰度/紧急开关（DAILY_MOTION_VARIANT）：sketch=全量关停回旧线，
// dress=全量放量；空串=按哈希自然分流。
const (
	MotionVariantSketch = "sketch"
	MotionVariantDress  = "dress"
)

// dressAssetsVersion 换装素材清单版本（客户端预热缓存/回源判重用）。
// 素材更新（M4 流水线重跑）时递增日期。
const dressAssetsVersion = "20260923"

// motionVariant 当日动画方案：override 优先，否则稳定哈希分流。
func motionVariant(userID, genDate, override string) string {
	switch override {
	case MotionVariantSketch, MotionVariantDress:
		return override
	}
	if stableHash("dress|"+userID+"|"+genDate)%2 == 0 {
		return MotionVariantDress
	}
	return MotionVariantSketch
}

// ---------- dress_lock 收敛脚本的语义值库 ----------
//
// 这些名字是服务端与客户端洗牌库共享的词汇表（客户端 presets.ts 同名同序），
// 改任何一边都必须同步另一边：target 是语义值，不是索引。
// 词表同时是 LLM 产出 lock 的合法取值域（provider 校验用），所以导出。

var DressLookKeys = []string{
	"outfit", "ratio", "fit", "occasion",
	"look-shirt-skirt", "look-hoodie-jeans", "look-coat", "look-trench",
}

var DressColorNames = []string{
	"燕麦", "砖红", "墨绿", "藏蓝", "浅灰", "驼色",
	"雾蓝", "酒红", "橄榄", "炭灰", "奶油白", "粉棕",
}

var DressWaistNames = []string{"高腰", "中腰", "低腰"}

var DressHairKeys = []string{"wave", "bun", "bob", "long"}

// ValidLockValue 某一维取值是否在词表内（空值视为合法=该维走回退）。
// provider 校验与 validateOutput 共用这一份，不另抄词表。
func ValidLockValue(dim, value string) bool {
	if value == "" {
		return true
	}
	var list []string
	switch dim {
	case "look":
		list = DressLookKeys
	case "color":
		list = DressColorNames
	case "waist":
		list = DressWaistNames
	case "hair":
		list = DressHairKeys
	default:
		return false
	}
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
}

// inList 词表成员判定（内部用）。
func inList(list []string, value string) bool {
	if value == "" {
		return false
	}
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
}

// lookOfCategory 分类 → 造型键（spec §1：就近归类，语义真）。
// 没有自然对应物的分类走稳定随机——当天看哪套由 uid+date 决定（剧场真）。
func lookOfCategory(category, seed string) string {
	switch category {
	case "outfit":
		return "outfit"
	case "proportion":
		return "ratio"
	case "fit":
		return "fit"
	case "occasion":
		return "occasion"
	}
	return DressLookKeys[stableHash(seed+"|look")%uint64(len(DressLookKeys))]
}

// pickStable 词汇表稳定抽样：同一天固定，跨天会换。
func pickStable(list []string, seed string) string {
	return list[stableHash(seed)%uint64(len(list))]
}

// dressLockPresentation 换装洗牌的收敛脚本（kind=dress_lock）。
//
// target 派生纪律（spec §1）：定格的那套必须就是建议本身——
// 优先用与建议同源落库的 content.Lock（LLM 生成时产出、逐维校验过词表）；
// 缺哪维回退哪维：look ← category 就近归类，color/waist/hair ← hash(uid+date)
// 稳定随机（剧场真）。pace 沿用收敛脚本的节奏哲学：等得越久揭晓越隆重。
func dressLockPresentation(content domain.DailyContent, elapsedMS int, assetBase string) Presentation {
	seed := content.UserID + "|" + content.GenDate
	look := lookOfCategory(content.Category, seed)
	color := pickStable(DressColorNames, seed+"|color")
	waist := pickStable(DressWaistNames, seed+"|waist")
	// 发型只对 hero look 存在素材；非 hero 的日子客户端自动跳过该维度
	hair := pickStable(DressHairKeys, seed+"|hair")
	if lock := content.Lock; lock != nil {
		if inList(DressLookKeys, lock.Look) {
			look = lock.Look
		}
		if inList(DressColorNames, lock.Color) {
			color = lock.Color
		}
		if inList(DressWaistNames, lock.Waist) {
			waist = lock.Waist
		}
		if inList(DressHairKeys, lock.Hair) {
			hair = lock.Hair
		}
	}
	target := map[string]any{"look": look, "color": color, "waist": waist, "hair": hair}
	pace := "normal"
	if elapsedMS >= 15000 {
		pace = "slow"
	}
	// 换装素材走 /assets/ 静态路由（与序列帧揭晓同一机制，dress-assets.py 产出落盘）
	dressBase := ""
	if assetBase != "" {
		dressBase = assetBase + "/assets/daily/dress"
	}
	// 收敛轮时长上界：客户端在脚本缺席时用它兜底放行，避免动画没播完就被
	// 切海报（每维加速快切+飞卡约 2.4s，加四维连击与盖章停顿）。
	dims := 3 // look/color/waist
	if look == "outfit" {
		dims = 4 // hero look 才带发型维度（发型素材只做了 hero look）
	}
	durationMS := 1400 + dims*2400
	return Presentation{
		Version: presentationVersion,
		Stages: []Stage{{
			Phase:      "settle",
			Kind:       "dress_lock",
			DurationMS: durationMS,
			Params: map[string]any{
				"target": target,
				"pace":   pace,
				"assets": map[string]any{"base": dressBase, "version": dressAssetsVersion},
			},
		}},
	}
}
