const api = require('../../services/api')
const { userImage } = require('../../utils/media')

Page({
  data: { hasReport: false, hasPlan: false, deleting: false, profileImage: '' },
  onShow() {
    const reportID = wx.getStorageSync('jianwo_report_id') || ''
    const savedPlanID = wx.getStorageSync('jianwo_saved_plan_id') || wx.getStorageSync('jianwo_plan_id') || ''
    this.setData({ hasReport: Boolean(reportID), hasPlan: Boolean(savedPlanID) })
    if (!reportID) { this.setData({ profileImage: '' }); return }
    // Keep the previously loaded avatar instead of clearing it on every show,
    // so switching back to this tab doesn't flash placeholder -> photo.
    if (this.data.profileImage) return
    api.getReport(reportID)
      .then((report) => api.getAnalysis(report.analysis_id))
      .then((analysis) => {
        const face = (analysis.media || []).find((item) => item.kind === 'face')
        this.setData({ profileImage: userImage(face && face.url) })
      })
      .catch(() => {})
  },
  openReport() { const id = wx.getStorageSync('jianwo_report_id'); if (id) wx.navigateTo({ url: `/pages/report/index?id=${id}` }) },
  openPlan() { const id = wx.getStorageSync('jianwo_saved_plan_id') || wx.getStorageSync('jianwo_plan_id'); if (id) wx.navigateTo({ url: `/pages/plan/index?id=${id}` }) },
  reanalyze() { this.setData({ profileImage: '' }); wx.navigateTo({ url: '/pages/capture/index?scene=general&replace=1' }) },
  editBasic() { wx.showToast({ title: '基础资料编辑将在下一版开放', icon: 'none' }) },
  editBody() { wx.showModal({ title: '身体数据为选填', content: '体重和三围只在需要提升穿搭准确度或体验 3D/试衣时填写，不影响基础形象分析。', showCancel: false, confirmText: '知道了' }) },
  openLab() { wx.navigateTo({ url: '/pages/lab/index' }) },
  openWardrobe() { wx.navigateTo({ url: '/pages/wardrobe/index' }) },
  openAdvisor() { wx.navigateTo({ url: '/pages/advisor/index' }) },
  deleteData() {
    wx.showModal({ title: '删除形象档案？', content: '照片、报告、方案与反馈都会永久删除，此操作无法撤销。', confirmText: '确认删除', confirmColor: '#9b4b45', success: ({ confirm }) => {
      if (!confirm) return
      this.setData({ deleting: true })
      api.deleteData().then(() => {
        wx.removeStorageSync('jianwo_report_id'); wx.removeStorageSync('jianwo_plan_id'); wx.removeStorageSync('jianwo_saved_plan_id'); wx.removeStorageSync('jianwo_active_analysis_id'); wx.removeStorageSync('jianwo_active_plan_generation'); wx.removeStorageSync('jianwo_active_hair_preview')
        this.setData({ hasReport: false, hasPlan: false, profileImage: '' })
        wx.showToast({ title: '档案已删除', icon: 'success' })
      }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ deleting: false }))
    } })
  }
})
