// 图片出处的唯一投影：谁来决定角标、是否弱化、能不能渲染。
//
// 红线：角标只看 `source_kind` 这个带类型的判别字段——不看 provider、不看 URL、
// 也不看 URL 里有没有 "demo"。URL 快过期/类型不对/是 webp 一律返回 null，
// 由调用点渲染可见空态，绝不隐式回退内置模特图。
import test from 'node:test'
import assert from 'node:assert/strict'
import { projectDisplayMedia } from '../src/media/display.ts'

const media = (patch = {}) => ({
  asset_id: 'asset-1',
  url: 'https://cdn.example/asset-1.jpg?sig=one',
  // 远未来：默认夹具一旦写成"当时的下周"，整组用例会随真实时间静默过期。
  // 过期路径由第 3 个用例显式传 now + 过去的时间戳来覆盖。
  url_expires_at: '2099-01-01T00:00:00Z',
  mime_type: 'image/jpeg',
  source_kind: 'generated_preview',
  display_label: '风格参考',
  ...patch,
})

test('source kind, not provider or url, controls the badge', () => {
  assert.deepEqual(projectDisplayMedia(media()), {
    key: 'asset-1:https://cdn.example/asset-1.jpg?sig=one',
    src: 'https://cdn.example/asset-1.jpg?sig=one',
    badge: '风格参考',
    soften: false,
    sourceKind: 'generated_preview',
  })
  assert.equal(
    projectDisplayMedia(
      media({ source_kind: 'demo_example', display_label: '效果示例' }),
    ).badge,
    '效果示例',
  )
})

test('bundled and demo are softened; generated preview is not', () => {
  assert.equal(projectDisplayMedia(media({ source_kind: 'bundled_reference' })).soften, true)
  assert.equal(projectDisplayMedia(media({ source_kind: 'demo_example' })).soften, true)
  assert.equal(projectDisplayMedia(media({ source_kind: 'generated_preview' })).soften, false)
})

test('invalid, expired and webp media return null without fallback', () => {
  assert.equal(projectDisplayMedia(media({ url: '' })), null)
  assert.equal(projectDisplayMedia(media({ mime_type: 'image/webp' })), null)
  assert.equal(projectDisplayMedia(media({ url: 'https://cdn.example/a.webp' })), null)
  assert.equal(
    projectDisplayMedia(media({ url_expires_at: '2020-01-01T00:00:00Z' }), Date.parse('2026-09-12T08:00:00Z')),
    null,
  )
})

test('generated, bundled and demo assets must be jpeg', () => {
  assert.equal(projectDisplayMedia(media({ mime_type: 'image/png' })), null)
  assert.ok(
    projectDisplayMedia(
      media({
        source_kind: 'user_original',
        display_label: '原本',
        mime_type: 'image/png',
      }),
    ),
  )
})

test('devtools local files render; any other http url stays rejected', () => {
  const local = { source_kind: 'user_original', display_label: '原本' }
  assert.ok(projectDisplayMedia(media({ ...local, url: 'http://tmp/ab12cd.jpg' })))
  assert.ok(projectDisplayMedia(media({ ...local, url: 'http://usr/store/ab12cd' })))
  assert.equal(projectDisplayMedia(media({ ...local, url: 'http://cdn.example/x.jpg' })), null)
})

test('inline image data urls render with a short react key', () => {
  const inlined = projectDisplayMedia(
    media({ source_kind: 'user_original', display_label: '原本', url: 'data:image/jpeg;base64,AAAA' }),
  )
  assert.equal(inlined?.src, 'data:image/jpeg;base64,AAAA')
  // data URL 本体几百 KB，不进 React key
  assert.equal(inlined?.key, 'asset-1:data')
})
