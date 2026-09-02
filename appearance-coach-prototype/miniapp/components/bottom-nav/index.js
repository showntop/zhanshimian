Component({
  properties: { active: { type: String, value: 'home' } },
  methods: {
    navigate(event) {
      const target = event.currentTarget.dataset.target
      if (target === this.data.active) return

      let targetUrl = ''
      if (target === 'home') targetUrl = '/pages/home/index'
      if (target === 'mine') targetUrl = '/pages/profile/index'
      if (target === 'plans') {
        const reportID = wx.getStorageSync('jianwo_report_id')
        if (!reportID) { wx.showToast({ title: '完成分析后查看方案', icon: 'none' }); return }
        targetUrl = `/pages/plans/index?reportId=${reportID}&tab=1`
      }
      if (!targetUrl) return

      const targetPath = targetUrl.split('?')[0].replace(/^\//, '')
      const pages = getCurrentPages()
      // If the target page is already in the stack, navigate back to its existing
      // instance instead of re-launching it. This keeps page state (scroll, data,
      // images) and avoids replaying entrance animations on every tab switch.
      const index = pages.findIndex((page) => page.route === targetPath)
      if (index !== -1) {
        const delta = pages.length - 1 - index
        if (delta > 0) { wx.navigateBack({ delta }); return }
      }
      // Fade transition feels closer to a native tab switch than the default push.
      wx.navigateTo({ url: targetUrl, animationType: 'fade', animationDuration: 200 })
    }
  }
})
