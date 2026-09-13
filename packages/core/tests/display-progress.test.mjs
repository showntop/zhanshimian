// 显示进度补间（只追不跳、不回退）
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  advanceDisplayProgress,
  useDisplayProgress,
  createDisplayProgress,
} from '../src/index.ts'

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('advanceDisplayProgress：只追不跳、不回退，500ms 尺度逼近', () => {
  // 追赶方向：不超过 real
  const next = advanceDisplayProgress(0, 80, 500)
  assert.ok(next > 0 && next < 80)
  // 500ms 覆盖约 63% 差距（指数逼近）
  assert.ok(Math.abs(next - 80 * (1 - Math.exp(-1))) < 0.001)
  // 追平后保持
  assert.equal(advanceDisplayProgress(80, 80, 500), 80)
  // real 回落时显示值不动
  assert.equal(advanceDisplayProgress(80, 30, 500), 80)
  // real 超过显示值时单调不减
  const later = advanceDisplayProgress(next, 90, 500)
  assert.ok(later > next && later <= 90)
  // 越界钳制
  assert.equal(advanceDisplayProgress(120, 130, 100), 100)
  assert.equal(advanceDisplayProgress(-5, -1, 100), 0)
})

test('useDisplayProgress 控制器：set(real) 后 onUpdate 单调递增到追平', async () => {
  const values = []
  const handle = useDisplayProgress({ tickMs: 10, onUpdate: (v) => values.push(v) })
  try {
    handle.set(90)
    await sleep(1800) // tau=500ms：1800ms 覆盖约 97%（≈87），未到但已逼近
    const last = handle.get()
    assert.ok(last > 85 && last <= 90, `displayed=${last}`)
    for (let i = 1; i < values.length; i += 1) assert.ok(values[i] >= values[i - 1])
  } finally {
    handle.stop() // 断言失败也必须清理定时器，避免泄漏悬挂进程
  }
})

test('createDisplayProgress：maxRatePerSecond 限制大跳变的追平速度', async () => {
  const handle = createDisplayProgress({ tickMs: 20, maxRatePerSecond: 5 })
  try {
    handle.set(100)
    // 不限速率时 ~300ms 已能追到 45%+；限 5%/s 时 300ms 最多 1.5%
    await sleep(300)
    const displayed = handle.get()
    assert.ok(displayed < 5, `displayed=${displayed} 应受速率上限约束`)
  } finally {
    handle.stop()
  }
})

test('createDisplayProgress：pace 时间推期在真实进度停滞时接管显示', async () => {
  const handle = createDisplayProgress({ tickMs: 20, pace: { ceiling: 90, tauMs: 200 } })
  try {
    handle.set(20)
    await sleep(100)
    handle.set(20) // 真实进度停滞（模拟服务端长时间不上报）
    await sleep(400)
    const displayed = handle.get()
    // tau=200ms：500ms 后推期目标 ≈ 82，追逐（tau=500ms）滞后后显示仍应
    // 明显领先停滞的 20（实测 ~39），且绝不超过封顶
    assert.ok(displayed > 30, `displayed=${displayed} 应被时间推期接管`)
    assert.ok(displayed <= 90, `displayed=${displayed} 不得超过封顶`)
  } finally {
    handle.stop()
  }
})
