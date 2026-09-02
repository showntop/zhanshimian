const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: { card: null, snapshot: null, loading: true, owner: false, saving: false },
  onLoad(options) {
    if (options.token) {
      api.getShareCard(options.token).then((card) => this.setCard(card, false)).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
      return
    }
    if (!options.type || !options.id) { this.setData({ loading: false }); return }
    api.createShareCard({ source_type: options.type, source_id: options.id, include_photo: false }).then((card) => { this.setCard(card, true); api.trackEvent('share_card_create', { source_type: options.type, source_id: options.id }).catch(() => {}) }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  setCard(card, owner) {
    const snapshot = card.snapshot ? { ...card.snapshot, image_url: card.snapshot.image_url ? lookImage(card.snapshot.image_url, 'natural', 'plan') : '' } : null
    this.setData({ card, snapshot, owner, loading: false })
  },
  savePoster() {
    if (!this.data.snapshot) return
    this.setData({ saving: true })
    const query = wx.createSelectorQuery()
    query.select('#shareCanvas').fields({ node: true, size: true }).exec((result) => {
      const canvas = result[0] && result[0].node
      if (!canvas) { this.setData({ saving: false }); return }
      const width = result[0].width; const height = result[0].height; const ratio = wx.getWindowInfo().pixelRatio || 2
      canvas.width = width * ratio; canvas.height = height * ratio
      const context = canvas.getContext('2d'); context.scale(ratio, ratio)
      context.fillStyle = '#f8f5f0'; context.fillRect(0, 0, width, height)
      context.fillStyle = '#587344'; context.font = '18px sans-serif'; context.fillText('怎么打扮 · AI 形象顾问', 28, 42)
      context.fillStyle = '#292d29'; context.font = 'bold 28px sans-serif'; this.drawText(context, this.data.snapshot.title || '我的形象方案', 28, 92, width - 56, 38)
      context.fillStyle = '#676d66'; context.font = '16px sans-serif'; this.drawText(context, this.data.snapshot.summary || '', 28, 180, width - 56, 26)
      context.fillStyle = '#eef2e9'; context.fillRect(28, height - 118, width - 56, 68)
      context.fillStyle = '#587344'; context.font = '16px sans-serif'; context.fillText('发型 · 妆容 · 穿搭已为我组合好', 48, height - 78)
      context.fillStyle = '#858a84'; context.font = '13px sans-serif'; context.fillText('方案由本人主动分享 · 照片默认不公开', 28, height - 22)
      wx.canvasToTempFilePath({ canvas, success: ({ tempFilePath }) => wx.saveImageToPhotosAlbum({ filePath: tempFilePath, success: () => wx.showToast({ title: '已保存到相册', icon: 'success' }), fail: () => wx.showToast({ title: '请允许保存到相册', icon: 'none' }), complete: () => this.setData({ saving: false }) }), fail: () => this.setData({ saving: false }) })
    })
  },
  drawText(context, text, x, y, maxWidth, lineHeight) {
    let line = ''; let top = y
    for (const character of text) {
      if (context.measureText(line + character).width > maxWidth) { context.fillText(line, x, top); line = character; top += lineHeight } else line += character
    }
    if (line) context.fillText(line, x, top)
  },
  revoke() {
    api.revokeShareCard(this.data.card.id).then(() => { wx.showToast({ title: '分享已撤销', icon: 'success' }); wx.navigateBack() }).catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  onShareAppMessage() {
    return { title: `${this.data.snapshot ? this.data.snapshot.title : '我的形象方案'}｜怎么打扮`, path: `/pages/share/index?token=${this.data.card ? this.data.card.token : ''}` }
  }
})
