package share_test

import (
	"context"

	"github.com/zhanshimian/server/internal/service/share"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type shareReaderFake struct{}

func (shareReaderFake) ReadShareSource(context.Context, string, string, string) (share.Source, error) {
	return share.Source{}, nil
}

var _ share.Reader = shareReaderFake{}
