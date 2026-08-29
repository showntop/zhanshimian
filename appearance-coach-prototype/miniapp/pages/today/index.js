const api = require('../../services/api')

Page({
  data: { plan: null, loading: true, refreshing: false, previewOpen: false, feedbacks: ['适合我', '太正式', '想更轻松', '今天穿了'] },
  onLoad() { api.trackEvent('page_view', { page: 'today' }).catch(() => {}); this.load() },
  load() {
    api.getTodayPlan().then((plan) => {
      if (plan) this.setData({ plan, loading: false })
      else this.generate(false)
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  generate(refresh) {
    this.setData({ refreshing: true })
    api.createTodayPlan({ report_id: wx.getStorageSync('jianwo_report_id'), city: '上海', schedule: '通勤', refresh })
      .then((plan) => { this.setData({ plan, loading: false }); api.trackEvent('today_plan_generate', { refresh: Boolean(refresh), plan_id: plan.id }).catch(() => {}) })
      .catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
      .finally(() => this.setData({ refreshing: false }))
  },
  regenerate() { this.generate(true) },
  activate() {
    api.activateTodayPlan(this.data.plan.id).then((plan) => { this.setData({ plan }); api.trackEvent('today_plan_activate', { plan_id: plan.id }).catch(() => {}); wx.showToast({ title: '已加入今日清单', icon: 'success' }) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  feedback(event) {
    const feedback = event.currentTarget.dataset.value
    api.feedbackTodayPlan(this.data.plan.id, feedback).then((plan) => { this.setData({ plan }); api.trackEvent('today_feedback', { plan_id: plan.id, feedback }).catch(() => {}); wx.showToast({ title: '顾问记住了', icon: 'success' }) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  preview() { this.setData({ previewOpen: true }) },
  closePreview() { this.setData({ previewOpen: false }) },
  noop() {},
  sharePlan() { wx.navigateTo({ url: `/pages/share/index?type=today&id=${this.data.plan.id}` }) },
  openAdvisor() { wx.navigateTo({ url: `/pages/advisor/index?todayPlanId=${this.data.plan.id}` }) }
})
