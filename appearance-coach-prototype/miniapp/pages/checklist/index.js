const api = require('../../services/api')
const { lookImage, exampleImage, isBundledAsset } = require('../../utils/media')

Page({
  data: { id: '', plan: null, items: [], completed: 0, loading: true, loadError: false },
  onLoad(options) { this.setData({ id: options.id || wx.getStorageSync('jianwo_plan_id') }); this.load() },
  load() {
    this.setData({ loading: true, loadError: false })
    Promise.all([api.getPlan(this.data.id), api.getChecklist(this.data.id)]).then((results) => {
      const plan = results[0]
      const items = results[1]
      // 方案图 URL 无效时显式回退到内置示例图,wxml 用 plan.imageExample 叠加「风格参考」角标
      const planImageURL = lookImage(plan.image_url)
      plan.image_url = planImageURL || exampleImage(plan.slug, 'plan')
      plan.imageExample = !planImageURL || isBundledAsset(planImageURL)
      this.setData({ plan, items, completed: items.filter((item) => item.completed).length, loading: false })
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false, loadError: true }) })
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
