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

function lookImage(value, slug = 'natural', variant = 'full') {
  const normalized = shippedAsset(value)
  if (isDisplayableImage(normalized)) return normalized
  return localLook(slug, variant)
}

// Use this for user photos. A missing or invalid URL must stay visibly empty
// instead of silently turning into a stock model image.
function userImage(value) {
  return isDisplayableImage(value) ? value : ''
}

module.exports = { LOCAL_LOOKS, localLook, lookImage, userImage, isDisplayableImage, shippedAsset }
