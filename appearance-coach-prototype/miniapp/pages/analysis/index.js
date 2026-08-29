const api = require('../../services/api')

Page({
  data: { id: '', progress: 8, stage: '正在安全上传照片', failed: false },
  onLoad(options) {
    this.setData({ id: options.id || getApp().globalData.analysisID })
    this.poll()
  },
  onUnload() { if (this.timer) clearTimeout(this.timer) },
  poll() {
    api.getAnalysis(this.data.id).then((analysis) => {
      this.setData({ progress: analysis.progress, stage: analysis.stage, failed: analysis.status === 'failed' })
      if (analysis.status === 'completed') {
        getApp().globalData.reportID = analysis.report_id
        wx.setStorageSync('jianwo_report_id', analysis.report_id)
        setTimeout(() => wx.redirectTo({ url: `/pages/report/index?id=${analysis.report_id}` }), 500)
        return
      }
      if (analysis.status !== 'failed') this.timer = setTimeout(() => this.poll(), 700)
    }).catch(() => { this.timer = setTimeout(() => this.poll(), 1200) })
  },
  retry() { wx.navigateBack({ delta: 1 }) }
})
