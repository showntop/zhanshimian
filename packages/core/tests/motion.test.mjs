import test from 'node:test'
import assert from 'node:assert/strict'
import {
  planConverge,
  readConvergeParams,
  readDressLockParams,
  reducedPlan,
  planDuration,
  roamThemes,
  roamPerThemeMS,
  readFramesParams,
} from '../src/daily/motion.ts'

// form 的视觉空间：调度器不认识它，只从外部拿 counts 与 resolve。
const VARIANTS = {
  top: ['cropped', 'regular', 'long', 'oversize'],
  bottom: ['wide', 'straight', 'slim', 'long'],
  palette: ['clash', 'mixed', 'harmony'],
}
const AXES = [
  { id: 'top', brake_at: 1, target: 'cropped', counter: 'long' },
  { id: 'bottom', brake_at: 0.85, target: 'long', counter: 'wide' },
  { id: 'palette', brake_at: 0.7, target: 'harmony', counter: 'clash' },
]
const counts = AXES.map((a) => VARIANTS[a.id].length)
const resolve = (id, value) => {
  const index = VARIANTS[id].indexOf(value)
  return index < 0 ? 0 : index
}
const targets = AXES.map((a) => resolve(a.id, a.target))
const rand = () => 0.9 // 确定性随机源

const params = (over = {}) => ({
  axes: AXES,
  tempo: { steps: 16, interval: [90, 300] },
  ...over,
})

test('定格的那一步就是 target（动画终点 = 建议本身）', () => {
  const plan = planConverge(params(), counts, resolve, rand)
  const lock = plan[plan.length - 1]
  assert.equal(lock.phase, 'lock')
  assert.deepEqual(lock.indices, targets)
})

test('收敛幅度单调衰减到 0', () => {
  const plan = planConverge(params(), counts, resolve, rand)
  const chaos = plan.filter((s) => s.phase === 'chaos').map((s) => s.amp)
  assert.ok(chaos.length > 1)
  for (let i = 1; i < chaos.length; i++) assert.ok(chaos[i] <= chaos[i - 1])
  assert.equal(chaos[chaos.length - 1] < chaos[0], true)
})

test('轴按 brake_at 依次锁死：先配色，再下装，最后上身', () => {
  const plan = planConverge(params(), counts, resolve, rand)
  const chaos = plan.filter((s) => s.phase === 'chaos')
  const lockedFrom = (axisIndex) => {
    const brakeAt = AXES[axisIndex].brake_at
    return chaos.filter((s, i) => i / (chaos.length - 1) >= brakeAt)
  }
  // 过了各自的 brake_at 之后，该轴必须恒等于 target
  for (const [i] of AXES.entries()) {
    for (const step of lockedFrom(i)) assert.equal(step.indices[i], targets[i])
  }
  // 早期步骤里 brake_at 最小的轴（palette, 0.7）会比上身穿(1.0)先稳定
  const paletteLocked = lockedFrom(2).length
  const topLocked = lockedFrom(0).length
  assert.ok(paletteLocked > topLocked)
})

test('戏剧停顿让时间更长，但终点不变', () => {
  const withTheatre = planConverge(params({ theatrical: true }), counts, resolve, rand)
  const plain = planConverge(params({ theatrical: false }), counts, resolve, rand)
  assert.ok(planDuration(withTheatre) > planDuration(plain))
  assert.deepEqual(withTheatre[withTheatre.length - 1].indices, plain[plain.length - 1].indices)
})

test('过冲那一步越过 target 半格，随后回弹', () => {
  const plan = planConverge(params(), counts, resolve, rand)
  const over = plan.find((s) => s.phase === 'overshoot')
  assert.ok(over)
  assert.notDeepEqual(over.indices, targets)
  const lock = plan[plan.length - 1]
  assert.ok(lock.at > over.at)
  assert.deepEqual(lock.indices, targets)
})

test('减动效：整条时间轴塌成一步，直接定格', () => {
  const plan = reducedPlan(planConverge(params(), counts, resolve, rand))
  assert.equal(plan.length, 1)
  assert.equal(plan[0].at, 0)
  assert.deepEqual(plan[0].indices, targets)
})

