package home

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Clock 是时间源的最小抽象：测试注入固定时钟，生产注入系统时钟。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// NewClock 返回生产用的系统时钟。
func NewClock() Clock { return systemClock{} }

type Service struct {
	reader Reader
	clock  Clock
}

func New(reader Reader, clock Clock) *Service {
	return &Service{reader: reader, clock: clock}
}

// Bootstrap 是首页的唯一入口：一次 ReadHome 调用，不在 service 层再拼第二份数据。
func (s *Service) Bootstrap(ctx context.Context, userID string) (Snapshot, error) {
	snapshot, err := s.reader.ReadHome(ctx, userID, s.clock.Now())
	if snapshot.ActiveOperations == nil {
		snapshot.ActiveOperations = []domain.OperationRef{}
	}
	return snapshot, err
}
