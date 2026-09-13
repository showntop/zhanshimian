package advisor_test

import (
	"context"

	"github.com/zhanshimian/server/internal/service/advisor"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type advisorReaderFake struct{}

func (advisorReaderFake) ReadAdvisorGrounding(context.Context, string) (advisor.Grounding, error) {
	return advisor.Grounding{}, nil
}

var _ advisor.Reader = advisorReaderFake{}
