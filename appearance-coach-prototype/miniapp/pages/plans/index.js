const api = require('../../services/api')
const { lookImage, userImage } = require('../../utils/media')

Page({
  data: {
    reportId: '', scene: '', plans: [], savedHair: [], selected: 0, showCurrent: false, loading: true,
    sceneLabel: '', asTab: true, generating: false, allIdle: false, allFailed: false, allReady: false,
    generationCount: 0, currentFaceUrl: '', currentBodyUrl: '', generationFailureMessage: ''
  },
  onLoad() {
    this.applyIntent(true)
  },
  onShow() {
    // Tab pages only fire onShow (not onLoad) when revisited via the tab bar.
    // Re-apply any pending scene intent stored by the caller before switchTab.
    this.applyIntent()
  },
  applyIntent(force = false) {
    const labels = { interview: '面试', wedding: '婚礼', date: '约会', daily: '日常' }
    const scene = wx.getStorageSync('jianwo_plans_scene') || ''
    const reportId = wx.getStorageSync('jianwo_report_id') || ''
    if (scene) wx.removeStorageSync('jianwo_plans_scene')
    const changed = scene !== this.data.scene || reportId !== this.data.reportId
    if (force || changed) {
      this.setData({ reportId, scene, sceneLabel: labels[scene] || '', loading: true })
      this.load()
    }
  },
  onUnload() { if (this.timer) clearTimeout(this.timer) },
  retry() { this.setData({ loading: true }); this.load() },
  mapPlans(plans, currentFaceUrl = this.data.currentFaceUrl, currentBodyUrl = this.data.currentBodyUrl) {
    return (plans || []).map((plan) => {
      const generated = Boolean(plan.generated_image_url)
      const isDemo = /^demo\//.test(plan.look_provider || '') || /^demo-/.test(plan.look_provider || '')
      const status = plan.generation_status || 'idle'
      const generatedURL = generated ? userImage(plan.generated_image_url) : ''
      return {
        ...plan,
        generated,
        isDemo,
        generating: status === 'queued' || status === 'processing',
        generateFailed: status === 'failed',
        generationFailureMessage: status === 'failed' ? friendlyGenerationFailure(plan.generation_error) : '',
        display_url: generatedURL || lookImage(plan.image_url, plan.slug, 'plan'),
        display_mode: generatedURL ? 'aspectFit' : 'aspectFill',
        current_image_url: currentBodyUrl || currentFaceUrl || userImage(plan.current_image_url),
        current_image_mode: currentBodyUrl ? 'aspectFit' : 'aspectFill',
        thumbnail_url: generatedURL || lookImage(plan.image_url, plan.slug, 'portrait'),
        thumbnail_mode: generatedURL ? 'aspectFit' : 'aspectFill',
        look_label: isDemo ? '免费流程预览' : (generated ? 'AI 本人方案' : '效果示例'),
        compare_label: isDemo ? '流程预览' : (generated ? '本人方案' : '风格参考')
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
      const mappedHair = savedHair.map((item) => ({ ...item, preview_label: (item.provider_version || '').indexOf('demo') === 0 ? '效果示例' : 'AI 本人预览' }))
      const allIdle = mapped.length > 0 && mapped.every((item) => !item.generated && !item.generating && !item.generateFailed)
      const allFailed = mapped.length > 0 && mapped.every((item) => item.generateFailed)
      const allReady = mapped.length > 0 && mapped.every((item) => item.generated)
      const generationCount = mapped.filter((item) => item.generated).length
      const firstFailure = mapped.find((item) => item.generateFailed)
      this.setData({ plans: mapped, savedHair: mappedHair, allIdle, allFailed, allReady, generationCount, currentFaceUrl, currentBodyUrl, generationFailureMessage: firstFailure ? firstFailure.generationFailureMessage : '', selected: Math.max(0, mapped.findIndex((item) => item.recommended)), loading: false })
      if (allReady) wx.removeStorageSync('jianwo_active_plan_generation')
      this.schedulePoll(mapped)
    }).catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false }) })
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
    wx.setStorageSync('jianwo_active_plan_generation', { reportId: this.data.reportId, scene: this.data.scene })
    const refresh = this.data.plans.some((item) => item.generation_status === 'completed' && !item.generated)
    api.generatePlanLooks(this.data.reportId, this.data.scene, refresh)
      .then((plans) => {
        const mapped = this.mapPlans(plans)
        this.setData({ plans: mapped, allIdle: false, allFailed: false, generationFailureMessage: '' })
        this.schedulePoll(mapped)
      })
      .catch((error) => wx.showToast({ title: error.message || '生成暂时不可用', icon: 'none' }))
      .finally(() => this.setData({ generating: false }))
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
  onShareAppMessage() { return { title: '同一个我，三种更适合的表达｜怎么打扮', path: '/pages/home/index' } }
})

// Provider errors are retained in the API for diagnostics, but should not be
// shown verbatim in the mini-program. In particular, credentials and vendor
// request IDs must never become user-facing copy.
function friendlyGenerationFailure(value) {
  const message = String(value || '')
  if (/401|403|AccessDenied|Unauthorized|Authentication|quota|unpurchased/i.test(message)) {
    return '本人方案生成服务暂时不可用，请稍后再试。'
  }
  return '这套方案暂时没有生成成功，请稍后重试。'
}
