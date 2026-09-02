const api = require('../../services/api')
const { lookImage, userImage } = require('../../utils/media')

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
    todayLookUrl: '/assets/plans/sharp.jpg',
    currentLookUrl: '',
    referenceLookUrl: '/assets/reports/sharp.jpg',
    referenceGenerated: false,
    referenceDemo: false,
    comparisonTitle: '清晰利落',
    todayPlan: null,
    previewOpen: false,
    todayOpening: false,
    tasks: [],
    initialized: false
  },
  onLoad() {
    // Trigger entrance animations only on first page load, not on tab switch.
    this.setData({ initialized: true })
  },
  onShow() {
    api.trackEvent('page_view', { page: 'home' }).catch(() => {})
    const reportID = wx.getStorageSync('jianwo_report_id') || ''
    const hadProfile = this.data.hasProfile
    const hasProfile = Boolean(reportID)
    // Avoid wiping image URLs while refreshing so the screen doesn't flash
    // from placeholder/empty back to real images when returning from another tab.
    const update = { reportID, todayOpening: false }
    if (hadProfile !== hasProfile) update.hasProfile = hasProfile
    this.setData(update)
    this.refreshTasks(reportID)
    if (reportID) {
      api.getTodayPlan().then((todayPlan) => {
        if (todayPlan) this.setData({ todayPlan, todayLookUrl: lookImage(todayPlan.image_url, 'sharp', 'full') })
      }).catch(() => {})
      Promise.all([api.getReport(reportID), api.getPlans(reportID)]).then((results) => {
        const report = results[0]
        const plans = results[1]
        const featured = plans.find((item) => item.recommended) || plans[0]
        const generatedURL = featured && userImage(featured.generated_image_url)
        this.setData({
          currentLookUrl: userImage(report.current_image_url),
          referenceLookUrl: featured ? (generatedURL || lookImage(featured.image_url, featured.slug, 'report')) : '/assets/reports/sharp.jpg',
          referenceGenerated: Boolean(generatedURL),
          referenceDemo: Boolean(featured && (/^demo\//.test(featured.look_provider || '') || /^demo-/.test(featured.look_provider || ''))),
          comparisonTitle: featured ? featured.name : '清晰利落'
        })
      }).catch(() => {})
    }
  },
  refreshTasks(reportID) {
    const requests = []
    const activeAnalysisID = wx.getStorageSync('jianwo_active_analysis_id') || ''
    if (activeAnalysisID) {
      requests.push(api.getAnalysis(activeAnalysisID).then((analysis) => {
        if (analysis.status === 'completed' && analysis.report_id) {
          wx.setStorageSync('jianwo_report_id', analysis.report_id)
          this.setData({ hasProfile: true, reportID: analysis.report_id })
        }
        return {
          kind: 'analysis', id: analysis.id, reportId: analysis.report_id || '', state: analysis.status,
          title: analysis.status === 'completed' ? '形象分析已经完成' : (analysis.status === 'failed' ? '形象分析未完成' : '正在分析 3 张形象照片'),
          note: analysis.status === 'completed' ? '查看报告与三套方向' : (analysis.status === 'failed' ? '查看原因并重新提交' : `${analysis.progress || 0}% · ${analysis.stage || '正在处理'}`),
          action: analysis.status === 'completed' ? '查看报告' : (analysis.status === 'failed' ? '重新分析' : '查看进度')
        }
      }).catch(() => null))
    }
    const activePlan = wx.getStorageSync('jianwo_active_plan_generation')
    const planReportID = activePlan && activePlan.reportId || reportID
    if (activePlan && planReportID) {
      requests.push(api.getPlans(planReportID, activePlan.scene || '').then((plans) => {
        const completed = plans.filter((item) => item.generation_status === 'completed').length
        const working = plans.some((item) => item.generation_status === 'queued' || item.generation_status === 'processing')
        const failed = plans.length > 0 && plans.every((item) => item.generation_status === 'failed')
        return {
          kind: 'plans', reportId: planReportID, scene: activePlan.scene || '', state: working ? 'processing' : (failed ? 'failed' : 'completed'),
          title: working ? '3 套本人方案正在生成' : (failed ? '本人方案生成未完成' : '3 套本人方案已经完成'),
          note: working ? `${completed} / ${plans.length} 套完成 · 可退出后继续生成` : (failed ? '可以重新尝试，分析报告不会丢失' : '脸部、发型与全身穿搭都已准备好'),
          action: working ? '查看进度' : (failed ? '重新生成' : '查看方案')
        }
      }).catch(() => null))
    }
    const activeHairID = wx.getStorageSync('jianwo_active_hair_preview') || ''
    if (activeHairID) {
      requests.push(api.getHairPreview(activeHairID).then((preview) => ({
        kind: 'hair', id: preview.id, state: preview.status,
        title: preview.status === 'completed' ? '发型预览已经完成' : (preview.status === 'failed' ? '发型预览未完成' : '发型预览正在生成'),
        note: preview.status === 'completed' ? `${preview.style_name} · 查看并决定是否保存` : (preview.status === 'failed' ? '可以更换照片或重新生成' : `${preview.progress || 0}% · ${preview.stage || '正在处理'}`),
        action: preview.status === 'completed' ? '查看结果' : (preview.status === 'failed' ? '重新尝试' : '查看进度')
      })).catch(() => null))
    }
    Promise.all(requests).then((tasks) => this.setData({ tasks: tasks.filter(Boolean) }))
  },
  openTask(event) {
    const task = this.data.tasks[Number(event.currentTarget.dataset.index)]
    if (!task) return
    if (task.kind === 'analysis') {
      if (task.state === 'completed' && task.reportId) { wx.removeStorageSync('jianwo_active_analysis_id'); wx.navigateTo({ url: `/pages/report/index?id=${task.reportId}` }); return }
      if (task.state === 'failed') { wx.removeStorageSync('jianwo_active_analysis_id'); wx.navigateTo({ url: '/pages/capture/index?scene=general&replace=1' }); return }
      wx.navigateTo({ url: `/pages/analysis/index?id=${task.id}&scene=general` }); return
    }
    if (task.kind === 'plans') {
      if (task.scene) wx.setStorageSync('jianwo_plans_scene', task.scene)
      wx.switchTab({ url: '/pages/plans/index' }); return
    }
    if (task.kind === 'hair') wx.navigateTo({ url: '/pages/hair/index' })
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
  openToday() { if (this.data.todayOpening) return; this.setData({ todayOpening: true }); wx.navigateTo({ url: '/pages/today/index', fail: () => this.setData({ todayOpening: false }) }) },
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
    if (this.data.reportID) wx.switchTab({ url: '/pages/plans/index' })
    else wx.navigateTo({ url: '/pages/capture/index?scene=general&demo=1' })
  },
  openLab() { wx.navigateTo({ url: '/pages/lab/index' }) },
  onShareAppMessage() { return { title: '怎么打扮｜你的 AI 形象顾问', path: '/pages/home/index' } }
})
