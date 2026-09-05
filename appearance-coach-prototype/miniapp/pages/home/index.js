const api = require('../../services/api')
const { lookImage, exampleImage, userImage, isBundledAsset } = require('../../utils/media')

function greetingByHour() {
  const hour = new Date().getHours()
  if (hour < 6) return '夜深了'
  if (hour < 9) return '早上好'
  if (hour < 11) return '上午好'
  if (hour < 14) return '中午好'
  if (hour < 18) return '下午好'
  return '晚上好'
}

Page({
  data: {
    scenes: [
      { id: 'interview', label: '面试', note: '精神可信', icon: '/assets/icons/briefcase.png' },
      { id: 'wedding', label: '婚礼', note: '得体上镜', icon: '/assets/icons/heart.png' },
      { id: 'date', label: '约会', note: '自然有记忆点', icon: '/assets/icons/bookmark.png' },
      { id: 'daily', label: '日常', note: '省心耐看', icon: '/assets/icons/sun.png' }
    ],
    tools: [
      { id: 'hair', label: '发型预览', note: '先看再决定', icon: '/assets/capture/face-line.png', badge: '推荐' },
      { id: 'outfit', label: '穿搭诊断', note: '今天怎么改', icon: '/assets/icons/document.png' },
      { id: 'purchase', label: '购买判断', note: '这件适合吗', icon: '/assets/icons/shield-check.png' },
      { id: 'advisor', label: '问顾问', note: '继续调整方案', icon: '/assets/icons/user.png' }
    ],
    hasProfile: false,
    reportID: '',
    greeting: greetingByHour(),
    todayContext: null,
    todayLookUrl: '',
    todayLookExample: false,
    currentLookUrl: '',
    referenceLookUrl: '',
    referenceGenerated: false,
    referenceDemo: false,
    comparisonTitle: '',
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
    api.trackEvent('page_view', { page: 'home' }).catch((error) => console.warn('[home] 埋点上报失败', error))
    const reportID = wx.getStorageSync('jianwo_report_id') || ''
    if (!reportID && !this.reportRecovered) {
      // A reinstalled mini-program wipes local cache but the report still
      // lives on the server; recover the latest one once per session.
      this.reportRecovered = true
      api.getCurrentReport()
        .then((report) => {
          wx.setStorageSync('jianwo_report_id', report.id)
          this.onShow()
        })
        .catch((error) => console.warn('[home] 历史报告恢复失败', error))
      return
    }
    const hadProfile = this.data.hasProfile
    const hasProfile = Boolean(reportID)
    // Avoid wiping image URLs while refreshing so the screen doesn't flash
    // from placeholder/empty back to real images when returning from another tab.
    const update = { reportID, todayOpening: false, greeting: greetingByHour() }
    if (hadProfile !== hasProfile) update.hasProfile = hasProfile
    this.setData(update)
    this.refreshTasks(reportID)
    if (reportID) {
      api.getTodayPlan().then((todayPlan) => {
        if (todayPlan) {
          // 方案图 URL 无效时显式回退到内置示例图,wxml 用 todayLookExample 叠加「风格参考」角标
          const lookURL = lookImage(todayPlan.image_url)
          this.setData({ todayPlan, todayContext: todayPlan.context, todayLookUrl: lookURL || exampleImage('sharp', 'full'), todayLookExample: !lookURL || isBundledAsset(lookURL) })
        } else {
          this.setData({ todayPlan: null, todayLookUrl: '', todayLookExample: false })
          this.loadTodayContext()
        }
      }).catch((error) => {
        console.warn('[home] 今日方案加载失败', error)
        // 今日卡进入空态:wxml 显示「今天的造型建议还没生成」与占位缩略图
        this.setData({ todayPlan: null, todayLookUrl: '', todayLookExample: false })
        this.loadTodayContext()
      })
      Promise.all([api.getReport(reportID), api.getPlans(reportID)]).then((results) => {
        const report = results[0]
        const plans = results[1]
        const featured = plans.find((item) => item.recommended) || plans[0]
        const generatedURL = featured && userImage(featured.generated_image_url)
        const referenceURL = featured ? (generatedURL || lookImage(featured.image_url)) : ''
        this.setData({
          currentLookUrl: userImage(report.current_image_url),
          referenceLookUrl: referenceURL || (featured ? exampleImage(featured.slug, 'report') : ''),
          referenceGenerated: Boolean(generatedURL),
          referenceDemo: Boolean(featured && (/^demo\//.test(featured.look_provider || '') || /^demo-/.test(featured.look_provider || ''))),
          comparisonTitle: featured ? featured.name : ''
        })
      }).catch((error) => {
        console.warn('[home] 方案对比卡加载失败', error)
        // 对比卡进入空态:wxml 的 referenceGenerated/referenceDemo 守卫会显示「生成本人方案后可对比」
        this.setData({ currentLookUrl: '', referenceLookUrl: '', referenceGenerated: false, referenceDemo: false, comparisonTitle: '' })
      })
    }
  },
  loadTodayContext() {
    // 今日方案还没生成时也展示真实天气，不再用写死的"上海 · 多云 · 26°C"
    api.getTodayContext(wx.getStorageSync('jianwo_city') || '').then((context) => {
      if (context) this.setData({ todayContext: context })
    }).catch((error) => console.warn('[home] 今日天气加载失败', error))
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
          kind: 'analysis', id: analysis.id, reportId: analysis.report_id || '', state: analysis.status, scene: analysis.scene || 'general',
          title: analysis.status === 'completed' ? '形象分析已经完成' : (analysis.status === 'failed' ? '形象分析未完成' : '正在分析 3 张形象照片'),
          note: analysis.status === 'completed' ? '查看报告与三套方向' : (analysis.status === 'failed' ? '查看原因并重新提交' : `${analysis.progress || 0}% · ${analysis.stage || '正在处理'}`),
          action: analysis.status === 'completed' ? '查看报告' : (analysis.status === 'failed' ? '重新分析' : '查看进度')
        }
      }).catch((error) => { console.warn('[home] 分析任务状态加载失败', error); return null }))
    }
    const activePlan = wx.getStorageSync('jianwo_active_plan_generation')
    const planReportID = activePlan && activePlan.reportId || reportID
    if (activePlan && planReportID) {
      requests.push(api.getPlans(planReportID, activePlan.scene || '').then((plans) => {
        const completed = plans.filter((item) => item.generation_status === 'completed').length
        const working = plans.some((item) => item.generation_status === 'queued' || item.generation_status === 'processing')
        const failedCount = plans.filter((item) => item.generation_status === 'failed').length
        // plans 从未生成时全是 idle，不能兜底显示“已完成”
        const idle = plans.length > 0 && completed === 0 && failedCount === 0
        let task
        if (working) {
          task = { state: 'processing', title: '3 套本人方案正在生成', note: `${completed} / ${plans.length} 套完成 · 可退出后继续生成`, action: '查看进度' }
        } else if (plans.length > 0 && failedCount === plans.length) {
          task = { state: 'failed', title: '本人方案生成未完成', note: '可以重新尝试，分析报告不会丢失', action: '重新生成' }
        } else if (idle) {
          task = { state: 'idle', title: '尚未生成本人方案', note: '分析报告已就绪，随时可以生成', action: '去生成' }
        } else if (failedCount > 0) {
          task = { state: 'partial', title: `${completed} / ${plans.length} 套本人方案已完成`, note: `${failedCount} 套可重试`, action: '查看方案' }
        } else {
          task = { state: 'completed', title: '3 套本人方案已经完成', note: '脸部、发型与全身穿搭都已准备好', action: '查看方案' }
        }
        return { kind: 'plans', reportId: planReportID, scene: activePlan.scene || '', ...task }
      }).catch((error) => { console.warn('[home] 方案任务状态加载失败', error); return null }))
    }
    const activeHairID = wx.getStorageSync('jianwo_active_hair_preview') || ''
    if (activeHairID) {
      requests.push(api.getHairPreview(activeHairID).then((preview) => ({
        kind: 'hair', id: preview.id, state: preview.status,
        title: preview.status === 'completed' ? '发型预览已经完成' : (preview.status === 'failed' ? '发型预览未完成' : '发型预览正在生成'),
        note: preview.status === 'completed' ? `${preview.style_name} · 查看并决定是否保存` : (preview.status === 'failed' ? '可以更换照片或重新生成' : `${preview.progress || 0}% · ${preview.stage || '正在处理'}`),
        action: preview.status === 'completed' ? '查看结果' : (preview.status === 'failed' ? '重新尝试' : '查看进度')
      })).catch((error) => { console.warn('[home] 发型预览任务状态加载失败', error); return null }))
    }
    Promise.all(requests).then((tasks) => this.setData({ tasks: tasks.filter(Boolean) }))
  },
  openTask(event) {
    const task = this.data.tasks[Number(event.currentTarget.dataset.index)]
    if (!task) return
    if (task.kind === 'analysis') {
      const scene = task.scene || 'general'
      if (task.state === 'completed' && task.reportId) {
        wx.removeStorageSync('jianwo_active_analysis_id')
        // 用户中途退出导致场景方案没来得及创建时，在这里补上，场景流程不再断链
        const brief = wx.getStorageSync('jianwo_scene_brief')
        if (scene !== 'general' && brief && brief.scene === scene) {
          api.createScenePlans(task.reportId, brief).catch((error) => console.warn('[home] 场景方案补建失败', error)).finally(() => wx.navigateTo({ url: `/pages/report/index?id=${task.reportId}&scene=${scene}` }))
          return
        }
        wx.navigateTo({ url: `/pages/report/index?id=${task.reportId}` }); return
      }
      if (task.state === 'failed') { wx.removeStorageSync('jianwo_active_analysis_id'); wx.navigateTo({ url: `/pages/capture/index?scene=${scene}&replace=1` }); return }
      wx.navigateTo({ url: `/pages/analysis/index?id=${task.id}&scene=${scene}` }); return
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
    if (!this.data.todayLookUrl) return
    this.setData({ previewOpen: true })
  },
  closeTodayLook() { this.setData({ previewOpen: false }) },
  start() { wx.navigateTo({ url: '/pages/capture/index?scene=general' }) },
  openReport() {
    if (this.data.reportID) { wx.navigateTo({ url: `/pages/report/index?id=${this.data.reportID}` }); return }
    this.openExample()
  },
  openExample() {
    if (this.data.reportID) wx.switchTab({ url: '/pages/plans/index' })
    else wx.navigateTo({ url: '/pages/capture/index?scene=general&demo=1' })
  },
  openLab() { wx.navigateTo({ url: '/pages/lab/index' }) },
  onShareAppMessage() { return { title: '怎么打扮｜你的私人形象顾问', path: '/pages/home/index' } }
})
