const api = require('../../services/api')

Page({
  data: { id: '', scene: 'general', progress: 8, stage: '正在安全上传照片', failed: false },
  onLoad(options) {
    this.setData({ id: options.id || getApp().globalData.analysisID, scene: options.scene || 'general' })
    this.poll()
  },
  onUnload() { if (this.timer) clearTimeout(this.timer) },
  poll() {
    api.getAnalysis(this.data.id).then((analysis) => {
      this.setData({ progress: analysis.progress, stage: analysis.stage, failed: analysis.status === 'failed' })
      if (analysis.status === 'completed') {
        getApp().globalData.reportID = analysis.report_id
        wx.setStorageSync('jianwo_report_id', analysis.report_id)
        this.createPendingScenePlans(analysis.report_id)
        return
      }
      if (analysis.status !== 'failed') this.timer = setTimeout(() => this.poll(), 700)
    }).catch(() => { this.timer = setTimeout(() => this.poll(), 1200) })
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
