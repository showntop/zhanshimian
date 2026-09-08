// 文案红线测试：不出现「通勤」「颜值」「评分」；场景四席；空态必带下一步动作
import test from 'node:test'
import assert from 'node:assert/strict'
import { ERROR_COPY, EMPTY_COPY, SCENES, greetingByHour, FEEDBACK_WORDS, HOME_TITLE, PRIVACY_NOTE } from '../src/index.ts'

const FORBIDDEN = ['通勤', '颜值', '评分']

function allStrings(value) {
  if (typeof value === 'string') return [value]
  if (Array.isArray(value)) return value.flatMap(allStrings)
  if (value && typeof value === 'object') return Object.values(value).flatMap(allStrings)
  return []
}

test('文案红线：全量文案不出现禁词', () => {
  const greetings = Array.from({ length: 24 }, (_, hour) => greetingByHour(hour))
  const corpus = allStrings({ ERROR_COPY, EMPTY_COPY, SCENES, FEEDBACK_WORDS, HOME_TITLE, PRIVACY_NOTE, greetings })
  for (const text of corpus) {
    for (const word of FORBIDDEN) assert.ok(!text.includes(word), `文案出现禁词「${word}」：${text}`)
  }
})

test('场景固定四席：面试/婚礼/约会/日常', () => {
  assert.deepEqual(SCENES.map((s) => s.label), ['面试', '婚礼', '约会', '日常'])
  assert.deepEqual(SCENES.map((s) => s.id), ['interview', 'wedding', 'date', 'daily'])
})

test('空态文案必须给出下一步动作（history 允许空动作除外）', () => {
  for (const [key, item] of Object.entries(EMPTY_COPY)) {
    if (key === 'history') continue
    assert.ok(item.action.length > 0, `空态 ${key} 缺少下一步动作`)
    assert.ok(item.title.length > 0 && item.body.length > 0)
  }
})

test('问候语分时段 + 首页标题保留', () => {
  assert.equal(greetingByHour(3), '夜深了')
  assert.equal(greetingByHour(8), '早上好')
  assert.equal(greetingByHour(10), '上午好')
  assert.equal(greetingByHour(13), '中午好')
  assert.equal(greetingByHour(15), '下午好')
  assert.equal(greetingByHour(22), '晚上好')
  assert.equal(HOME_TITLE, '你好，我是你的私人形象顾问')
})
