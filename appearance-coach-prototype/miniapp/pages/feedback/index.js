const api = require('../../services/api')

Page({
  data: {
    planId: '',
    tags: ['很像我', '更有精神', '容易做到', '不够自然'].map((label) => ({ label, active: false })),
    selected: [],
    comment: '',
    feedbackImage: '',
    loading: false,
    done: false
  },
  onLoad(options) { this.setData({ planId: options.planId || wx.getStorageSync('jianwo_plan_id') }) },
  toggle(event) {
    const tag = event.currentTarget.dataset.tag
    const selected = this.data.selected.includes(tag) ? this.data.selected.filter((item) => item !== tag) : [...this.data.selected, tag]
    const tags = this.data.tags.map((item) => ({ ...item, active: selected.includes(item.label) }))
    this.setData({ selected, tags })
  },
  input(event) { this.setData({ comment: event.detail.value }) },
  choosePhoto() {
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => this.setData({ feedbackImage: tempFiles[0].tempFilePath }) })
  },
  submit() {
    if (!this.data.selected.length) return
    this.setData({ loading: true })
    api.addFeedback({ plan_id: this.data.planId, tags: this.data.selected, comment: this.data.comment }).then(() => this.setData({ done: true })).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ loading: false }))
  },
  home() { wx.reLaunch({ url: '/pages/home/index' }) }
})