test('契约缺字段时防御式解析，不崩', () => {
  const empty = readConvergeParams(undefined)
  assert.deepEqual(empty.axes, [])
  assert.ok(empty.tempo.steps > 0)
  assert.equal(planConverge(empty, counts, resolve, rand).length, 0)

  const broken = readConvergeParams({
    phase: 'settle',
    kind: 'converge',
    params: { axes: [{ id: 'top' }, null, { noId: true }], tempo: { steps: 999, interval: [1] } },
  })
  assert.equal(broken.axes.length, 1)
  assert.equal(broken.axes[0].brake_at, 1)
  assert.ok(broken.tempo.steps <= 40)
  assert.deepEqual(broken.tempo.interval.length, 2)
})

test('巡游主题解析与兜底', () => {
  const themes = roamThemes({
    phase: 'roam',
    kind: 'roam_tour',
    params: { themes: [{ theme: 'color', form: 'swatch_bars', label: '在看颜色' }, { bad: true }] },
  })
  assert.equal(themes.length, 1)
  assert.equal(themes[0].label, '在看颜色')
  assert.deepEqual(roamThemes(undefined), [])
  assert.equal(roamPerThemeMS(undefined), 4000)
})

test('序列帧参数：正常解析 + 缺字段防御 + 空值返回 null', () => {
  const stage = {
    phase: 'settle',
    kind: 'frames',
    params: {
      urls: ['https://x/f_01.jpg', 'https://x/f_02.jpg', '', 42],
      interval_ms: 108,
      hold_ms: 320,
    },
  }
  const frames = readFramesParams(stage)
  assert.equal(frames.urls.length, 2)
  assert.equal(frames.intervalMS, 108)
  assert.equal(frames.holdMS, 320)

  // interval/hold 缺失或非法 → 原型调参的默认值
  const defaulted = readFramesParams({ phase: 'settle', kind: 'frames', params: { urls: ['a'] } })
  assert.equal(defaulted.intervalMS, 108)
  assert.equal(defaulted.holdMS, 320)

  // urls 缺失 / 非数组 / 全空：返回 null，播放器据此直接揭晓
  assert.equal(readFramesParams(undefined), null)
  assert.equal(readFramesParams({ phase: 'settle', kind: 'frames', params: {} }), null)
  assert.equal(readFramesParams({ phase: 'settle', kind: 'frames', params: { urls: [] } }), null)
  assert.equal(readFramesParams({ phase: 'settle', kind: 'frames', params: { urls: 'nope' } }), null)
})

// ---------- dress_lock ----------

test('dress_lock：完整参数解析出语义 target 与素材位', () => {
  const stage = {
    phase: 'settle',
    kind: 'dress_lock',
    params: {
      target: { look: 'outfit', color: '砖红', waist: '高腰', hair: 'wave' },
      pace: 'slow',
      assets: { base: 'https://cdn.example.com', version: '20260923' },
    },
  }
  const parsed = readDressLockParams(stage)
  assert.deepEqual(parsed?.target, { look: 'outfit', color: '砖红', waist: '高腰', hair: 'wave' })
  assert.equal(parsed?.pace, 'slow')
  assert.equal(parsed?.assets.base, 'https://cdn.example.com')
})

test('dress_lock：缺语义值 / kind 不对 / 无参数一律 null（回落旧揭晓线）', () => {
  assert.equal(readDressLockParams({ phase: 'settle', kind: 'frames', params: {} }), null)
  assert.equal(readDressLockParams({ phase: 'settle', kind: 'dress_lock' }), null)
  assert.equal(readDressLockParams(undefined), null)
  assert.equal(
    readDressLockParams({
      phase: 'settle',
      kind: 'dress_lock',
      params: { target: { look: 'outfit', color: '', waist: '高腰', hair: 'wave' } },
    }),
    null,
  )
})

test('dress_lock：pace 缺省回落 normal，assets 缺省空串', () => {
  const parsed = readDressLockParams({
    phase: 'settle',
    kind: 'dress_lock',
    params: { target: { look: 'ratio', color: '砖红', waist: '低腰', hair: 'bob' } },
  })
  assert.equal(parsed?.pace, 'normal')
  assert.deepEqual(parsed?.assets, { base: '', version: '' })
})
