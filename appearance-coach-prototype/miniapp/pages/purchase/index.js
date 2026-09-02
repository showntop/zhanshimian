const api = require('../../services/api')

const sceneCodes = { '日常': 'daily', '面试': 'interview', '约会': 'date' }

Page({
  data: {
    photo: '', mediaID: '', context: '日常', contexts: ['日常', '面试', '约会'],
    loading: false, result: null, error: '', saving: false
  },
  chooseProduct() {
    wx.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['album', 'camera'],
      success: ({ tempFiles }) => this.setData({
        photo: tempFiles[0].tempFilePath, mediaID: '', result: null, error: ''
      })
    })
  },
  useDemo() {
    if (this.data.loading) return
    this.setData({ loading: true, error: '' })
    api.createDemoMedia('product').then((asset) => this.setData({
      photo: '/assets/placeholders/figure.png', mediaID: asset.id, result: null, loading: false
    })).catch((error) => this.setData({ error: error.message, loading: false }))
  },
  selectContext(event) {
    this.setData({ context: event.currentTarget.dataset.value, result: null, error: '' })
  },
  analyze() {
    if (!this.data.photo || this.data.loading) return
    this.setData({ loading: true, error: '' })
    const mediaReady = this.data.mediaID
      ? Promise.resolve({ id: this.data.mediaID })
      : api.uploadMedia('product', this.data.photo)
    mediaReady.then((asset) => {
      this.setData({ mediaID: asset.id })
      return api.runTool({
        kind: 'purchase',
        media_id: asset.id,
        report_id: wx.getStorageSync('jianwo_report_id') || '',
        scene: sceneCodes[this.data.context] || 'daily'
      })
    }).then((result) => this.setData({ result, loading: false }))
      .catch((error) => this.setData({ error: error.message, loading: false }))
  },
  saveProduct() {
    if (!this.data.result || this.data.saving) return
    this.setData({ saving: true, error: '' })
    api.saveToolResult(this.data.result.id).then(() => {
      wx.setStorageSync('jianwo_saved_product', {
        resultID: this.data.result.id, photo: this.data.photo, context: this.data.context
      })
      this.setData({ saving: false, 'result.saved': true })
      wx.showToast({ title: '已加入方案', icon: 'success' })
    }).catch((error) => this.setData({ error: error.message, saving: false }))
  }
})
