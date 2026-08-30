const api = require('../../services/api')
const { userImage } = require('../../utils/media')

Page({
  data: { id: '', scene: 'general', progress: 8, stage: '正在安全上传照片', failed: false, media: [], previewImage: '', previewKind: 'face', previewLabel: '正脸照', previewMode: 'aspectFill' },
  onLoad(options) {
    const app = getApp()
    const initialMedia = app.globalData.analysisMedia || []
    this.setData({ id: options.id || app.globalData.analysisID, scene: options.scene || 'general' })
    this.updateMedia(initialMedia, this.data.progress)
    this.poll()
  },
  onUnload() { if (this.timer) clearTimeout(this.timer) },
  poll() {
    api.getAnalysis(this.data.id).then((analysis) => {
      this.failures = 0
      this.setData({ progress: analysis.progress, stage: analysis.stage, failed: analysis.status === 'failed' })
      if (Array.isArray(analysis.media) && analysis.media.length) {
        getApp().globalData.analysisMedia = analysis.media
        this.updateMedia(analysis.media, analysis.progress)
      }
      if (analysis.status === 'completed') {
        getApp().globalData.reportID = analysis.report_id
        wx.setStorageSync('jianwo_report_id', analysis.report_id)
        this.createPendingScenePlans(analysis.report_id)
        return
      }
      if (analysis.status !== 'failed') this.timer = setTimeout(() => this.poll(), 700)
    }).catch((error) => {
      // A 404 means the analysis is gone (data was cleared mid-run); anything
      // else may be transient. Either way, stop retrying silently forever —
      // surface the failure UI so the page never looks stuck.
      if (error && error.statusCode === 404) {
        this.setData({ failed: true })
        return
      }
      this.failures = (this.failures || 0) + 1
      if (this.failures >= 5) {
        this.setData({ failed: true })
        return
      }
      this.timer = setTimeout(() => this.poll(), 1200)
    })
  },
  updateMedia(media, progress) {
    const normalized = (media || []).map((item) => ({ ...item, url: userImage(item.url) }))
    const desiredKind = progress < 54 ? 'face' : (progress < 84 ? 'side' : 'body')
    const preview = normalized.find((item) => item.kind === desiredKind && item.url) || normalized.find((item) => item.kind === 'face' && item.url) || normalized.find((item) => item.url)
    this.setData({
      media: normalized,
      previewImage: preview ? preview.url : '',
      previewKind: preview ? preview.kind : desiredKind,
      previewLabel: ({ face: '正脸照', side: '侧脸照', body: '全身照' })[preview ? preview.kind : desiredKind] || '照片',
      previewMode: preview && preview.kind === 'body' ? 'aspectFit' : 'aspectFill'
    })
  },
  createPendingScenePlans(reportID) {
    const brief = wx.getStorageSync('jianwo_scene_brief')
    const shouldCreate = this.data.scene !== 'general' && brief && brief.scene === this.data.scene
    const destination = (scene) => setTimeout(() => wx.redirectTo({ url: `/pages/report/index?id=${reportID}${scene ? `&scene=${scene}` : ''}` }), 500)
    if (!shouldCreate) {
      destination('')
      return
    }
    this.setData({ stage: '正在为这次场合组合三套方案' })
    api.createScenePlans(reportID, brief)
      .then(() => destination(this.data.scene))
      .catch(() => {
        wx.showToast({ title: '场合方案稍后可重新生成', icon: 'none' })
        destination('')
      })
  },
  retry() { wx.navigateBack({ delta: 1 }) }
})
