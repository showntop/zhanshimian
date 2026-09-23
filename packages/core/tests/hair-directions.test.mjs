// 发型方向目录契约：性别分组、服务端目录与本包目录的合并规则、参考图的诚实性。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  CUSTOM_DIRECTION_ID,
  HAIR_COPY,
  HAIR_DIRECTIONS,
  HAIR_GENDERS,
  hairDirectionViews,
} from '../src/index.ts'

const FORBIDDEN = ['通勤', '颜值', '评分', '缺陷', '丑']

test('方向目录按性别分组，每一侧都有可选方向且都带差异说明', () => {
  assert.deepEqual([...HAIR_GENDERS], ['women', 'men'])
  for (const gender of HAIR_GENDERS) {
    const views = hairDirectionViews(gender, [])
    assert.ok(views.length >= 3, `${gender} 可选方向太少：${views.length}`)
    for (const view of views) {
      assert.ok(view.name.length > 0 && view.tag?.length > 0 && view.desc?.length > 0, `${gender} 方向缺名称/标签/说明`)
    }
  }
})

test('方向名就是生成提示词：不出现红线禁词，且不与自定义 id 撞车', () => {
  for (const item of HAIR_DIRECTIONS) {
    assert.notEqual(item.id, CUSTOM_DIRECTION_ID)
    for (const word of FORBIDDEN) {
      assert.ok(!item.name.includes(word) && !item.desc.includes(word), `方向「${item.name}」出现禁词「${word}」`)
    }
  }
})

test('参考图不跨性别：每一侧的 slug 都是本侧自己的素材', () => {
  // 两侧都要看得见发型长什么样（不盲选），但男士只能拿 men-*、女士只能拿 women 三款
  for (const gender of HAIR_GENDERS) {
    const views = hairDirectionViews(gender, [])
    for (const view of views) {
      assert.ok(view.slug, `${gender} 方向「${view.name}」缺参考图，选择会变成盲选`)
      const isMenAsset = String(view.slug).startsWith('men-')
      assert.equal(isMenAsset, gender === 'men', `方向「${view.name}」的参考图跨性别了：${view.slug}`)
    }
  }
  // 男士三张与女士三张不共用同一份素材
  const womenSlugs = new Set(hairDirectionViews('women', []).map((view) => view.slug))
  for (const view of hairDirectionViews('men', [])) {
    assert.ok(!womenSlugs.has(view.slug), `男士方向借用了女士素材：${view.slug}`)
  }
})

test('合并规则：服务端条目在前、unisex 两侧都出现、同 id 不重复', () => {
  const server = [
    { id: 'sharp', name: '服务端锁骨发', media: { asset_id: 'a' }, gender: 'women' },
    { id: 'unisex-basic', name: '通用款', media: { asset_id: 'b' }, gender: 'unisex' },
    { id: 'men-only', name: '男士专属', media: { asset_id: 'c' }, gender: 'men' },
  ]

  const women = hairDirectionViews('women', server)
  assert.deepEqual(women.slice(0, 2).map((view) => view.id), ['sharp', 'unisex-basic'])
  assert.equal(women.filter((view) => view.id === 'sharp').length, 1)
  assert.equal(women[0].name, '服务端锁骨发')
  assert.ok(women[0].media, '服务端下发的图要带上')
  assert.ok(!women.some((view) => view.id === 'men-only'))

  const men = hairDirectionViews('men', server)
  assert.deepEqual(men.slice(0, 2).map((view) => view.id), ['unisex-basic', 'men-only'])
  assert.ok(!men.some((view) => view.id === 'sharp'), '女士专属方向不得出现在男士一侧')
})

test('性别缺省的目录条目按 unisex 处理（缺字段的服务端不该整条消失）', () => {
  const women = hairDirectionViews('women', [{ id: 'legacy', name: '老目录条目', media: null }])
  const men = hairDirectionViews('men', [{ id: 'legacy', name: '老目录条目', media: null }])
  assert.ok(women.some((view) => view.id === 'legacy') && men.some((view) => view.id === 'legacy'))
})

test('自定义方向的输入层文案齐备', () => {
  const keys = [
    'genderLabel', 'genderWomen', 'genderMen',
    'customName', 'customTitle', 'customLabel', 'customPlaceholder',
    'customHelper', 'customCta', 'customEmpty', 'customTooLong',
  ]
  for (const key of keys) {
    assert.ok(HAIR_COPY[key]?.length > 0, `缺文案 ${key}`)
  }
})
