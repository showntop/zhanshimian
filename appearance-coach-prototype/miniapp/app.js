App({
  globalData: {
    apiBaseURL: 'http://localhost:8080',
    token: '',
    user: null,
    analysisID: '',
    reportID: '',
    planID: ''
  },

  onLaunch() {
    this.globalData.token = wx.getStorageSync('jianwo_token') || ''
    this.globalData.reportID = wx.getStorageSync('jianwo_report_id') || ''
    this.globalData.planID = wx.getStorageSync('jianwo_plan_id') || ''
  }
})
