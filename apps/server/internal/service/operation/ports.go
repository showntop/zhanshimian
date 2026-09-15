package operation

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type Reader interface {
	GetOperation(context.Context, string, string) (domain.Operation, error)
	GetOperations(context.Context, string, []string) ([]domain.Operation, error)
}

// StaleFailer 把超过在途阈值的孤儿 operation 落失败终态（worker 整体死亡时
// 用户不再无限转圈）。写入由仓储以单语句 CAS 保证不误杀活着的任务。
type StaleFailer interface {
	FailStaleOperation(ctx context.Context, userID, id string, idleFor time.Duration, code, publicMessage string) (bool, error)
}
