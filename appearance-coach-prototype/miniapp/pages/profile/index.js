const api = require('../../services/api')

Page({
  data: { hasReport: false, hasPlan: false, deleting: false },
  onShow() { this.setData({ hasReport: Boolean(wx.getStorageSync('jianwo_report_id')), hasPlan: Boolean(wx.getStorageSync('jianwo_plan_id')) }) },
  openReport() { const id = wx.getStorageSync('jianwo_report_id'); if (id) wx.navigateTo({ url: `/pages/report/index?id=${id}` }) },
  openPlan() { const id = wx.getStorageSync('jianwo_plan_id'); if (id) wx.navigateTo({ url: `/pages/plan/index?id=${id}` }) },
  editBasic() { wx.showToast({ title: '基础资料编辑将在下一版开放', icon: 'none' }) },
  editBody() { wx.showModal({ title: '身体数据为选填', content: '体重和三围只在需要提升穿搭准确度或体验 3D/试衣时填写，不影响基础形象分析。', showCancel: false, confirmText: '知道了' }) },
  openLab() { wx.navigateTo({ url: '/pages/lab/index' }) },
  deleteData() {
    wx.showModal({ title: '删除形象档案？', content: '照片、报告、方案与反馈都会永久删除，此操作无法撤销。', confirmText: '确认删除', confirmColor: '#9b4b45', success: ({ confirm }) => {
      if (!confirm) return
      this.setData({ deleting: true })
      api.deleteData().then(() => {
        wx.removeStorageSync('jianwo_report_id'); wx.removeStorageSync('jianwo_plan_id'); wx.removeStorageSync('jianwo_saved_plan_id')
        this.setData({ hasReport: false, hasPlan: false })
        wx.showToast({ title: '档案已删除', icon: 'success' })
      }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ deleting: false }))
    } })
  }
})
