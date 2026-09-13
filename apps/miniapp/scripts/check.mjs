#!/usr/bin/env node
/**
 * 小程序静态门禁（CI 必跑）。单文件规则集中在 checkSourceFile（可被 node:test 直测）：
 *  - scss/JSX 字符串禁止裸 px（须 rpx；阴影/模糊的视觉 px 保留）
 *  - 拒绝 {count && <...>}、{items.length && <...>} 等数值直接 && JSX
 *  - 拒绝 key={index}/{i}/{idx}
 *  - 全仓禁 .webp；禁 provider 嗅探、isBundledAsset、钉住旧图
 *  - 禁业务 Storage key；主闭环页面不得 import api/cache/operations/storage 层
 *  - 轮询唯一入口：createOperationPolling 只许出现在 use-operation-polling；
 *    useTaskPolling/createTaskPolling 全仓禁止
 * 全仓规则（路由↔目录、页面三件套、资产存在性、组件 config、体积）由 run() 执行。
 */
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { pathToFileURL } from 'node:url'

const appRoot = join(import.meta.dirname, '..')
const src = join(appRoot, 'src')
const dist = join(appRoot, 'dist')

/** 主闭环八页：它们的薄壳纪律由 here 与 tests/architecture 双重把守。 */
const MAIN_LOOP_ROUTES = ['capture', 'analysis', 'report', 'scene', 'plans', 'plan', 'checklist', 'feedback']
const OPERATION_WRAPPER = 'app/operations/use-operation-polling'
const NUMERIC_NAME = /(?:count|total|num|number|length|size|index|idx|done|remain|remaining)s?$/i

/** 单文件规则：返回违例文案数组（已带 file:line）。path 用仓内相对形式。 */
export function checkSourceFile(path, source) {
  const problems = []
  const posix = path.replace(/\\/g, '/')
  const isScss = posix.endsWith('.scss')
  const isTsx = posix.endsWith('.tsx') || posix.endsWith('.ts')

  source.split('\n').forEach((rawLine, i) => {
    const lineNo = i + 1
    const fail = (message) => problems.push(`${message}: ${posix}:${lineNo}`)
    const code = rawLine.replace(/\/\/.*$/, '')

    if (isScss) {
      // 阴影/模糊的视觉 px 有意保留：先把这类声明整段剥掉再扫
      const withoutShadow = code.replace(/(box-shadow|text-shadow|backdrop-filter|filter)\s*:[^;}]+/g, '')
      const barePx = /(^|[\s:,(])-\d+px(?![a-z])/i.exec(withoutShadow) || /(^|[\s:,(])\d+px(?![a-z])/i.exec(withoutShadow)
      if (barePx) fail('scss 裸 px（须 rpx）')
      return
    }

    if (!isTsx) return

    // 数值/可能为 0 的表达式直接 && JSX：React 会把 0 渲染成 "0"
    const zeroAnd = /(^|[^!\w'])\s*(?:\w+\.length|\w+\.size|[A-Za-z_$][\w$]*)\s*&&\s*</.exec(code)
    if (zeroAnd) {
      const subject = code.slice(0, zeroAnd.index).trim() || zeroAnd[0]
      const name = /([\w.]+)\s*&&/.exec(`${subject} ${zeroAnd[0]}`)?.[1] ?? ''
      if (/\.(length|size)\s*&&/.test(zeroAnd[0]) || NUMERIC_NAME.test(name.split('.').pop() ?? '')) {
        fail('条件渲染可能渲染 0')
        return
      }
    }

    // 列表下标 key
    if (/key=\{(index|i|idx)\}/.test(code)) fail('列表禁止下标 key')

    // WebP（微信渲染空白）
    if (/\.webp/i.test(code)) fail('禁止 WebP')

    // 钉住旧图 / provider 嗅探 / isBundledAsset
    if (/pinnedUrl|PinnedImage/.test(code)) fail('禁止钉住旧图')
    if (/provider_version\s*\?\?|provider_version\)\s*\.startsWith|\.startsWith\('demo'\)/.test(code)) {
      fail('禁止按 provider 判定演示图')
    }
    if (/\bisBundledAsset\b/.test(code)) fail('禁止 isBundledAsset')

    // 业务 Storage key
    if (/STORAGE_KEYS\.(reportId|planId|savedPlanId|activeTask|scene|outfit|purchase|advisor)/.test(code)) {
      fail('禁止业务 Storage key')
    }

    // 轮询纪律
    if (/useTaskPolling|createTaskPolling/.test(code)) fail('禁止任务轮询（统一 useOperationPolling）')
    if (/createOperationPolling/.test(code) && !posix.replace(/^\.\//, '').startsWith(OPERATION_WRAPPER)) {
      fail('轮询只能经 useOperationPolling（createOperationPolling 禁止外用）')
    }

    // 主闭环页面不得 import 数据层
    if (posix.startsWith('pages/') && posix.endsWith('/index.tsx')) {
      const route = posix.split('/')[1]
      if (MAIN_LOOP_ROUTES.includes(route)) {
        if (/from\s+'[^']*(app\/api|app\/cache|app\/operations|services\/storage)/.test(rawLine)) {
          fail('主闭环页面不得 import api/cache/operations/storage 层')
        }
      }
    }
  })

  return problems
}

function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) walk(full, out)
    else out.push(full)
  }
  return out
}

