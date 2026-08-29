const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: { id: '', scene: '', loading: true, report: null, annotations: [], image: '/assets/reports/natural.jpg' },
  onLoad(options) {
    const id = options.id || getApp().globalData.reportID || wx.getStorageSync('jianwo_report_id')
    this.setData({ id, scene: options.scene || '' })
    this.load()
  },
  load() {
    if (!this.data.id) { this.setData({ loading: false }); wx.showToast({ title: '请先完成形象分析', icon: 'none' }); return }
    api.getReport(this.data.id).then((report) => {
      wx.setStorageSync('jianwo_report_id', this.data.id)
      const findings = report.findings || []
      this.setData({ report: { ...report, findings }, annotations: findings.slice(0, 2), image: lookImage(report.current_image_url, 'natural', 'report'), loading: false })
    }).catch((error) => {
      wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false })
    })
  },
  viewPlans() {
    const scene = this.data.scene ? `&scene=${this.data.scene}` : ''
    wx.navigateTo({ url: `/pages/plans/index?reportId=${this.data.id}${scene}` })
  },
  onShareAppMessage() { return { title: '我的形象分析报告｜怎么打扮', path: '/pages/home/index' } }
})
