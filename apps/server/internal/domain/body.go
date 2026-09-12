package domain

import "time"

// BodyPresentationInput 是创建 3D 形象 Lite 的入参：正脸 + 正面全身媒体 ID。
type BodyPresentationInput struct {
	BodyMediaID string `json:"body_media_id"`
	FaceMediaID string `json:"face_media_id"`
}

// OrbitFrame 是环绕抽帧的 API 形状；存储侧只持久化 yaw 与 object key，
// URL 在读取时由签名器投影。
type OrbitFrame struct {
	Yaw float64 `json:"yaw"`
	URL string  `json:"url"`
}

type BodyOrbitView struct {
	VideoURL   string       `json:"video_url,omitempty"`
	DurationMS int          `json:"duration_ms,omitempty"`
	Frames     []OrbitFrame `json:"frames"`
}

type BodyMesh struct {
	Format     string `json:"format"`
	URL        string `json:"url"`
	TextureURL string `json:"texture_url,omitempty"`
}

// BodyPresentation 的状态/进度/阶段字段投影自关联的 body_orbit 任务，
// 不在 body_presentations 表上冗余存储。
type BodyPresentation struct {
	ID              string        `json:"id"`
	BodyMediaID     string        `json:"body_media_id"`
	FaceMediaID     string        `json:"face_media_id"`
	Representation  string        `json:"representation"`
	Orbit           BodyOrbitView `json:"orbit"`
	Mesh            *BodyMesh     `json:"mesh"`
	ProviderVersion string        `json:"provider_version,omitempty"`
	Status          string        `json:"status"`
	Progress        int           `json:"progress"`
	Stage           string        `json:"stage"`
	ErrorMessage    string        `json:"error_message,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// BodyPresentationStatus 是实验室启动读模型：能力是否可用 + 进行中 /
// 最新可播成功 / 比成功更新的最新失败。
type BodyPresentationStatus struct {
	Available bool              `json:"available"`
	Active    *BodyPresentation `json:"active"`
	Completed *BodyPresentation `json:"completed"`
	Failed    *BodyPresentation `json:"failed"`
}

// BodyOrbitTaskPayload 是 body_orbit 任务 payload_version=1 的形状。
type BodyOrbitTaskPayload struct {
	PresentationID string `json:"presentation_id"`
}

// StoredBodyPresentation 是仓储层读取结果：API 视图 + 存储键，
// 签名投影（VideoURL/Frames[].URL）由服务层在读取时完成。
type StoredBodyPresentation struct {
	Presentation    BodyPresentation
	VideoStorageKey string
	FrameKeys       []string
}

// CreatedBodyPresentation 是创建结果：展示资源 + 公开操作 + 队列任务。
// Reused=true 表示命中进行中的既有资源（不重复扣费）。
type CreatedBodyPresentation struct {
	Presentation StoredBodyPresentation
	Operation    Operation
	Task         Task
	Reused       bool
}
