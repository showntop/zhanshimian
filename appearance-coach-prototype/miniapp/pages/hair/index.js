const api = require('../../services/api')
const { lookImage, exampleImage, userImage, isBundledAsset } = require('../../utils/media')

Page({
  data: {
    // 发型推荐只来自接口;为空或失败时页面显示真实的空态/错误态,不再展示写死的假发型
    styles: [], activeIndex: 0, activeStyle: null, loading: true, error: '',
    sourceMediaID: '', currentImage: '', resultImage: '', previewID: '', previewStatus: '', progress: 0,
    stage: '', showCurrent: false, generating: false, saving: false, saved: false, isDemo: false, usingProfilePhoto: false
  },
  onLoad() { this.loadRecommendations(); this.loadProfilePhoto(); this.resumePreview() },
  onUnload() { this.stopPolling() },
  loadRecommendations() {
    this.setData({ loading: true, error: '' })
    api.runTool({ kind: 'hair', report_id: wx.getStorageSync('jianwo_report_id') || '', scene: 'daily' }).then((result) => {
      // 推荐图 URL 无效时显式回退到内置示例图,wxml 用 item.isExample 叠加「风格参考」角标
      const styles = (result.options || []).map((item) => {
        const imageURL = lookImage(item.image_url)
        return { ...item, image: imageURL || exampleImage(item.id, 'hair'), isExample: !imageURL || isBundledAsset(imageURL) }
      })
      const resumedStyleID = this.data.previewID && this.data.activeStyle && this.data.activeStyle.id
      const activeIndex = resumedStyleID ? Math.max(0, styles.findIndex((item) => item.id === resumedStyleID)) : 0
      this.setData({ styles, activeStyle: styles[activeIndex] || null, activeIndex, loading: false })
    }).catch((error) => this.setData({ error: error.message, loading: false }))
  },
  goAnalyze() { wx.navigateTo({ url: '/pages/capture/index?scene=general' }) },
  selectStyle(event) {
    if (this.data.generating) return
    const activeIndex = Number(event.currentTarget.dataset.index)
    wx.removeStorageSync('jianwo_active_hair_preview')
    this.setData({ activeIndex, activeStyle: this.data.styles[activeIndex], resultImage: '', previewID: '', saved: false, showCurrent: true })
  },
  choosePhoto() {
    if (this.data.generating) return
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => this.uploadPhoto(tempFiles[0].tempFilePath) })
  },
  uploadPhoto(path) {
    wx.showLoading({ title: '安全上传中' })
    api.uploadMedia('face', path).then((asset) => {
      wx.removeStorageSync('jianwo_active_hair_preview')
      this.setData({ sourceMediaID: asset.id, currentImage: path, resultImage: '', previewID: '', showCurrent: true, saved: false, usingProfilePhoto: false })
      wx.vibrateShort({ type: 'light' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => wx.hideLoading())
  },
  useDemoPhoto() {
    api.createDemoMedia('face').then((asset) => { wx.removeStorageSync('jianwo_active_hair_preview'); this.setData({ sourceMediaID: asset.id, currentImage: userImage(asset.url), resultImage: '', previewID: '', showCurrent: true, usingProfilePhoto: false }) })
      .catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  resumePreview() {
    const previewID = wx.getStorageSync('jianwo_active_hair_preview') || ''
    if (!previewID) return
    api.getHairPreview(previewID).then((preview) => {
      const activeIndex = Math.max(0, this.data.styles.findIndex((item) => item.id === preview.style_id))
      const base = { previewID: preview.id, previewStatus: preview.status, progress: preview.progress, stage: preview.stage, activeIndex, activeStyle: this.data.styles[activeIndex] || this.data.activeStyle }
      if (preview.status === 'completed') {
        this.setData({ ...base, generating: false, currentImage: userImage(preview.source_image_url) || this.data.currentImage, resultImage: lookImage(preview.result_image_url), showCurrent: false, isDemo: (preview.provider_version || '').indexOf('demo') === 0 })
        return
      }
      if (preview.status === 'failed') {
        wx.removeStorageSync('jianwo_active_hair_preview')
        this.setData({ ...base, generating: false, error: preview.error_message || '预览暂时没有生成' })
        return
      }
      this.setData({ ...base, generating: true, error: '', currentImage: userImage(preview.source_image_url) || this.data.currentImage })
      this.pollPreview()
    }).catch((error) => { console.warn('[hair] 预览进度恢复失败', error); wx.removeStorageSync('jianwo_active_hair_preview') })
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
      .catch((error) => console.warn('[hair] 形象档案正脸照加载失败', error))
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
      wx.setStorageSync('jianwo_active_hair_preview', preview.id)
      this.setData({ previewID: preview.id, previewStatus: preview.status, progress: preview.progress, stage: preview.stage })
      this.pollPreview()
    }).catch((error) => this.setData({ generating: false, error: error.message }))
  },
  pollPreview() {
    if (!this.data.previewID) return
    api.getHairPreview(this.data.previewID).then((preview) => {
      if (preview.status === 'completed') {
        this.setData({ generating: false, previewStatus: preview.status, progress: 100, stage: preview.stage, currentImage: userImage(preview.source_image_url) || this.data.currentImage, resultImage: lookImage(preview.result_image_url), showCurrent: false, isDemo: (preview.provider_version || '').indexOf('demo') === 0 })
        wx.vibrateShort({ type: 'light' })
        return
      }
      if (preview.status === 'failed') {
        wx.removeStorageSync('jianwo_active_hair_preview')
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
      wx.removeStorageSync('jianwo_active_hair_preview')
      this.setData({ saved: true })
      wx.showToast({ title: '已保存到方案', icon: 'success' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ saving: false }))
  },
  changePhoto() { this.choosePhoto() }
})
