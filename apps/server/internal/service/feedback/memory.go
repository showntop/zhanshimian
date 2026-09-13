package feedback

import "github.com/zhanshimian/server/internal/domain"

// acknowledgementCodeForMemories 根据已持久化的记忆返回精确确认码;无记忆只确认
// 记录完成。与 domain.AcknowledgementCodeFor 的差异在于它消费已落库的
// domain.PreferenceMemory 而非 domain.PreferenceMemoryDraft,因此页面只承诺真正
// 写入的偏好。
func acknowledgementCodeForMemories(memories []domain.PreferenceMemory) domain.AcknowledgementCode {
	if len(memories) == 0 {
		return domain.AckFeedbackRecorded
	}
	switch memories[0].Key {
	case "formality":
		return domain.AckLessFormalSaved
	case "complexity":
		return domain.AckSimplerSaved
	case "avoid_color":
		return domain.AckAvoidColorSaved
	case "preserve_hair", "preserve_makeup", "preserve_outfit":
		return domain.AckPreserveSaved
	default:
		return domain.AckFeedbackRecorded
	}
}
