# @zsm/miniapp · 怎么打扮小程序（Taro 4 + React）

## 命令

```bash
pnpm --filter @zsm/miniapp dev:weapp    # 开发构建（微信开发者工具打开本目录）
pnpm --filter @zsm/miniapp build:weapp  # 生产构建（产出 dist/）
pnpm --filter @zsm/miniapp typecheck
node scripts/check.mjs                  # 静态门禁（路由/资产/rpx/体积）
```

## 兼容性 Pin（受控决策）

- **react/react-dom 18.3.1**：Taro 4.2.1 的 weapp 渲染层基于 react-reconciler，
  与 React 19 不兼容。解锁条件：Taro 官方 React 19 支持稳定（见 docs/architecture.md §3）。
- 所有依赖精确版本（无 `^`），升级须整体验证。

## 工程要点

- `designWidth: 750`，样式一律 `rpx`（check.mjs 门禁；阴影/模糊的视觉 px 除外）。
- `copy.patterns` + `assetsInlineLimit: 0`：保 `/assets/*.jpg` 绝对路径契约
  （@zsm/core 媒体真实性逻辑的前提），禁止 Vite base64 内联。
- `@zsm/design/dist/tokens.scss` 经 workspace 符号链接 + 显式 `.scss` 扩展名导入
  （Taro 的 nativeStyleImporter 无法从 node_modules 解析 .wxss 后缀）。
- config/index.ts 由 Taro CLI 以 CJS 加载：**禁用 `import.meta`**（用 `__dirname`）。
- 业务请求只经 `services/http.ts` 的 client（401 单飞重登重放）；禁直接 Taro.request。
- 本地存储 key 见 `services/storage.ts`（zsm_ 前缀）。
- 组件默认 `styleIsolation: 'apply-shared'`（Taro 编译默认），全局语义类可直接作用。
