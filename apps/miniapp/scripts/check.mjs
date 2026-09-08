#!/usr/bin/env node
/**
 * 小程序静态门禁（CI 必跑）：
 *  1. app.config.ts 的 pages/subPackages ↔ src 目录一一对应
 *  2. 每页三件套（index.tsx/.scss/.config.ts）齐全
 *  3. 引用的 /assets/ 本地资源在 src 与 dist 中存在
 *  4. dist/assets 无 .webp；模特图目录无 .png 母版
 *  5. .scss 禁止裸 px（rpx / CSS 变量 / 1rpx 发丝线除外；注释行忽略）
 *  6. Image src 字面量必须经媒体契约组件（example-image / 例图常量）
 *  7. 禁止字面量 http:// 图片地址
 *  8. 主包体积（dist 除 packages/ 外）≤ 1.6MB
 */
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const appRoot = join(import.meta.dirname, '..')
const src = join(appRoot, 'src')
const dist = join(appRoot, 'dist')
const problems = []
const notes = []

// ---- 1/2. 路由 ↔ 目录 ----
const configText = readFileSync(join(src, 'app.config.ts'), 'utf8')
// pages 数组在 subPackages 之前；只取该段的 'xxx/index' 条目
const pagesSection = configText.split('subPackages')[0] ?? configText
const mainPages = [...pagesSection.matchAll(/^\s*'([\w/-]+\/index)',?\s*$/gm)].map((m) => m[1])
const subRoots = [...configText.matchAll(/root:\s*'([^']+)'/g)].map((m) => m[1])
if (mainPages.length === 0) problems.push('app.config.ts 未解析到主包页面')

// 分包页面：root + pages 段内的条目
const subPages = []
for (const root of subRoots) {
  const rootIndex = configText.indexOf(`root: '${root}'`)
  const section = configText.slice(rootIndex)
  const nextRoot = section.indexOf("root: '", 10)
  const body = nextRoot > 0 ? section.slice(0, nextRoot) : section
  for (const m of body.matchAll(/^\s+'([\w/-]+\/index)',?\s*$/gm)) subPages.push(`${root}/${m[1]}`)
}

for (const page of [...mainPages, ...subPages]) {
  // Taro 页面路径即文件基名（pages/home/index → src/pages/home/index.tsx）
  if (!existsSync(join(src, `${page}.tsx`))) problems.push(`页面缺 index.tsx: ${page}`)
  if (!existsSync(join(src, `${page}.scss`))) problems.push(`页面缺 index.scss: ${page}`)
  if (!existsSync(join(src, `${page}.config.ts`))) problems.push(`页面缺 index.config.ts: ${page}`)
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
function walk(dir, out = []) {
  if (!existsSync(dir)) return out
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) walk(full, out)
    else out.push(full)
  }
  return out
}

const assetRefs = new Set()
function scanDir(dir) {
  for (const full of walk(dir)) {
    if (!/\.(tsx|ts|scss)$/.test(full)) continue
    const text = readFileSync(full, 'utf8')
    for (const m of text.matchAll(/['"(](\/assets\/[^'")\s]+)['")]/g)) assetRefs.add(m[1])
  }
}
scanDir(src)
for (const ref of assetRefs) {
  if (ref.endsWith('.webp')) problems.push(`引用 webp 资产（微信渲染空白）: ${ref}`)
  if (!existsSync(join(src, ref.replace(/^\//, '')))) problems.push(`引用的本地资产不存在: ${ref}`)
}

if (existsSync(dist)) {
  for (const full of walk(join(dist, 'assets'))) {
    const rel = relative(dist, full)
    if (rel.endsWith('.webp')) problems.push(`dist 含 webp: ${rel}`)
    if (/assets\/(looks|plans|portraits|reports|hair)\/.*\.png$/.test(rel.replace(/\\/g, '/'))) {
      problems.push(`dist 含 PNG 母版: ${rel}`)
    }
  }
  for (const ref of assetRefs) {
    if (!existsSync(join(dist, ref.replace(/^\//, '')))) {
      problems.push(`dist 缺资产（检查 copy.patterns 与 assetsInlineLimit:0）: ${ref}`)
    }
  }
} else {
  notes.push('dist 不存在：跳过 3/4 的 dist 侧检查（先执行 build:weapp）')
}

// ---- 5. scss 裸 px（布局尺寸须 rpx；阴影/模糊的视觉 px 有意保留） ----
for (const full of walk(src)) {
  if (!full.endsWith('.scss')) continue
  const rel = relative(src, full)
  const lines = readFileSync(full, 'utf8').split('\n')
  lines.forEach((line, i) => {
    const code = line.replace(/\/\/.*$/, '')
    if (/^\s*(box-shadow|text-shadow|backdrop-filter|filter)/.test(code)) return
    const barePx = /(^|[\s:,(])-\d+px(?![a-z])/i.exec(code) || /(^|[\s:,(])\d+px(?![a-z])/i.exec(code)
    if (barePx) problems.push(`scss 裸 px（须 rpx）: ${rel}:${i + 1} → ${line.trim()}`)
  })
}

// ---- 6/7. Image src 纪律 ----
for (const full of walk(src)) {
  if (!full.endsWith('.tsx') || full.includes('components/example-image')) continue
  const rel = relative(src, full)
  const text = readFileSync(full, 'utf8')
  if (/src=\{?["']https?:\/\/[^"']+["']/.test(text)) {
    problems.push(`Image src 使用字面量 http(s) URL（须经 example-image 或 API 数据）: ${rel}`)
  }
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
