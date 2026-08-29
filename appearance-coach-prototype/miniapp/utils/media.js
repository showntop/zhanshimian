const LOCAL_LOOKS = {
  natural: {
    full: '/assets/looks/natural.webp',
    portrait: '/assets/portraits/natural.webp',
    report: '/assets/reports/natural.webp',
    hair: '/assets/hair/natural.webp',
    plan: '/assets/plans/natural.webp'
  },
  sharp: {
    full: '/assets/looks/sharp.webp',
    portrait: '/assets/portraits/sharp.webp',
    report: '/assets/reports/sharp.webp',
    hair: '/assets/hair/sharp.webp',
    plan: '/assets/plans/sharp.webp'
  },
  warm: {
    full: '/assets/looks/warm.webp',
    portrait: '/assets/portraits/warm.webp',
    report: '/assets/reports/warm.webp',
    hair: '/assets/hair/warm.webp',
    plan: '/assets/plans/warm.webp'
  }
}

function isUsableExternalImage(value) {
  return value && !value.includes('localhost') && !value.includes('127.0.0.1') && !value.startsWith('http://')
}

function localLook(slug = 'natural', variant = 'full') {
  const look = LOCAL_LOOKS[slug] || LOCAL_LOOKS.natural
  return look[variant] || look.full
}

function lookImage(value, slug = 'natural', variant = 'full') {
  if (isUsableExternalImage(value)) return value
  return localLook(slug, variant)
}

module.exports = { LOCAL_LOOKS, localLook, lookImage }
