const api = require('../../services/api')

Page({
  data: {
    scene: 'general',
    shots: [
      { kind: 'face', title: '正脸', tip: '看脸型与五官比例', asset: null, path: '', placeholder: '/assets/capture/face.png' },
      { kind: 'side', title: '侧脸', tip: '看轮廓与发型空间', asset: null, path: '', placeholder: '/assets/capture/side.png' },
      { kind: 'body', title: '全身', tip: '看整体比例与穿搭', asset: null, path: '', placeholder: '/assets/capture/body.png' }
    ],
    loading: false,
    completed: 0,
    demo: false,
    replace: false
  },
  onLoad(options) { this.setData({ scene: options.scene || 'general', demo: options.demo === '1', replace: options.replace === '1' }) },
  chooseOne(event) {
    const index = Number(event.currentTarget.dataset.index)
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => this.upload(index, tempFiles[0].tempFilePath) })
  },
  choosePhotos(sourceType) {
    const pending = this.data.shots.map((item, index) => item.asset ? -1 : index).filter((index) => index >= 0)
    if (!pending.length) return this.startAnalysis()
    wx.chooseMedia({
      count: pending.length,
      mediaType: ['image'],
      sourceType: [sourceType],
      sizeType: ['compressed'],
      success: ({ tempFiles }) => {
        const chosen = tempFiles.slice(0, pending.length)
        this.setData({ loading: true })
        Promise.all(chosen.map((file, offset) => api.uploadMedia(this.data.shots[pending[offset]].kind, file.tempFilePath).then((asset) => ({ index: pending[offset], asset, path: file.tempFilePath })))).then((results) => {
          const shots = this.data.shots.map((item, index) => {
            const result = results.find((entry) => entry.index === index)
            return result ? { ...item, asset: result.asset, path: result.path } : item
          })
          this.setData({ shots, completed: shots.filter((item) => item.asset).length })
          wx.vibrateShort({ type: 'light' })
        }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ loading: false }))
      }
    })
  },
  primaryAction() { this.data.completed === 3 ? this.startAnalysis() : this.choosePhotos('camera') },
  chooseFromAlbum() { this.choosePhotos('album') },
  showHelp() {
    wx.showModal({ title: '这样拍更准确', content: '站在窗边自然光下，关闭美颜与滤镜。正脸平视镜头；侧脸转向 90°；全身照露出肩颈与腰线即可，脚部可以不完整入镜。', showCancel: false, confirmText: '知道了', confirmColor: '#587344' })
  },
  upload(index, path) {
    const kind = this.data.shots[index].kind
    wx.showLoading({ title: '安全上传中' })
    api.uploadMedia(kind, path).then((asset) => this.updateShot(index, asset, path)).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => wx.hideLoading())
  },
  updateShot(index, asset, path) {
    const shots = this.data.shots.map((item, itemIndex) => itemIndex === index ? { ...item, asset, path } : item)
    this.setData({ shots, completed: shots.filter((item) => item.asset).length })
    wx.vibrateShort({ type: 'light' })
  },
  useDemo() {
    this.setData({ loading: true })
    Promise.all(this.data.shots.map((item) => api.createDemoMedia(item.kind))).then((assets) => {
      const paths = ['/assets/capture/face.png', '/assets/capture/side.png', '/assets/capture/body.png']
      const shots = this.data.shots.map((item, index) => ({ ...item, asset: assets[index], path: paths[index] }))
      this.setData({ shots, completed: 3 })
      wx.showToast({ title: '示例照片已就绪', icon: 'success' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ loading: false }))
  },
  startAnalysis() {
    if (this.data.completed !== 3) return
    const activeID = wx.getStorageSync('jianwo_active_analysis_id') || ''
    if (activeID) {
      this.setData({ loading: true })
      api.getAnalysis(activeID).then((analysis) => {
        if (analysis.status === 'queued' || analysis.status === 'processing') {
          // 服务端只允许单一活跃分析，新上传的照片无法并行处理，先诚实告知用户
          wx.showModal({
            title: '已有一份分析正在进行',
            content: '继续使用之前上传的照片完成分析；完成后可以重新上传新照片。',
            showCancel: false,
            confirmText: '查看进度',
            success: () => wx.navigateTo({ url: `/pages/analysis/index?id=${analysis.id}&scene=${this.data.scene}` })
          })
          return
        }
        wx.removeStorageSync('jianwo_active_analysis_id')
        this.createAnalysis()
      }).catch(() => {
        wx.removeStorageSync('jianwo_active_analysis_id')
        this.createAnalysis()
      })
      return
    }
    this.createAnalysis()
  },
  createAnalysis() {
    this.setData({ loading: true })
    const mediaIDs = this.data.shots.map((item) => item.asset.id)
    api.createAnalysis({ scene: this.data.scene, media_ids: mediaIDs, profile: { height_cm: 165, role: '', budget: '' } }).then((analysis) => {
      const localMedia = this.data.shots.map((item) => ({ id: item.asset.id, kind: item.kind, url: item.path || item.asset.url || '' }))
      const app = getApp()
      app.globalData.analysisID = analysis.id
      app.globalData.analysisMedia = analysis.media && analysis.media.length ? analysis.media : localMedia
      wx.setStorageSync('jianwo_active_analysis_id', analysis.id)
      wx.navigateTo({ url: `/pages/analysis/index?id=${analysis.id}&scene=${this.data.scene}` })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ loading: false }))
  }
})
