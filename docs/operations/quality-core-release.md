# Quality Core 发布与回滚手册

本手册固定 quality-core 切换的发布顺序、放量阶梯与回滚动作。放量百分比
只允许 `0/5/25/50/100` 五档,一律经 `node apps/server/scripts/set-rollout.mjs`
修改,禁止手编辑 `ai-routing.production.json` 绕过阶梯。

## 1. 基线部署

1. 停 API/Worker;删除并重建开发或 Staging database/volume;执行
   `apps/server/internal/database/migrations/001_baseline.sql`;部署同版本
   API 与独立 Worker;发布同步小程序。
2. 先运行内部授权样本与 `apps/server/scripts/staging-golden.sh`
   (`UPLOOK_GOLDEN_DATASET_DIR` 指向私有对象存储挂载的授权金集),
   锁定集报告 `/tmp/uplook-locked-release.json` 必须全部 hard gate 通过。

## 2. 放量阶梯

依次提交路由百分比 5、25、50、100:

```bash
node apps/server/scripts/set-rollout.mjs --percent 5    # 观察 ≥72h / 100 用户 / 300 次发布
node apps/server/scripts/set-rollout.mjs --percent 25   # 观察 ≥72h / 500 用户 / 1500 次发布
node apps/server/scripts/set-rollout.mjs --percent 50   # 观察 ≥72h / 1000 用户 / 3000 次发布
node apps/server/scripts/set-rollout.mjs --percent 100  # 观察 ≥168h / 2000 用户 / 6000 次发布
```

每档最小观察窗满足后、且复检全部通过才升下一档。每档复检:来源错配、
敏感推断、身份/人体错误、P95(拍照检查/报告/方案文案/首张渲染)、成本
和 Candidate 2 比率。指标按 `provider_invocations.release_bucket`
(0-99 确定性桶号)与 `routing_config_version` 归因。

## 3. 停止条件与回滚

任一停止条件触发(来源错配、WebP 发布、旧生成覆盖、严重坏图发布率
≥1%、身份通过率 <90%、P95 超阈、供应商留存违规),立即执行:

```bash
node apps/server/scripts/set-rollout.mjs --percent 0
```

并发布该路由配置。若问题不在路由,部署 `quality-core-pre-cutover` 标记
的 API/Worker 镜像。

回滚时**禁止**执行 `docker compose down -v`、旧 migration、数据回填、
双读或兼容 DTO;新 schema 和新业务数据保持不变。

## 4. 修复后重新发布

修复版本必须在同一新 baseline 上通过全仓 gates(`make e2e`、
`node apps/miniapp/scripts/check.mjs`、`make server-vet && make server-test`、
`pnpm typecheck && pnpm lint && make design-build`、
`pnpm --filter @zsm/core api:check`、`docker compose config --quiet`)与
锁定金集(`bash apps/server/scripts/staging-golden.sh`)后,重新从 5% 开始。

## 5. 已知限制

- attempt 耗尽的 leased task 目前没有自动清扫器(lease sweeper),积压时
  需人工把 `lease_expires_at` 过期的 task 重置回 `pending`。
- 渲染质量门禁的 worker 侧接入与 hairstyles 目录表为后续跟进项,见计划
  文档执行修正记录。
