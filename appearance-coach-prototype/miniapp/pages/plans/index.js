const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: { reportId: '', scene: '', plans: [], savedHair: [], selected: 0, showCurrent: false, loading: true, sceneLabel: '', asTab: false },
  onLoad(options) {
    const labels = { interview: '面试', wedding: '婚礼', date: '约会', daily: '日常' }
    this.setData({ reportId: options.reportId || wx.getStorageSync('jianwo_report_id'), scene: options.scene || '', sceneLabel: labels[options.scene] || '', asTab: options.tab === '1' })
    this.load()
  },
  retry() { this.setData({ loading: true }); this.load() },
  load() {
	const planRequest = this.data.reportId ? api.getPlans(this.data.reportId, this.data.scene) : Promise.resolve([])
	Promise.all([planRequest, api.getSavedHairPreviews()]).then(([plans, savedHair]) => {
      const mapped = plans.map((plan) => ({
        ...plan,
        image_url: lookImage(plan.image_url, plan.slug, 'plan'),
        current_image_url: lookImage('', 'natural', 'plan'),
        thumbnail_url: lookImage(plan.image_url, plan.slug, 'portrait')
      }))
		const mappedHair = savedHair.map((item) => ({ ...item, preview_label: (item.provider_version || '').indexOf('demo') === 0 ? '效果示例' : 'AI 本人预览' }))
		this.setData({ plans: mapped, savedHair: mappedHair, selected: Math.max(0, mapped.findIndex((item) => item.recommended)), loading: false })
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  swiperChange(event) { this.setData({ selected: event.detail.current, showCurrent: false }); wx.vibrateShort({ type: 'light' }) },
  selectThumb(event) { this.setData({ selected: Number(event.currentTarget.dataset.index), showCurrent: false }) },
  toggleCompare(event) { this.setData({ showCurrent: event.currentTarget.dataset.mode === 'current' }) },
  openPlan() { const plan = this.data.plans[this.data.selected]; if (plan) wx.navigateTo({ url: `/pages/plan/index?id=${plan.id}` }) },
  onShareAppMessage() { return { title: '同一个我，三种更适合的表达｜怎么打扮', path: '/pages/home/index' } }
})