function run() {
  const problems = []
  const notes = []

  // ---- 单文件规则 ----
  for (const full of walk(src)) {
    if (!/\.(ts|tsx|scss)$/.test(full)) continue
    const rel = relative(src, full)
    for (const problem of checkSourceFile(rel, readFileSync(full, 'utf8'))) {
      problems.push(problem)
    }
  }

// ---- 组件 config 门：每个 custom component 必须显式 apply-shared ----
const componentsDir = join(src, 'components')
for (const name of readdirSync(componentsDir)) {
  const componentDir = join(componentsDir, name)
  if (!statSync(componentDir).isDirectory()) continue
  const configFile = join(componentDir, 'index.config.ts')
  if (!existsSync(configFile)) {
    problems.push(`组件缺 index.config.ts: components/${name}`)
    continue
  }
  const configText = readFileSync(configFile, 'utf8')
  if (!/styleIsolation:\s*'apply-shared'/.test(configText)) {
    problems.push(`组件 config 缺 styleIsolation: 'apply-shared': components/${name}`)
  }
}

// ---- 1/2. 路由 ↔ 目录 ----
const configText = readFileSync(join(src, 'app.config.ts'), 'utf8')
const pagesSection = configText.split('subPackages')[0] ?? configText
const mainPages = [...pagesSection.matchAll(/^\s*'([\w/-]+\/index)',?\s*$/gm)].map((m) => m[1])
const subRoots = [...configText.matchAll(/root:\s*'([^']+)'/g)].map((m) => m[1])
if (mainPages.length === 0) problems.push('app.config.ts 未解析到主包页面')

const subPages = []
for (const root of subRoots) {
  const rootIndex = configText.indexOf(`root: '${root}'`)
  const section = configText.slice(rootIndex)
  const nextRoot = section.indexOf("root: '", 10)
  const body = nextRoot > 0 ? section.slice(0, nextRoot) : section
  for (const m of body.matchAll(/^\s+'([\w/-]+\/index)',?\s*$/gm)) subPages.push(`${root}/${m[1]}`)
}

function featureOf(pageFile) {
  const text = readFileSync(pageFile, 'utf8')
  const m = /from\s+'[^']*\/features\/([\w-]+)/.exec(text)
  return m ? m[1] : null
}

for (const page of [...mainPages, ...subPages]) {
  const pageFile = join(src, `${page}.tsx`)
  if (!existsSync(pageFile)) problems.push(`页面缺 index.tsx: ${page}`)
  if (!existsSync(join(src, `${page}.config.ts`))) problems.push(`页面缺 index.config.ts: ${page}`)
  if (!existsSync(join(src, `${page}.scss`)) && existsSync(pageFile)) {
    const feature = featureOf(pageFile)
    if (!feature) {
      problems.push(`页面缺 index.scss，也未迁到 features/（样式不知归属）: ${page}`)
    } else if (!existsSync(join(src, 'features', feature, 'index.scss'))) {
      problems.push(`页面样式迁到 features/${feature}，但那里没有 index.scss: ${page}`)
    }
  }
}

