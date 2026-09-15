package storage

import (
	"context"
	"io"
)

// Opener 是只读对象库的最小接口；ObjectStorage 与各服务的窄接口都满足。
type Opener interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

// OpenProcessedOr 优先用数据万象下载时处理（process 规则如
// imageMogr2/thumbnail/1280x/...）；存储不支持或 CI 未开通时回退原对象。
// 注意：CI 处理产出的格式以规则为准（现均为 JPEG），与原资产声明格式
// 可能不同——需要 MIME 时用 ai.SniffImageMIME 按字节内容判定。
func OpenProcessedOr(ctx context.Context, objects Opener, key, process string) (io.ReadCloser, error) {
	if processed, ok := objects.(ProcessedOpener); ok {
		if reader, err := processed.OpenProcessed(ctx, key, process); err == nil {
			return reader, nil
		}
	}
	return objects.Open(ctx, key)
}
