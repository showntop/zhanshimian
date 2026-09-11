import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const appRoot = join(import.meta.dirname, '..')
const homeSource = readFileSync(join(appRoot, 'src/pages/home/index.tsx'), 'utf8')
const homeScss = readFileSync(join(appRoot, 'src/pages/home/index.scss'), 'utf8')

assert.match(
  homeSource,
  /home__preview-image[\s\S]*slug="natural"[\s\S]*home__preview-image[\s\S]*slug="sharp"/,
  '新用户 Hero 应使用已标识的真实风格参考图讲述“照片到方案”',
)

assert.match(
  homeSource,
  /home__tool--lead[\s\S]*home__tool-visual[\s\S]*<ExampleImage/,
  '推荐工具大卡应有真实照片焦点，不能只剩空白底色',
)

assert.match(homeSource, /\{SCENES\.map\(/, '场景区应使用统一非对称网格，不应拆成两个同构行')

const toolBlock = homeScss.indexOf('  &__tool {')
const leadModifier = homeScss.indexOf('    &--lead {', toolBlock)
const baseDisplay = homeScss.indexOf('    display: flex;', toolBlock)
assert.ok(
  toolBlock >= 0 && baseDisplay > toolBlock && baseDisplay < leadModifier,
  '工具卡基础样式必须先于 lead 修饰样式，避免修饰规则被基础规则覆盖',
)

assert.match(homeScss, /&__scenes\s*\{[\s\S]*grid-template-columns:/, '场景区应有明确的非对称网格')
assert.match(
  homeScss,
  /&__steps\s*\{[\s\S]*margin-top:\s*24rpx[\s\S]*padding-top:\s*20rpx/,
  '建档步骤区应压缩垂直留白',
)
assert.match(
  homeScss,
  /&__step\s*\{[\s\S]*flex-direction:\s*row[\s\S]*align-items:\s*flex-start/,
  '建档步骤应使用编号与文案横排的紧凑结构',
)
assert.match(
  homeSource,
  /className="home__tool-visual"[\s\S]*mode="aspectFit"/,
  '发型推荐图必须完整显示头部',
)
assert.match(
  homeScss,
  /&--interview\s*\{[\s\S]*background:\s*#d7e0d2[\s\S]*\.home__scene-visual\s*\{[\s\S]*opacity:\s*0\.28/,
  '面试卡应使用更明确的冷绿层次和图形对比',
)
assert.match(
  homeScss,
  /&--daily\s*\{[\s\S]*background:\s*#d5dfcf[\s\S]*\.home__scene-visual\s*\{[\s\S]*opacity:\s*0\.26/,
  '日常卡应使用更明确的苔绿层次和图形对比',
)

console.log('[home-visual-contract] ✅ 首页视觉结构契约通过')
