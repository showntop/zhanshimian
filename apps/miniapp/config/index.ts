import path from 'node:path'
import { defineConfig } from '@tarojs/cli'
import devConfig from './dev'
import prodConfig from './prod'

// 兼容性 pin：Taro 4.2.1 weapp 渲染层（react-reconciler）与 React 19 不兼容，
// 锁 react/react-dom 18.3.1。解锁条件：Taro 官方 React 19 支持稳定后升级。
// 注：本文件不进 tsc typecheck（由 Taro CLI 自行转译），避免 CLI 泛型默认
// webpack5 与 vite compiler 的类型冲突。
export default defineConfig(async (merge) => {
  const baseConfig = {
    projectName: 'zhanshimian-miniapp',
    designWidth: 750,
    deviceRatio: { 640: 2.34 / 2, 750: 1, 375: 2, 828: 1.81 / 2 },
    sourceRoot: 'src',
    outputRoot: 'dist',
    plugins: [],
    framework: 'react',
    compiler: 'vite',
    alias: {
      // 配置文件由 Taro CLI 以 CJS 加载：必须用 __dirname，禁 import.meta。
      '@': path.resolve(__dirname, '..', 'src'),
    },
    defineConstants: {
      API_BASE_URL: JSON.stringify(process.env.ZSM_API_BASE_URL || 'http://127.0.0.1:58000'),
    },
    copy: {
      patterns: [
        { from: 'src/assets/', to: 'dist/assets/' },
        { from: 'src/sitemap.json', to: 'dist/sitemap.json' },
      ],
      options: {},
    },
    mini: {
      postcss: {
        pxtransform: { enable: true, config: {} },
      },
      miniCssExtractPluginOption: { ignoreOrder: true },
      // 保 /assets/*.jpg 绝对路径契约：禁止 Vite 把小图转 base64。
      vitePlugins: [],
    },
    build: { assetsInlineLimit: 0 },
    h5: {},
  }
  if (process.env.NODE_ENV === 'development') {
    return merge({}, baseConfig, devConfig)
  }
  return merge({}, baseConfig, prodConfig)
})
