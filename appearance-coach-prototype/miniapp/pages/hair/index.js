const api = require('../../services/api')
const { lookImage, userImage } = require('../../utils/media')

const fallbackStyles = [
  { id: 'sharp', name: '锁骨层次发', note: '首选推荐', image: '/assets/hair/sharp.jpg', reason: '提高视觉重心并保留脸侧空气感。', tags: ['重心提高', '肩颈更清晰'] },
  { id: 'warm', name: '空气微卷', note: '柔和表达', image: '/assets/hair/warm.jpg', reason: '柔和的发尾弧度能保留亲和感。', tags: ['自然柔和', '上镜'] },
  { id: 'natural', name: '自然偏分', note: '低维护', image: '/assets/hair/natural.jpg', reason: '只调整分缝与耳侧线条，最容易维持。', tags: ['改动小', '低维护'] }
]

Page({
  data: {
    styles: fallbackStyles, activeIndex: 0, activeStyle: fallbackStyles[0], loading: true, error: '',
    sourceMediaID: '', currentImage: '', resultImage: '', previewID: '', previewStatus: '', progress: 0,
    stage: '', showCurrent: false, generating: false, saving: false, saved: false, isDemo: false, usingProfilePhoto: false
  },
  onLoad() { this.loadRecommendations(); this.loadProfilePhoto() },
  onUnload() { this.stopPolling() },
  loadRecommendations() {
    this.setData({ loading: true, error: '' })
    api.runTool({ kind: 'hair', report_id: wx.getStorageSync('jianwo_report_id') || '', scene: 'daily' }).then((result) => {
      const styles = (result.options || []).map((item) => ({ ...item, image: lookImage(item.image_url, item.id, 'hair') }))
      const available = styles.length ? styles : fallbackStyles
      this.setData({ styles: available, activeStyle: available[0], activeIndex: 0, loading: false })
    }).catch((error) => this.setData({ error: error.message, loading: false }))
  },
  selectStyle(event) {
    if (this.data.generating) return
    const activeIndex = Number(event.currentTarget.dataset.index)
    this.setData({ activeIndex, activeStyle: this.data.styles[activeIndex], resultImage: '', previewID: '', saved: false, showCurrent: true })
  },
  choosePhoto() {
    if (this.data.generating) return
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => this.uploadPhoto(tempFiles[0].tempFilePath) })
  },
  uploadPhoto(path) {
    wx.showLoading({ title: '安全上传中' })
    api.uploadMedia('face', path).then((asset) => {
      this.setData({ sourceMediaID: asset.id, currentImage: path, resultImage: '', previewID: '', showCurrent: true, saved: false, usingProfilePhoto: false })
      wx.vibrateShort({ type: 'light' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => wx.hideLoading())
  },
  useDemoPhoto() {
    api.createDemoMedia('face').then((asset) => this.setData({ sourceMediaID: asset.id, currentImage: lookImage(asset.url, 'natural', 'portrait'), resultImage: '', previewID: '', showCurrent: true, usingProfilePhoto: false }))
      .catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  loadProfilePhoto() {
    const reportID = wx.getStorageSync('jianwo_report_id') || ''
    if (!reportID) return
    api.getReport(reportID)
      .then((report) => api.getAnalysis(report.analysis_id))
      .then((analysis) => {
        if (this.data.sourceMediaID) return
        const face = (analysis.media || []).find((item) => item.kind === 'face')
        const currentImage = face && userImage(face.url)
        if (!face || !currentImage) return
        this.setData({ sourceMediaID: face.id, currentImage, showCurrent: true, usingProfilePhoto: true })
      })
      .catch(() => {})
  },
  primaryAction() {
    if (this.data.resultImage) return this.savePreview()
    if (!this.data.sourceMediaID) return this.choosePhoto()
    this.generatePreview()
  },
  generatePreview() {
    if (this.data.generating) return
    this.stopPolling()
    this.setData({ generating: true, error: '', progress: 8, stage: '正在创建预览', resultImage: '', saved: false })
    api.createHairPreview({ media_id: this.data.sourceMediaID, report_id: wx.getStorageSync('jianwo_report_id') || '', style_id: this.data.activeStyle.id, scene: 'daily' }).then((preview) => {
      this.setData({ previewID: preview.id, previewStatus: preview.status, progress: preview.progress, stage: preview.stage })
      this.pollPreview()
    }).catch((error) => this.setData({ generating: false, error: error.message }))
  },
  pollPreview() {
    if (!this.data.previewID) return
    api.getHairPreview(this.data.previewID).then((preview) => {
      if (preview.status === 'completed') {
        this.setData({ generating: false, previewStatus: preview.status, progress: 100, stage: preview.stage, currentImage: userImage(preview.source_image_url) || this.data.currentImage, resultImage: lookImage(preview.result_image_url, this.data.activeStyle.id, 'hair'), showCurrent: false, isDemo: (preview.provider_version || '').indexOf('demo') === 0 })
        wx.vibrateShort({ type: 'light' })
        return
      }
      if (preview.status === 'failed') {
        this.setData({ generating: false, previewStatus: preview.status, error: preview.error_message || '预览暂时没有生成' })
        return
      }
      this.setData({ previewStatus: preview.status, progress: preview.progress, stage: preview.stage })
      this.pollTimer = setTimeout(() => this.pollPreview(), 900)
    }).catch((error) => this.setData({ generating: false, error: error.message }))
  },
  stopPolling() { if (this.pollTimer) clearTimeout(this.pollTimer); this.pollTimer = null },
  toggleCurrent(event) { if (this.data.resultImage) this.setData({ showCurrent: event.currentTarget.dataset.current === '1' }) },
  savePreview() {
    if (!this.data.previewID || !this.data.resultImage) return wx.showToast({ title: '请先生成本人预览', icon: 'none' })
    if (this.data.saving) return
    this.setData({ saving: true })
    api.saveHairPreview(this.data.previewID).then(() => {
      wx.setStorageSync('jianwo_saved_hair', this.data.previewID)
      this.setData({ saved: true })
      wx.showToast({ title: '已保存到方案', icon: 'success' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ saving: false }))
  },
  changePhoto() { this.choosePhoto() }
})
