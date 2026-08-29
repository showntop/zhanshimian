Page({
  data: {
    scenes: [
      { id: 'interview', label: '面试', note: '精神可信', icon: '/assets/icons/briefcase.svg' },
      { id: 'wedding', label: '婚礼', note: '得体上镜', icon: '/assets/icons/sparkles.svg' },
      { id: 'date', label: '约会', note: '自然有记忆点', icon: '/assets/icons/heart.svg' },
      { id: 'daily', label: '日常', note: '省心耐看', icon: '/assets/icons/sun.svg' }
    ],
    tools: [
      { id: 'hair', label: '发型预览', note: '先看再决定', icon: '/assets/capture/face-line.png', badge: '推荐' },
      { id: 'outfit', label: '穿搭诊断', note: '今天怎么改', icon: '/assets/icons/document.svg' },
      { id: 'purchase', label: '购买判断', note: '这件适合吗', icon: '/assets/icons/bookmark.svg' },
      { id: 'advisor', label: '问顾问', note: '继续调整方案', icon: '/assets/icons/sparkles.svg' }
    ],
    hasProfile: false,
    reportID: '',
    todayLookUrl: '/assets/plans/sharp.webp',
    todayPlan: null,
    previewOpen: false
  },
  onShow() {
	const api = require('../../services/api')
	api.trackEvent('page_view', { page: 'home' }).catch(() => {})
    const reportID = wx.getStorageSync('jianwo_report_id') || ''
    this.setData({ hasProfile: Boolean(reportID), reportID })
    if (reportID) {
      api.getTodayPlan().then((todayPlan) => {
        if (todayPlan) this.setData({ todayPlan, todayLookUrl: todayPlan.image_url })
      }).catch(() => {})
    }
  },
  chooseScene(event) {
    const scene = event.currentTarget.dataset.scene
    wx.navigateTo({ url: `/pages/scene/index?scene=${scene}` })
  },
  openTool(event) {
    const tool = event.currentTarget.dataset.tool
    const routes = { hair: '/pages/hair/index', outfit: '/pages/outfit/index', purchase: '/pages/purchase/index', advisor: '/pages/advisor/index' }
    wx.navigateTo({ url: routes[tool] })
  },
  openToday() { wx.navigateTo({ url: '/pages/today/index' }) },
  previewTodayLook() {
    this.setData({ previewOpen: true })
  },
  closeTodayLook() { this.setData({ previewOpen: false }) },
  noop() {},
  start() { wx.navigateTo({ url: '/pages/capture/index?scene=general' }) },
  openReport() {
    if (this.data.reportID) wx.navigateTo({ url: `/pages/report/index?id=${this.data.reportID}` })
  },
  openExample() {
    if (this.data.reportID) wx.navigateTo({ url: `/pages/plans/index?reportId=${this.data.reportID}` })
    else wx.navigateTo({ url: '/pages/capture/index?scene=general&demo=1' })
  },
  openLab() { wx.navigateTo({ url: '/pages/lab/index' }) },
  onShareAppMessage() { return { title: '见我｜你的私人形象顾问', path: '/pages/home/index' } }
})
