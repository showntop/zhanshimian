const api = require('../../services/api')

const categories = [
  { value: 'top', label: '上装' }, { value: 'bottom', label: '下装' }, { value: 'outer', label: '外套' }, { value: 'shoes', label: '鞋' }, { value: 'bag', label: '包' }
]

Page({
  data: { items: [], filtered: [], categories, active: 'all', adding: false, saving: false, outfit: null, loadError: false, form: { media_id: '', path: '', name: '', category: 'top', color: '黑色' } },
  onShow() { api.trackEvent('page_view', { page: 'wardrobe' }).catch(() => {}); this.load() },
  load() { this.setData({ loadError: false }); api.getWardrobeItems().then((items) => this.setItems(items)).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loadError: true }) }) },
  setItems(items) { this.setData({ items, filtered: this.data.active === 'all' ? items : items.filter((item) => item.category === this.data.active) }) },
  filter(event) { const active = event.currentTarget.dataset.value; this.setData({ active, filtered: active === 'all' ? this.data.items : this.data.items.filter((item) => item.category === active) }) },
  openAdd() { this.setData({ adding: true, form: { media_id: '', path: '', name: '', category: 'top', color: '黑色' } }) },
  closeAdd() { this.setData({ adding: false }) },
  stop() {},
  inputName(event) { this.setData({ 'form.name': event.detail.value }) },
  inputColor(event) { this.setData({ 'form.color': event.detail.value }) },
  chooseCategory(event) { this.setData({ 'form.category': event.currentTarget.dataset.value }) },
  choosePhoto() {
    wx.chooseMedia({ count: 1, mediaType: ['image'], sourceType: ['album', 'camera'], sizeType: ['compressed'], success: ({ tempFiles }) => {
      const path = tempFiles[0].tempFilePath; wx.showLoading({ title: '上传中' })
      api.uploadMedia('wardrobe', path).then((asset) => this.setData({ 'form.media_id': asset.id, 'form.path': path })).catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => wx.hideLoading())
    } })
  },
  saveItem() {
    this.setData({ saving: true })
    const form = this.data.form
    api.createWardrobeItem({ media_id: form.media_id, name: form.name || `${categories.find((item) => item.value === form.category).label}单品`, category: form.category, color: form.color, scenes: ['daily'] })
      .then((item) => { this.setItems([item, ...this.data.items]); this.setData({ adding: false }); api.trackEvent('wardrobe_item_add', { item_id: item.id, category: item.category }).catch(() => {}); wx.showToast({ title: '已加入衣橱', icon: 'success' }) })
      .catch((error) => wx.showToast({ title: error.message, icon: 'none' })).finally(() => this.setData({ saving: false }))
  },
  remove(event) {
    const id = event.currentTarget.dataset.id
    wx.showModal({ title: '移除这件单品？', content: '不会删除你手机相册中的照片。', success: ({ confirm }) => { if (confirm) api.deleteWardrobeItem(id).then(() => this.setItems(this.data.items.filter((item) => item.id !== id))).catch((error) => wx.showToast({ title: error.message, icon: 'none' })) } })
  },
  createOutfit() {
    api.getTodayContext().then((context) => api.createWardrobeOutfit(context)).then((outfit) => { this.setData({ outfit }); api.trackEvent('wardrobe_outfit_generate', { outfit_id: outfit.id, item_count: outfit.items.length }).catch(() => {}) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  },
  closeOutfit() { this.setData({ outfit: null }) },
  wearOutfit() { api.wearWardrobeOutfit(this.data.outfit.id).then((outfit) => { this.setData({ outfit }); api.trackEvent('wardrobe_outfit_wear', { outfit_id: outfit.id }).catch(() => {}); wx.showToast({ title: '已记录今天穿了', icon: 'success' }) }).catch((error) => wx.showToast({ title: error.message, icon: 'none' })) }
})
