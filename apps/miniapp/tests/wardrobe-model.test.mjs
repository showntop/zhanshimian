// 衣橱 outfit 的运行期归一：契约声明 items 是 required 数组，
// 但服务端 Go nil slice 会序列化成 null——渲染前必须归一，否则 items.map 白屏。
import test from 'node:test'
import assert from 'node:assert/strict'
import { normalizeOutfit } from '../src/features/wardrobe/model.ts'

const outfit = {
  id: 'o1',
  title: '今日组合',
  note: '按今天天气配的',
  item_ids: ['i1', 'i2'],
  worn: false,
}

test('null and undefined items normalize to an empty array', () => {
  assert.deepEqual(normalizeOutfit({ ...outfit, items: null }).items, [])
  assert.deepEqual(normalizeOutfit({ ...outfit, items: undefined }).items, [])
})

test('existing items are preserved and other fields are untouched', () => {
  const items = [{ id: 'i1', name: '米白针织衫' }]
  const next = normalizeOutfit({ ...outfit, items })
  assert.equal(next.items, items)
  assert.equal(next.title, '今日组合')
  assert.equal(next.worn, false)
  assert.deepEqual(next.item_ids, ['i1', 'i2'])
  // 原对象不被改动
  assert.equal(outfit.items, undefined)
})
