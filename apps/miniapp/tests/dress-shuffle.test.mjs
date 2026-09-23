// 换装洗牌纯函数回归：维度库/洗牌/保底/发型出池规则/稳定哈希。
// 全部可单测是 presets.ts 的设计约束（不碰组件/Taro）。
import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildRunDims,
  dressTargetFromLock,
  pickPool,
  randomTarget,
  stableHash,
  stableTarget,
  stableVariant,
  COLORS,
  HAIRS,
  LOOKS,
  POOL_SIZE,
  WAISTS,
} from '../src/components/dress-shuffle/presets.ts'

test('stableHash is deterministic and 32-bit unsigned', () => {
  const a = stableHash('seed|2026-9-23')
  assert.equal(a, stableHash('seed|2026-9-23'))
  assert.ok(Number.isInteger(a) && a >= 0 && a <= 0xffffffff)
  assert.notEqual(a, stableHash('seed|2026-9-24'))
})

test('stableVariant splits seeds across both variants and is stable', () => {
  const seen = new Set()
  for (let i = 0; i < 64; i++) seen.add(stableVariant(`seed-${i}`))
  assert.deepEqual([...seen].sort(), ['dress', 'sketch'])
  assert.equal(stableVariant('abc'), stableVariant('abc'))
})

test('stableTarget stays in bounds and deterministic', () => {
  const t = stableTarget('u1|2026-9-23')
  assert.equal(t.look, stableTarget('u1|2026-9-23').look)
  assert.ok(t.look >= 0 && t.look < LOOKS.length)
  assert.ok(t.color >= 0 && t.color < COLORS.length)
  assert.ok(t.waist >= 0 && t.waist < WAISTS.length)
  assert.ok(t.hair >= 0 && t.hair < HAIRS.length)
})

test('pickPool always contains the target with the requested size', () => {
  for (const [len, n, targetIdx] of [
    [LOOKS.length, POOL_SIZE.look, 3],
    [COLORS.length, POOL_SIZE.color, 0],
    [3, POOL_SIZE.waist, 2],
  ]) {
    const pool = pickPool(len, n, targetIdx)
    assert.equal(pool.length, Math.min(n, len))
    assert.ok(pool.includes(targetIdx))
    assert.equal(new Set(pool).size, pool.length, 'pool entries are distinct')
  }
})

test('buildRunDims keeps target inside every dim pool', () => {
  const target = { look: 0, color: 5, waist: 1, hair: 2 }
  const dims = buildRunDims(target)
  assert.ok(dims.length > 0)
  for (const dim of dims) {
    assert.ok(dim.target >= 0 && dim.target < dim.items.length)
  }
})

test('hair dim only enters the pool for hero look', () => {
  const hero = { look: 0, color: 1, waist: 0, hair: 0 }
  const other = { look: 3, color: 1, waist: 0, hair: 0 }
  assert.ok(buildRunDims(hero).some((d) => d.key === 'hair'))
  assert.ok(!buildRunDims(other).some((d) => d.key === 'hair'))
})

test('hairAvailable=false excludes the hair dim even for hero look (spec §4)', () => {
  const hero = { look: 0, color: 1, waist: 0, hair: 0 }
  for (let i = 0; i < 12; i++) {
    const dims = buildRunDims(hero, { hairAvailable: false })
    assert.ok(!dims.some((d) => d.key === 'hair'))
  }
})

test('order option fixes dim sequence for the settling round', () => {
  const t = { look: 0, color: 1, waist: 0, hair: 0 }
  const dims = buildRunDims(t, { order: ['look', 'color', 'waist', 'hair'] })
  assert.deepEqual(
    dims.map((d) => d.key),
    ['look', 'color', 'waist', 'hair'],
  )
})

test('randomTarget stays in bounds', () => {
  for (let i = 0; i < 32; i++) {
    const t = randomTarget()
    assert.ok(t.look >= 0 && t.look < LOOKS.length)
    assert.ok(t.color >= 0 && t.color < COLORS.length)
    assert.ok(t.waist >= 0 && t.waist < WAISTS.length)
    assert.ok(t.hair >= 0 && t.hair < HAIRS.length)
  }
})

test('dressTargetFromLock maps semantic values to library indices', () => {
  const t = dressTargetFromLock({ look: 'ratio', color: '砖红', waist: '低腰', hair: 'bun' })
  assert.equal(t?.look, LOOKS.findIndex((l) => l.key === 'ratio'))
  assert.equal(t?.color, COLORS.findIndex((c) => c.name === '砖红'))
  assert.equal(t?.waist, WAISTS.findIndex((w) => w.name === '低腰'))
  assert.equal(t?.hair, HAIRS.findIndex((h) => h.key === 'bun'))
})

test('dressTargetFromLock returns null when vocabularies mismatch (fallback to old reveal)', () => {
  assert.equal(dressTargetFromLock({ look: 'future-look', color: '砖红', waist: '高腰', hair: 'wave' }), null)
  assert.equal(dressTargetFromLock({ look: 'outfit', color: '克莱因蓝', waist: '高腰', hair: 'wave' }), null)
})
