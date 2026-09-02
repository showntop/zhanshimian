const api = require('../../services/api')
const { lookImage, userImage } = require('../../utils/media')

Page({
  data: { id: '', plan: null, active: 0, loading: true, selecting: false, showCurrent: false, saved: false, categories: ['发型', '妆容', '穿搭'] },
  onLoad(options) { this.setData({ id: options.id }); this.load() },
  load() {
    api.getPlan(this.data.id).then((plan) => {
      const mediaRequest = api.getReport(plan.report_id).then((report) => api.getAnalysis(report.analysis_id)).catch(() => null)
      return Promise.all([Promise.resolve(plan), mediaRequest])
    }).then((results) => {
      const plan = results[0]
      const analysis = results[1]
      const slug = plan.slug || 'sharp'
      const media = analysis && analysis.media || []
      const body = media.find((item) => item.kind === 'body')
      const face = media.find((item) => item.kind === 'face')
      plan.generated = Boolean(plan.generated_image_url)
      plan.isDemo = /^demo\//.test(plan.look_provider || '') || /^demo-/.test(plan.look_provider || '')
      plan.image_url = plan.generated ? userImage(plan.generated_image_url) : lookImage(plan.image_url, slug, 'plan')
      plan.current_image_url = userImage(plan.current_image_url)
      plan.current_body_url = userImage(body && body.url)
      plan.current_face_url = userImage(face && face.url) || plan.current_image_url
      plan.current_compare_url = plan.current_body_url || plan.current_face_url
      plan.current_compare_mode = plan.current_body_url ? 'aspectFit' : 'aspectFill'
      plan.steps = (plan.steps || []).map((step) => ({ ...step, details: Array.isArray(step.details) ? step.details : [] }))
      const savedID = wx.getStorageSync('jianwo_saved_plan_id') || wx.getStorageSync('jianwo_plan_id')
      this.setData({ plan, loading: false, saved: savedID === plan.id })
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  changeTab(event) { this.setData({ active: Number(event.currentTarget.dataset.index) }); wx.vibrateShort({ type: 'light' }) },
  compare() {
    if (!this.data.plan || !this.data.plan.current_compare_url) {
      wx.showToast({ title: '当前照片暂时不可用', icon: 'none' })
      return
    }
    this.setData({ showCurrent: !this.data.showCurrent })
  },
  toggleSaved() {
    const saved = !this.data.saved
    this.setData({ saved })
    if (saved) wx.setStorageSync('jianwo_saved_plan_id', this.data.id)
    else wx.removeStorageSync('jianwo_saved_plan_id')
    wx.showToast({ title: saved ? '方案已保存' : '已取消保存', icon: 'none' })
  },
  createChecklist() {
    this.setData({ selecting: true })
    api.selectPlan(this.data.id).then(() => {
      getApp().globalData.planID = this.data.id
      wx.setStorageSync('jianwo_plan_id', this.data.id)
      wx.navigateTo({ url: `/pages/checklist/index?id=${this.data.id}` })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ selecting: false }))
  },
  feedback() { wx.navigateTo({ url: `/pages/feedback/index?planId=${this.data.id}` }) },
  sharePlan() { wx.navigateTo({ url: `/pages/share/index?type=plan&id=${this.data.id}` }) },
  onShareAppMessage() { return { title: `${this.data.plan ? this.data.plan.name : ''}形象方案｜怎么打扮`, path: '/pages/home/index' } }
})
