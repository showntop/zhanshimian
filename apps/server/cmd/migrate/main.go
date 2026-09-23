// 数据库迁移工具：默认只做向前迁移；--reset 仅限非生产环境。
//
// 生产环境（APP_ENV=production）的 reset 在触碰任何 SQL 之前直接退出——
// baseline 收敛后没有回填路径，重置生产库等于销毁全部用户数据。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/database"
)

func main() {
	reset := flag.Bool("reset", false, "drop public schema then migrate from scratch (non-production only)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatal("load config: %v", err)
	}

	ctx := context.Background()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal("%v", err)
	}
	defer pool.Close()

	if *reset {
		if cfg.Environment == "production" {
			fatal("refusing to reset a production database; reset is allowed only outside production")
		}
		if err := database.Reset(ctx, pool); err != nil {
			fatal("reset: %v", err)
		}
		fmt.Println("database reset")
	}
	if err := database.Migrate(ctx, pool); err != nil {
		fatal("migrate: %v", err)
	}
	fmt.Println("migrations applied")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate: "+format+"\n", args...)
	os.Exit(1)
}
