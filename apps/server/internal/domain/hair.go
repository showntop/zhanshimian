package domain

// HairPreviewTaskPayload 是 hair_preview 任务的载荷：主题即预览本身，
// 无 generation（每个预览一次性生成，不 supersede）。
type HairPreviewTaskPayload struct {
	PreviewID string `json:"preview_id"`
}
