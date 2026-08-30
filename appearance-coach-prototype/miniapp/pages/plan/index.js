const api = require('../../services/api')
const { lookImage, userImage } = require('../../utils/media')

Page({
  data: { id: '', plan: null, active: 0, loading: true, selecting: false, showCurrent: false, saved: false, categories: ['发型', '妆容', '穿搭'] },
  onLoad(options) { this.setData({ id: options.id }); this.load() },
  load() {
    api.getPlan(this.data.id).then((plan) => {
      const slug = plan.slug || 'sharp'
      plan.image_url = lookImage(plan.image_url, slug, 'plan')
      plan.current_image_url = userImage(plan.current_image_url)
      plan.detail_image_url = lookImage('', slug, 'hair')
      plan.steps = (plan.steps || []).map((step) => ({ ...step, details: Array.isArray(step.details) ? step.details : [] }))
      this.setData({ plan, loading: false, saved: wx.getStorageSync('jianwo_saved_plan_id') === plan.id })
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  changeTab(event) { this.setData({ active: Number(event.currentTarget.dataset.index) }); wx.vibrateShort({ type: 'light' }) },
  compare() {
    if (!this.data.plan || !this.data.plan.current_image_url) {
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
  sharePlan() { wx.navigateTo({ url: `/pages/share/index?type=plan&id=${this.data.id}` }) },
  onShareAppMessage() { return { title: `${this.data.plan ? this.data.plan.name : ''}形象方案｜怎么打扮`, path: '/pages/home/index' } }
})
