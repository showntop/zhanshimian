// Task 11 把 api 与 worker 拆成两个进程后，两个容器会在全新库上同时启动、
// 同时跑 Migrate：无锁时 DDL 撞 pg_type_typname_nsp_index（23505），
// 其中一个进程直接退出。Migrate 必须用 advisory lock 串行化。
package database_test

import (
	"context"
	"sync"
	"testing"

	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestMigrateConcurrentCallsSucceed(t *testing.T) {
	pool := testutil.NewPostgres(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start // 同时放行，最大化撞锁窗口
			errs[slot] = database.Migrate(ctx, pool)
		}(i)
	}
	close(start)
	wg.Wait()
	for slot, err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate %d: %v", slot, err)
		}
	}
}
