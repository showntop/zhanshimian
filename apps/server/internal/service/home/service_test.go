package home_test

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/service/home"
)

// 编译期契约：Reader 的签名是本计划冻结的边界，改签名这里先红。
type homeReaderFake struct{}

func (homeReaderFake) ReadHome(context.Context, string, time.Time) (home.Snapshot, error) {
	return home.Snapshot{}, nil
}

var _ home.Reader = homeReaderFake{}
