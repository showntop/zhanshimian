const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: { plan: null, loading: true, refreshing: false, activating: false, sendingFeedback: false, error: '', previewOpen: false, feedbacks: ['适合我', '太正式', '想更轻松', '今天穿了'] },
  onLoad() { api.trackEvent('page_view', { page: 'today' }).catch(() => {}); this.load() },
  load() {
    api.getTodayPlan().then((plan) => {
      if (plan) this.setData({ plan: this.mapPlan(plan), loading: false, error: '' })
      else this.generate(false)
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false, error: error.message || '今日方案暂时没有生成' }) })
  },
  mapPlan(plan) { return { ...plan, image_url: lookImage(plan.image_url, 'sharp', 'full') } },
  generate(refresh) {
    if (this.data.refreshing) return
    this.setData({ refreshing: true, loading: !this.data.plan, error: '' })
    api.createTodayPlan({ report_id: wx.getStorageSync('jianwo_report_id'), city: wx.getStorageSync('jianwo_city') || '', schedule: '', refresh })
      .then((plan) => { this.setData({ plan: this.mapPlan(plan), loading: false, error: '' }); api.trackEvent('today_plan_generate', { refresh: Boolean(refresh), plan_id: plan.id }).catch(() => {}) })
      .catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false, error: error.message || '今日方案暂时没有生成' }) })
      .finally(() => this.setData({ refreshing: false }))
  },
  regenerate() { this.generate(true) },
  editCity() {
    wx.showModal({
      title: '切换城市', editable: true, placeholderText: '输入城市名，如：杭州',
      content: wx.getStorageSync('jianwo_city') || (this.data.plan && this.data.plan.context.city) || '',
      success: ({ confirm, content }) => {
        if (!confirm) return
        const city = (content || '').trim()
        if (city) wx.setStorageSync('jianwo_city', city)
        else wx.removeStorageSync('jianwo_city')
        this.generate(true)
      }
    })
  },
  activate() {
    if (this.data.activating || !this.data.plan || this.data.plan.active) return
    this.setData({ activating: true })
    api.activateTodayPlan(this.data.plan.id).then((plan) => { this.setData({ plan: this.mapPlan(plan) }); api.trackEvent('today_plan_activate', { plan_id: plan.id }).catch(() => {}); wx.showToast({ title: '已加入今日清单', icon: 'success' }) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ activating: false }))
  },
  feedback(event) {
    if (this.data.sendingFeedback || !this.data.plan) return
    const feedback = event.currentTarget.dataset.value
    if (feedback === this.data.plan.feedback) return
    this.setData({ sendingFeedback: true })
    api.feedbackTodayPlan(this.data.plan.id, feedback).then((plan) => { this.setData({ plan: this.mapPlan(plan) }); api.trackEvent('today_feedback', { plan_id: plan.id, feedback }).catch(() => {}); wx.showToast({ title: '顾问记住了', icon: 'success' }) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ sendingFeedback: false }))
  },
  preview() { this.setData({ previewOpen: true }) },
  closePreview() { this.setData({ previewOpen: false }) },
  sharePlan() { wx.navigateTo({ url: `/pages/share/index?type=today&id=${this.data.plan.id}` }) },
  openAdvisor() { wx.navigateTo({ url: `/pages/advisor/index?todayPlanId=${this.data.plan.id}` }) }
})
