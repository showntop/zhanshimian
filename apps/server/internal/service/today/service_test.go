package today_test

import (
	"context"

	"github.com/zhanshimian/server/internal/service/today"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type todayReaderFake struct{}

func (todayReaderFake) ReadTodayGrounding(context.Context, string) (today.Grounding, error) {
	return today.Grounding{}, nil
}

var _ today.Reader = todayReaderFake{}
