const api = require('../../services/api')
const { lookImage, exampleImage, userImage } = require('../../utils/media')

const SCENE_LABELS = { interview: '面试', wedding: '婚礼', date: '约会', daily: '日常' }

Page({
  data: {
    reportId: '', scene: '', plans: [], savedHair: [], selected: 0, showCurrent: false, status: 'loading', errorMessage: '',
    sceneLabel: '', asTab: true, generating: false, allIdle: false, allFailed: false, allReady: false,
    hasReport: false, generationCount: 0, currentFaceUrl: '', currentBodyUrl: '',
    sceneTabs: [{ id: '', label: '形象方案' }, { id: 'interview', label: '面试' }, { id: 'wedding', label: '婚礼' }, { id: 'date', label: '约会' }, { id: 'daily', label: '日常' }]
  },
  onLoad() {
    // onLoad 后紧跟的首次 onShow 不再重复加载
    this.skipNextShow = true
    this.applyIntent(true)
  },
  onShow() {
    // Tab pages only fire onShow (not onLoad) when revisited via the tab bar.
    // Re-apply any pending scene intent stored by the caller before switchTab.
    if (this.skipNextShow) { this.skipNextShow = false; return }
    this.applyIntent()
  },
  applyIntent(force = false) {
    const intent = wx.getStorageSync('jianwo_plans_scene') || ''
    if (intent) wx.removeStorageSync('jianwo_plans_scene')
    const reportId = wx.getStorageSync('jianwo_report_id') || ''
    // 有待进入的场景意图才切换场景；否则保持当前场景，场景方案不会"只能看一次"
    const scene = intent || (force ? '' : this.data.scene)
    const changed = scene !== this.data.scene || reportId !== this.data.reportId
    if (force || changed) {
      this.setData({ reportId, scene, sceneLabel: SCENE_LABELS[scene] || '', hasReport: Boolean(reportId), status: 'loading', errorMessage: '' })
      this.load()
      return
    }
    // 场景未变时也刷新：生成进度、新方案图和跨页改动需要及时反映
    this.load()
  },
  switchScene(event) {
    const scene = event.currentTarget.dataset.id
    if (scene === this.data.scene || !this.data.reportId) return
    if (this.timer) clearTimeout(this.timer)
    this.setData({ scene, sceneLabel: SCENE_LABELS[scene] || '', plans: [], status: 'loading', errorMessage: '', allIdle: false, allFailed: false, allReady: false })
    this.load()
  },
  openSceneBrief() {
    if (this.data.scene) wx.navigateTo({ url: `/pages/scene/index?scene=${this.data.scene}` })
  },
  onUnload() { if (this.timer) clearTimeout(this.timer) },
  retry() { this.setData({ status: 'loading', errorMessage: '' }); this.load() },
  mapPlans(plans, currentFaceUrl = this.data.currentFaceUrl, currentBodyUrl = this.data.currentBodyUrl) {
    return (plans || []).map((plan) => {
      const generated = Boolean(plan.generated_image_url)
      const isDemo = /^demo\//.test(plan.look_provider || '') || /^demo-/.test(plan.look_provider || '')
      const status = plan.generation_status || 'idle'
      const generatedURL = generated ? userImage(plan.generated_image_url) : ''
      const slug = plan.slug || 'natural'
      // 无本人生成图时,图一律来自 API 示例或内置模特图,卡片必须叠加「风格参考」角标
      const example = !generatedURL
      return {
        ...plan,
        generated,
        isDemo,
        generating: status === 'queued' || status === 'processing',
        generateFailed: status === 'failed',
        example,
        display_url: generatedURL || lookImage(plan.image_url) || exampleImage(slug, 'plan'),
        display_mode: generatedURL ? 'aspectFit' : 'aspectFill',
        current_image_url: currentBodyUrl || currentFaceUrl || userImage(plan.current_image_url),
        current_image_mode: currentBodyUrl ? 'aspectFit' : 'aspectFill',
        thumbnail_url: generatedURL || lookImage(plan.image_url) || exampleImage(slug, 'portrait'),
        thumbnail_mode: generatedURL ? 'aspectFit' : 'aspectFill',
        look_label: generated ? '本人方案' : '',
        compare_label: generated ? '本人方案' : '风格参考'
      }
    })
  },
  load() {
    const planRequest = this.data.reportId ? api.getPlans(this.data.reportId, this.data.scene) : Promise.resolve([])
    const mediaRequest = this.data.reportId
      ? api.getReport(this.data.reportId).then((report) => api.getAnalysis(report.analysis_id)).catch(() => null)
      : Promise.resolve(null)
    Promise.all([planRequest, api.getSavedHairPreviews(), mediaRequest]).then((results) => {
      const plans = results[0]
      const savedHair = results[1]
      const analysis = results[2]
      const media = analysis && analysis.media || []
      const face = media.find((item) => item.kind === 'face')
      const body = media.find((item) => item.kind === 'body')
      const currentFaceUrl = userImage(face && face.url)
      const currentBodyUrl = userImage(body && body.url)
      const mapped = this.mapPlans(plans, currentFaceUrl, currentBodyUrl)
      const mappedHair = savedHair.map((item) => ({ ...item, preview_label: (item.provider_version || '').indexOf('demo') === 0 ? '风格参考' : '本人效果预览' }))
      const allIdle = mapped.length > 0 && mapped.every((item) => !item.generated && !item.generating && !item.generateFailed)
      const allFailed = mapped.length > 0 && mapped.every((item) => item.generateFailed)
      const allReady = mapped.length > 0 && mapped.every((item) => item.generated)
      const generationCount = mapped.filter((item) => item.generated).length
      this.setData({ plans: mapped, savedHair: mappedHair, allIdle, allFailed, allReady, generationCount, currentFaceUrl, currentBodyUrl, selected: Math.max(0, mapped.findIndex((item) => item.recommended)), status: mapped.length ? 'ready' : 'empty', errorMessage: '' })
      if (allReady) wx.removeStorageSync('jianwo_active_plan_generation')
      this.schedulePoll(mapped)
    }).catch((error) => { this.setData({ plans: [], status: 'error', errorMessage: (error && error.message) || '网络异常' }) })
  },
  schedulePoll(plans) {
    if (this.timer) clearTimeout(this.timer)
    if ((plans || []).some((item) => item.generating)) this.timer = setTimeout(() => this.load(), 2500)
  },
  generate() {
    if (this.data.generating || !this.data.reportId) return
    const active = wx.getStorageSync('jianwo_active_plan_generation')
    if (active && (active.reportId !== this.data.reportId || (active.scene || '') !== (this.data.scene || ''))) {
      wx.showToast({ title: '已有一组方案正在生成', icon: 'none' })
      if (active.scene) wx.setStorageSync('jianwo_plans_scene', active.scene)
      wx.switchTab({ url: '/pages/plans/index' })
      return
    }
    this.setData({ generating: true })
    const refresh = this.data.plans.some((item) => item.generation_status === 'completed' && !item.generated)
    api.generatePlanLooks(this.data.reportId, this.data.scene, refresh)
      .then((plans) => {
        // 入队成功后再记录进行中的生成，避免失败后 key 残留误导首页
        wx.setStorageSync('jianwo_active_plan_generation', { reportId: this.data.reportId, scene: this.data.scene })
        const mapped = this.mapPlans(plans)
        this.setData({ plans: mapped, allIdle: false, allFailed: false, status: mapped.length ? 'ready' : 'empty' })
        this.schedulePoll(mapped)
      })
      .catch((error) => wx.showToast({ title: error.message || '生成暂时不可用', icon: 'none' }))
      .finally(() => this.setData({ generating: false }))
  },
  // 服务端没有单套重试接口,失败卡片重试 = 整组重新入队,toast 说明
  retryPlan() {
    wx.showToast({ title: '已重新发起生成，三套方案会一起更新', icon: 'none' })
    this.generate()
  },
  swiperChange(event) { this.setData({ selected: event.detail.current, showCurrent: false }); wx.vibrateShort({ type: 'light' }) },
  selectThumb(event) { this.setData({ selected: Number(event.currentTarget.dataset.index), showCurrent: false }) },
  toggleCompare(event) {
    const showCurrent = event.currentTarget.dataset.mode === 'current'
    const plan = this.data.plans[this.data.selected]
    if (showCurrent && (!plan || !plan.current_image_url)) {
      wx.showToast({ title: '当前照片暂时不可用', icon: 'none' })
      return
    }
    this.setData({ showCurrent })
  },
  primaryPlanAction() {
    const plan = this.data.plans[this.data.selected]
    if (!plan) return
    if (plan.generating) {
      wx.showToast({ title: '生成完成后才能查看详情', icon: 'none' })
      return
    }
    if (!plan.generated) {
      this.generate()
      return
    }
    wx.navigateTo({ url: `/pages/plan/index?id=${plan.id}` })
  },
  startAnalysis() { wx.navigateTo({ url: '/pages/capture/index?scene=general' }) },
  onShareAppMessage() { return { title: '同一个我，三种更适合的表达｜怎么打扮', path: '/pages/home/index' } }
})
