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

var dressLookKeys = []string{
	"outfit", "ratio", "fit", "occasion",
	"look-shirt-skirt", "look-hoodie-jeans", "look-coat", "look-trench",
}

var dressColorNames = []string{
	"燕麦", "砖红", "墨绿", "藏蓝", "浅灰", "驼色",
	"雾蓝", "酒红", "橄榄", "炭灰", "奶油白", "粉棕",
}

var dressWaistNames = []string{"高腰", "中腰", "低腰"}

var dressHairKeys = []string{"wave", "bun", "bob", "long"}

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
	return dressLookKeys[stableHash(seed+"|look")%uint64(len(dressLookKeys))]
}

// pickStable 词汇表稳定抽样：同一天固定，跨天会换。
func pickStable(list []string, seed string) string {
	return list[stableHash(seed)%uint64(len(list))]
}

// dressLockPresentation 换装洗牌的收敛脚本（kind=dress_lock）。
//
// target 派生纪律（spec §1）：look ← content.category 就近归类（定格的那套
// 必须就是建议本身）；color/waist/hair ← hash(uid+date) 稳定随机（剧场真）。
// pace 沿用收敛脚本的节奏哲学：等得越久揭晓越隆重。
func dressLockPresentation(content domain.DailyContent, elapsedMS int, assetBase string) Presentation {
	seed := content.UserID + "|" + content.GenDate
	target := map[string]any{
		"look":  lookOfCategory(content.Category, seed),
		"color": pickStable(dressColorNames, seed+"|color"),
		"waist": pickStable(dressWaistNames, seed+"|waist"),
		// 发型只对 hero look 存在素材；非 hero 的日子客户端自动跳过该维度
		"hair": pickStable(dressHairKeys, seed+"|hair"),
	}
	pace := "normal"
	if elapsedMS >= 15000 {
		pace = "slow"
	}
	// 换装素材走 /assets/ 静态路由（与序列帧揭晓同一机制，dress-assets.py 产出落盘）
	dressBase := ""
	if assetBase != "" {
		dressBase = assetBase + "/assets/daily/dress"
	}
	return Presentation{
		Version: presentationVersion,
		Stages: []Stage{{
			Phase: "settle",
			Kind:  "dress_lock",
			Params: map[string]any{
				"target": target,
				"pace":   pace,
				"assets": map[string]any{"base": dressBase, "version": dressAssetsVersion},
			},
		}},
	}
}
