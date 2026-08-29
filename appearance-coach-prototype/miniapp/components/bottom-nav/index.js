Component({
  properties: { active: { type: String, value: 'home' } },
  methods: {
    navigate(event) {
      const target = event.currentTarget.dataset.target
      if (target === this.data.active) return
      if (target === 'home') wx.reLaunch({ url: '/pages/home/index' })
      if (target === 'mine') wx.navigateTo({ url: '/pages/profile/index' })
      if (target === 'plans') {
        const reportID = wx.getStorageSync('jianwo_report_id')
        if (reportID) wx.navigateTo({ url: `/pages/plans/index?reportId=${reportID}&tab=1` })
        else wx.showToast({ title: '完成分析后查看方案', icon: 'none' })
      }
    }
  }
})
