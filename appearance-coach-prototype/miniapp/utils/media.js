// 图片真实性约定(Phase 0 数据真实性清理):
// - 用户本人照片用 userImage():URL 无效一律返回 '',保持可见的空。
// - API 下发的方案图/效果图/单品图用 lookImage():URL 无效(空、非法、旧 webp)
//   同样返回 '',绝不隐式回退到内置模特图。调用点必须显式二选一:
//   显示占位/空态,或调用 exampleImage() 并在 UI 上叠加「风格参考」/「示例」
//   角标(公共样式 .example-badge,见 app.wxss)。
// - exampleImage() 是唯一返回内置模特图的入口;任何非用户本人照片都必须在 UI
//   上有「风格参考」或「示例」标注,不允许隐式回退。示例图同时叠加 .example-soft
//   (模糊弱化,见 app.wxss),与本人生成结果拉开视觉区分度。
const LOCAL_LOOKS = {
  natural: {
    full: '/assets/looks/natural.jpg',
    portrait: '/assets/portraits/natural.jpg',
    report: '/assets/reports/natural.jpg',
    hair: '/assets/hair/natural.jpg',
    plan: '/assets/plans/natural.jpg'
  },
  sharp: {
    full: '/assets/looks/sharp.jpg',
    portrait: '/assets/portraits/sharp.jpg',
    report: '/assets/reports/sharp.jpg',
    hair: '/assets/hair/sharp.jpg',
    plan: '/assets/plans/sharp.jpg'
  },
  warm: {
    full: '/assets/looks/warm.jpg',
    portrait: '/assets/portraits/warm.jpg',
    report: '/assets/reports/warm.jpg',
    hair: '/assets/hair/warm.jpg',
    plan: '/assets/plans/warm.jpg'
  }
}

function isDisplayableImage(value) {
  if (typeof value !== 'string' || !value) return false
  return value.startsWith('https://') || value.startsWith('http://') || value.startsWith('/assets/') || value.startsWith('wxfile://') || value.startsWith('file://')
}

// PNG masters and older .webp fixture paths are intentionally excluded from
// the mini-program package. Keep API responses from older reports usable by
// routing bundled look assets to the shipped JPEG with the same basename.
function shippedAsset(value) {
  if (typeof value !== 'string') return value
  if (/^\/assets\/(looks|plans|portraits|reports|hair)\/[a-z0-9_-]+\.(png|webp)$/i.test(value)) {
    return value.replace(/\.(png|webp)$/i, '.jpg')
  }
  return value
}

function localLook(slug = 'natural', variant = 'full') {
  const look = LOCAL_LOOKS[slug] || LOCAL_LOOKS.natural
  return look[variant] || look.full
}

// 严格模式:只返回真实可用的 API/本地图片 URL。空、非法、以及未被
// shippedAsset 转换的旧 webp 引用一律返回 '',由调用点决定占位或显式示例图。
function lookImage(value) {
  const normalized = shippedAsset(value)
  if (typeof normalized === 'string' && /\.webp(\?\S*)?$/i.test(normalized)) return ''
  return isDisplayableImage(normalized) ? normalized : ''
}

// 显式示例图:仅用于「风格参考」场景,调用点必须在对应图片上叠加
// 「风格参考」/「示例」角标(.example-badge)。
function exampleImage(slug = 'natural', variant = 'full') {
  return localLook(slug, variant)
}

// 服务端下发的 /assets/(looks|plans|portraits|reports|hair)/* 是与小程序包内
// 同源的内置模特素材(包括服务端 demo 数据),不是用户本人照片也不是 AI 生成
// 效果图。命中时必须按示例图对待(叠加「风格参考」角标),不论 URL 是否可渲染。
function isBundledAsset(value) {
  const normalized = shippedAsset(value)
  return typeof normalized === 'string' && /^\/assets\/(looks|plans|portraits|reports|hair)\//i.test(normalized)
}

// Use this for user photos. A missing or invalid URL must stay visibly empty
// instead of silently turning into a stock model image.
function userImage(value) {
  return isDisplayableImage(value) ? value : ''
}

module.exports = { LOCAL_LOOKS, localLook, lookImage, exampleImage, isBundledAsset, userImage, isDisplayableImage, shippedAsset }
