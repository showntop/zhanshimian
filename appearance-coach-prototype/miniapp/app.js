const runtime = require('./config/runtime')

function environmentVersion() {
  try {
    const account = wx.getAccountInfoSync()
    return account && account.miniProgram && account.miniProgram.envVersion || 'develop'
  } catch (_) {
    return 'develop'
  }
}

function resolveAPIBaseURL() {
  let extConfig = {}
  try { extConfig = wx.getExtConfigSync ? wx.getExtConfigSync() || {} : {} } catch (_) {}
  const configured = extConfig.apiBaseURL || runtime.apiBaseURLs[environmentVersion()] || ''
  return configured.replace(/\/$/, '')
}

App({
  globalData: {
    apiBaseURL: resolveAPIBaseURL(),
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
