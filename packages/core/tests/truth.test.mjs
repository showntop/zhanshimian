// 媒体真实性契约测试（验收标准 7：至少 4 断言）
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  lookImage,
  lookVideo,
  userImage,
  exampleImage,
  isBundledAsset,
  shippedAsset,
  isDisplayableImage,
  LOCAL_LOOK_SLUGS,
  LOOK_VARIANTS,
  setLocalLooksResolver
} from '../src/media/truth.ts'

test('lookImage: 空值/非法输入严格返回空串，绝不隐式回退内置图', () => {
  assert.equal(lookImage(''), '')
  assert.equal(lookImage(null), '')
  assert.equal(lookImage(undefined), '')
  // webp 一律拦截 —— 除非是内置资产母版（由 shippedAsset 改写成 jpg，见下个用例）
  assert.equal(lookImage('https://cdn.example.com/a.webp'), '')
  assert.equal(lookImage('https://cdn.example.com/a.webp?v=2'), '')
  assert.equal(lookImage('not-a-url'), '')
})

test('lookImage: 合法 https / 包内 jpg 通过', () => {
  assert.equal(lookImage('https://cdn.example.com/a.jpg'), 'https://cdn.example.com/a.jpg')
  assert.equal(lookImage('/assets/looks/natural.jpg'), '/assets/looks/natural.jpg')
})

test('shippedAsset: 内置资产的 png/webp 母版改写为同名 jpg', () => {
  assert.equal(shippedAsset('/assets/looks/natural.png'), '/assets/looks/natural.jpg')
  assert.equal(shippedAsset('/assets/plans/sharp.webp'), '/assets/plans/sharp.jpg')
  assert.equal(shippedAsset('/assets/portraits/warm.png'), '/assets/portraits/warm.jpg')
  assert.equal(shippedAsset('/assets/reports/natural.webp'), '/assets/reports/natural.jpg')
  assert.equal(shippedAsset('/assets/hair/natural.png'), '/assets/hair/natural.jpg')
  // 非内置路径与已是 jpg 的不改写
  assert.equal(shippedAsset('https://x/a.png'), 'https://x/a.png')
  assert.equal(shippedAsset('/assets/looks/natural.jpg'), '/assets/looks/natural.jpg')
  // 改写后 webp 母版经 lookImage 变为可渲染的 jpg
  assert.equal(lookImage('/assets/looks/natural.webp'), '/assets/looks/natural.jpg')
})

test('isBundledAsset: 命中五个内置目录（含改写后的母版）', () => {
  assert.equal(isBundledAsset('/assets/looks/natural.jpg'), true)
  assert.equal(isBundledAsset('/assets/plans/sharp.jpg'), true)
  assert.equal(isBundledAsset('/assets/portraits/warm.png'), true)
  assert.equal(isBundledAsset('/assets/reports/natural.webp'), true)
  assert.equal(isBundledAsset('/assets/hair/natural.jpg'), true)
  assert.equal(isBundledAsset('https://cdn.example.com/a.jpg'), false)
  assert.equal(isBundledAsset('/assets/icons/home.png'), false)
})

test('userImage: 无效保持可见的空，有效原样返回', () => {
  assert.equal(userImage(''), '')
  assert.equal(userImage(undefined), '')
  assert.equal(userImage('wxfile://tmp/1.jpg'), 'wxfile://tmp/1.jpg')
  assert.equal(userImage('file:///var/x.jpg'), 'file:///var/x.jpg')
  assert.equal(userImage('https://x/1.jpg'), 'https://x/1.jpg')
  // 注意：webp 对用户照片不做 lookImage 式拦截（真实上传不会出现 webp）
})

test('exampleImage: 唯一内置图入口，未知 slug/variant 回落 natural/full', () => {
  assert.equal(exampleImage('natural', 'full'), '/assets/looks/natural.jpg')
  assert.equal(exampleImage('sharp', 'hair'), '/assets/hair/sharp.jpg')
  assert.equal(exampleImage('warm', 'plan'), '/assets/plans/warm.jpg')
  assert.equal(exampleImage('unknown', 'full'), '/assets/looks/natural.jpg')
  assert.equal(exampleImage('natural', 'unknown'), '/assets/looks/natural.jpg')
  assert.equal(exampleImage(), '/assets/looks/natural.jpg')
})

test('exampleImage: 经注入的 LocalLooksResolver 解析（平台各自实现）', () => {
  setLocalLooksResolver({ resolve: (slug, variant) => `rn://${slug}-${variant}` })
  assert.equal(exampleImage('sharp', 'report'), 'rn://sharp-report')
  assert.equal(exampleImage('bad', 'bad'), 'rn://natural-full')
  // 还原默认解析器，避免影响其他用例
  setLocalLooksResolver({ resolve: (slug, variant) => `/assets/${variant === 'full' ? 'looks' : variant}s/${slug}.jpg` })
})

test('lookVideo: 空/WebP/WebM/非法拒绝，MP4 协议地址通过', () => {
  assert.equal(lookVideo(''), '')
  assert.equal(lookVideo(null), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webp'), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webm'), '')
  assert.equal(lookVideo('https://cdn.example.com/a.webm?v=1'), '')
  assert.equal(lookVideo('not-a-url'), '')
  assert.equal(lookVideo('https://cdn.example.com/orbit.mp4'), 'https://cdn.example.com/orbit.mp4')
  assert.equal(lookVideo('http://127.0.0.1:58000/uploads/u/orbit.mp4'), 'http://127.0.0.1:58000/uploads/u/orbit.mp4')
  assert.equal(lookVideo('wxfile://tmp/orbit.mp4'), 'wxfile://tmp/orbit.mp4')
})

test('常量与 isDisplayableImage 契约', () => {
  assert.deepEqual([...LOCAL_LOOK_SLUGS], ['natural', 'sharp', 'warm'])
  assert.deepEqual([...LOOK_VARIANTS], ['full', 'portrait', 'report', 'hair', 'plan'])
  assert.equal(isDisplayableImage('http://dev.local:58000/a.jpg'), true)
  assert.equal(isDisplayableImage('ftp://x/a.jpg'), false)
})
