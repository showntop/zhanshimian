const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: { id: '', plan: null, items: [], completed: 0, loading: true },
  onLoad(options) { this.setData({ id: options.id || wx.getStorageSync('jianwo_plan_id') }); this.load() },
  load() {
    Promise.all([api.getPlan(this.data.id), api.getChecklist(this.data.id)]).then(([plan, items]) => {
      plan.image_url = lookImage(plan.image_url, plan.slug)
      this.setData({ plan, items, completed: items.filter((item) => item.completed).length, loading: false })
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
  },
  toggle(event) {
    const index = Number(event.currentTarget.dataset.index)
    const before = this.data.items[index]
    const items = this.data.items.map((item, itemIndex) => itemIndex === index ? { ...item, completed: !item.completed } : item)
    this.setData({ items, completed: items.filter((item) => item.completed).length })
    wx.vibrateShort({ type: 'light' })
    api.setChecklistItem(before.id, !before.completed).catch(() => this.setData({ items: this.data.items.map((item, itemIndex) => itemIndex === index ? before : item) }))
  },
  feedback() { wx.navigateTo({ url: `/pages/feedback/index?planId=${this.data.id}` }) },
  onShareAppMessage() { return { title: '我的形象改造清单｜怎么打扮', path: '/pages/home/index' } }
})
