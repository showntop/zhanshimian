Page({
  onLoad(options) {
    if (options.feature === 'hair-ar') setTimeout(() => this.tryFeature(), 200)
  },
  tryFeature() {
    wx.showModal({ title: '发型 AR 内测', content: '当前版本展示交互流程；接入实时模型后会在相机画面中切换发型与妆容。', showCancel: false, confirmText: '知道了' })
  },
  joinWaitlist(event) {
    wx.setStorageSync(`jianwo_wait_${event.currentTarget.dataset.feature}`, true)
    wx.showToast({ title: '已加入体验名单', icon: 'success' })
  }
})
