package domain

import "regexp"

// BannedCopyPattern 是全服务端唯一的文案禁则词源：报告草稿校验
// （service/assessment）与每日内容生成校验（service/daily）共用同一份，
// 两处不分裂（方案 §4）。红线：不打颜值分/身材分、不身材羞辱、不做医学结论。
//
// 追加词条要同时影响两处，因此只在这里改；某个域需要更严的口径时，
// 在本词源之外叠加自己的补充词表（如 daily 追加 显胖/显瘦/肥胖）。
var BannedCopyPattern = regexp.MustCompile(`颜值|身材分|评分|百分位|缺陷严重|诊断|疾病|族裔|性格`)

// ContainsBannedCopy 命中禁则词即 true。
func ContainsBannedCopy(text string) bool { return BannedCopyPattern.MatchString(text) }

// ContainsAnyBannedCopy 任一段命中即 true。
func ContainsAnyBannedCopy(texts ...string) bool {
	for _, text := range texts {
		if ContainsBannedCopy(text) {
			return true
		}
	}
	return false
}
