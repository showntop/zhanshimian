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
// processed 为 true 表示返回的是 CI 处理后的字节——调用方应按处理规则
// 的产出格式（现均为 JPEG）标记 MIME，而不是沿用原资产的声明格式。
func OpenProcessedOr(ctx context.Context, objects Opener, key, process string) (reader io.ReadCloser, processed bool, err error) {
	if p, ok := objects.(ProcessedOpener); ok {
		if r, err := p.OpenProcessed(ctx, key, process); err == nil {
			return r, true, nil
		}
	}
	rc, err := objects.Open(ctx, key)
	return rc, false, err
}
