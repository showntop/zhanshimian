const api = require('../../services/api')
const { lookImage } = require('../../utils/media')

Page({
  data: {
    planId: '',
    tags: ['很像我', '更有精神', '容易做到', '不够自然'].map((label) => ({ label, active: false })),
    selected: [],
    comment: '',
    feedbackImage: '',
    referenceImage: '',
    loading: false,
    done: false
  },
  onLoad(options) {
    const planId = options.planId || wx.getStorageSync('jianwo_plan_id')
    this.setData({ planId })
    if (!planId) return
    // 方案参考图 URL 无效就保持为空,wxml 显示空态,不再默认展示内置模特图
    api.getPlan(planId).then((plan) => this.setData({ referenceImage: lookImage(plan.image_url) })).catch((error) => console.warn('[feedback] 方案参考图加载失败', error))
  },
  toggle(event) {
    const tag = event.currentTarget.dataset.tag
    const selected = this.data.selected.includes(tag) ? this.data.selected.filter((item) => item !== tag) : [...this.data.selected, tag]
    const tags = this.data.tags.map((item) => ({ ...item, active: selected.includes(item.label) }))
    this.setData({ selected, tags })
  },
  input(event) { this.setData({ comment: event.detail.value }) },
  choosePhoto() {
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => { this.feedbackMediaID = ''; this.setData({ feedbackImage: tempFiles[0].tempFilePath }) } })
  },
  submit() {
    if (!this.data.selected.length || this.data.loading) return
    this.setData({ loading: true })
    const send = () => api.addFeedback({ plan_id: this.data.planId, tags: this.data.selected, comment: this.data.comment, media_id: this.feedbackMediaID || '' })
      .then(() => this.setData({ done: true }))
      .catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
      .finally(() => this.setData({ loading: false }))
    // 用户选了实拍就必须真的上传，不再静默丢弃
    if (!this.data.feedbackImage || this.feedbackMediaID) { send(); return }
    wx.showLoading({ title: '上传实拍中' })
    api.uploadMedia('feedback', this.data.feedbackImage)
      .then((asset) => { this.feedbackMediaID = asset.id; send() })
      .catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
      .finally(() => wx.hideLoading())
  },
  home() { wx.switchTab({ url: '/pages/home/index' }) }
})