const tabPages = [...configText.matchAll(/pagePath:\s*'([^']+)'/g)].map((m) => m[1])
for (const tab of tabPages) {
  if (!mainPages.includes(tab)) problems.push(`tab 页不在主包 pages 数组: ${tab}`)
}
for (const root of subRoots) {
  if (mainPages.some((p) => p.startsWith(`${root}/`))) problems.push(`tab/主包页面误入分包: ${root}`)
}
notes.push(`主包 ${mainPages.length} 页 / 分包 ${subRoots.length} 个 / tab ${tabPages.length} 个`)

// ---- 3/4. 资产 ----
const assetRefs = new Set()
for (const full of walk(src)) {
  if (!/\.(tsx|ts|scss)$/.test(full)) continue
  const text = readFileSync(full, 'utf8')
  for (const m of text.matchAll(/['"(](\/assets\/[^'")\s]+)['")]/g)) assetRefs.add(m[1])
}
for (const m of configText.matchAll(/(?:iconPath|selectedIconPath):\s*'([^']+)'/g)) {
  assetRefs.add(m[1].startsWith('/') ? m[1] : `/${m[1]}`)
}
for (const ref of assetRefs) {
  if (ref.endsWith('.webp')) problems.push(`引用 webp 资产（微信渲染空白）: ${ref}`)
  if (!existsSync(join(src, ref.replace(/^\//, '')))) problems.push(`引用的本地资产不存在: ${ref}`)
}

// 照片母版目录只允许 jpg（png 母版不进包）；capture/ 是线稿指引图（非照片），
// png 合法；tabBar 图标只允许 png
const PHOTO_DIR = /assets\/(looks|plans|portraits|reports|hair)\//
for (const full of walk(join(src, 'assets'))) {
  const rel = relative(src, full).replace(/\\/g, '/')
  if (PHOTO_DIR.test(rel) && !/\.(jpe?g)$/i.test(rel)) {
    problems.push(`照片目录只允许 jpg: ${rel}`)
  }
  if (/assets\/tabbar\//.test(rel) && !/\.png$/.test(rel)) {
    problems.push(`tabBar 图标只允许 png: ${rel}`)
  }
}

if (existsSync(dist)) {
  for (const full of walk(join(dist, 'assets'))) {
    const rel = relative(dist, full).replace(/\\/g, '/')
    if (rel.endsWith('.webp')) problems.push(`dist 含 webp: ${rel}`)
    if (PHOTO_DIR.test(rel) && /\.png$/i.test(rel)) {
      problems.push(`dist 含 PNG 母版: ${rel}`)
    }
  }
  for (const ref of assetRefs) {
    if (!existsSync(join(dist, ref.replace(/^\//, '')))) {
      problems.push(`dist 缺资产（检查 copy.patterns 与 assetsInlineLimit:0）: ${ref}`)
    }
  }
  for (const m of configText.matchAll(/(?:iconPath|selectedIconPath):\s*'([^']+)'/g)) {
    const rel = m[1].replace(/^\//, '')
    if (!existsSync(join(appRoot, rel))) {
      problems.push(`项目根缺 tabBar 图标（微信会报 dist/app.json iconPath 未找到）: ${rel}`)
    }
  }
} else {
  notes.push('dist 不存在：跳过 3/4 的 dist 侧检查（先执行 build:weapp）')
}

// ---- 8. 主包体积 ----
if (existsSync(dist)) {
  const distFiles = walk(dist)
  let mainBytes = 0
  for (const full of distFiles) {
    const rel = relative(dist, full).replace(/\\/g, '/')
    if (rel.startsWith('packages/')) continue
    mainBytes += statSync(full).size
  }
  const mb = mainBytes / 1024 / 1024
  notes.push(`主包体积 ${mb.toFixed(2)}MB（上限 1.6MB）`)
  if (mb > 1.6) problems.push(`主包超限: ${mb.toFixed(2)}MB > 1.6MB`)
}

for (const note of notes) console.log(`[check] ${note}`)
if (problems.length > 0) {
  for (const problem of problems) console.error(`[check] ✗ ${problem}`)
  console.error(`[check] 失败：${problems.length} 个问题`)
  process.exit(1)
}
console.log('[check] ✅ miniapp 静态门禁通过')
}

// 被测试导入时只提供 checkSourceFile；直接执行才跑全仓门禁。
if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  run()
}
