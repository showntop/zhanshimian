package share

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Source 是不可变分享快照的来源投影：标题、摘要、发布媒体的 object key 与来源类型。
// ObjectKey 而非签名 URL——签名 URL 在公开读取时按需生成。
type Source struct {
	SourceType   string
	SourceID     string
	Title        string
	Summary      string
	AssetID      string
	ObjectKey    string
	MIMEType     string
	SourceKind   domain.MediaSourceKind
	DisplayLabel string
	PublishedAt  time.Time
}

type Reader interface {
	ReadShareSource(ctx context.Context, userID string, sourceType string, sourceID string) (Source, error)
}
