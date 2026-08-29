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
