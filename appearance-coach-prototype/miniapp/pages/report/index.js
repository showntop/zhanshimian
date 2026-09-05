const api = require('../../services/api')
const { userImage } = require('../../utils/media')

const CATEGORY_LABEL = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }
const SEVERITY_LABEL = { low: '轻微', medium: '适中', high: '明显' }
const CTA_GENERATE = '生成我的 3 套造型方案'
const CTA_VIEW = '查看我的 3 套造型方案'
const CTA_QUEUEING = '正在加入生成队列…'

// 后端文案偶发夹带替换符/方块类字符，渲染前过滤
function cleanCopy(value) {
  return typeof value === 'string' ? value.replace(/[\uFFFD\uFFFC\u25A0-\u25FF\u2580-\u259F]/g, '') : value
}

Page({
  data: {
    id: '',
    scene: '',
    status: 'loading',
    report: null,
    annotations: [],
    image: '',
    findingItems: [],
    suggestions: [],
    suggestionsError: false,
    looksReady: false,
    ctaText: CTA_GENERATE,
    generating: false
  },
  onLoad(options) {
    const id = options.id || getApp().globalData.reportID || wx.getStorageSync('jianwo_report_id')
    this.setData({ id: id || '', scene: options.scene || '' })
    if (!id) { this.setData({ status: 'empty' }); return }
    this.load()
  },
  load() {
    if (!this.data.id) { this.setData({ status: 'empty' }); return }
    this.setData({
      status: 'loading',
      report: null,
      annotations: [],
      image: '',
      findingItems: [],
      suggestions: [],
      suggestionsError: false,
      looksReady: false,
      ctaText: CTA_GENERATE,
      generating: false
    })
    api.getReport(this.data.id).then((report) => {
      wx.setStorageSync('jianwo_report_id', this.data.id)
      const findings = report.findings || []
      // 报告主图是全身照:只给 body 观察渲染图上标注(photo 为空的历史数据一直
      // 只画在主图上,保持原行为);face/side 结论仅进入下方「细致解读」。
      // 编号沿用 finding 在完整列表中的序号,与「细致解读」对齐。
      const annotations = []
      findings.forEach((item, index) => {
        if (item.photo && item.photo !== 'body') return
        if (annotations.length >= 3) return
        const x = Math.max(6, Math.min(94, (item.anchor_x == null ? 0.5 : item.anchor_x) * 100))
        const y = Math.max(5, Math.min(95, (item.anchor_y == null ? 0.5 : item.anchor_y) * 100))
        // 编号圆点钉在锚点上，说明气泡朝画面内侧展开，垂直方向优先放在锚点下方
        const chipBelow = y < 70
        let chipStyle
        if (x <= 24) chipStyle = 'left: 12rpx;'
        else if (x >= 76) chipStyle = 'right: 12rpx;'
        else chipStyle = `left: ${x}%; transform: translateX(-50%);`
        chipStyle += chipBelow ? ` top: calc(${y}% + 30rpx);` : ` bottom: calc(${100 - y}% + 30rpx);`
        annotations.push({
          ...item,
          number: index + 1,
          dotStyle: `left: ${x}%; top: ${y}%;`,
          chipStyle,
          lineStyle: chipBelow ? `left: ${x}%; top: calc(${y}% + 20rpx);` : `left: ${x}%; bottom: calc(${100 - y}% + 20rpx);`
        })
      })
      this.setData({
        status: 'ready',
        report: {
          ...report,
          findings,
          priority_title: cleanCopy(report.priority_title),
          priority_copy: cleanCopy(report.priority_copy)
        },
        annotations,
        findingItems: findings.map((item, index) => ({
          ...item,
          index: index + 1,
          categoryLabel: CATEGORY_LABEL[item.category] || '形象',
          severityLabel: SEVERITY_LABEL[item.severity] || ''
        })),
        image: userImage(report.current_image_url)
      })
      this.loadSuggestions()
    }).catch((error) => {
      wx.showToast({ title: error.message, icon: 'none' })
      // 进入 error 前清空全部内容数据，旧报告绝不与错误态同屏
      this.setData({
        status: 'error',
        report: null,
        annotations: [],
        image: '',
        findingItems: [],
        suggestions: [],
        suggestionsError: false,
        looksReady: false,
        generating: false
      })
    })
  },
  // 内部自捕获并返回 Promise，供 viewPlans 在建议加载失败后串联重拉
  loadSuggestions() {
    this.setData({ suggestionsError: false })
    return api.getPlans(this.data.id, this.data.scene).then((plans) => {
      const list = plans || []
      const looksReady = list.length > 0 && list.every((item) => item.generation_status === 'completed')
      this.setData({ looksReady, ctaText: looksReady ? CTA_VIEW : CTA_GENERATE })
      const plan = list.find((item) => item.recommended) || list[0]
      if (!plan) return null
      return api.getPlan(plan.id)
    }).then((detail) => {
      if (!detail) return
      const suggestions = (detail.steps || []).map((step) => ({
        category: step.category,
        type: CATEGORY_LABEL[step.category] || '建议',
        title: step.title,
        summary: step.summary
      }))
      this.setData({ suggestions })
    }).catch(() => {
      this.setData({ suggestionsError: true, looksReady: false, ctaText: CTA_GENERATE })
    })
  },
  retrySuggestions() {
    this.loadSuggestions()
  },
  viewPlans() {
    if (this.data.generating) return
    const go = () => {
      if (this.data.scene) wx.setStorageSync('jianwo_plans_scene', this.data.scene)
      wx.switchTab({ url: '/pages/plans/index' })
    }
    const active = wx.getStorageSync('jianwo_active_plan_generation')
    if (active && (active.reportId !== this.data.id || (active.scene || '') !== (this.data.scene || ''))) {
      wx.showToast({ title: '已有一组方案正在生成', icon: 'none' })
      if (active.scene) wx.setStorageSync('jianwo_plans_scene', active.scene)
      wx.switchTab({ url: '/pages/plans/index' })
      return
    }
    const decide = () => {
      if (this.data.suggestionsError) {
        wx.showToast({ title: '方案状态获取失败，请稍后重试', icon: 'none' })
        return
      }
      if (this.data.looksReady) { go(); return }
      this.setData({ generating: true, ctaText: CTA_QUEUEING })
      api.generatePlanLooks(this.data.id, this.data.scene)
        .then(() => {
          // 入队成功后再记录进行中的生成，避免失败后 key 残留误导首页
          wx.setStorageSync('jianwo_active_plan_generation', { reportId: this.data.id, scene: this.data.scene })
          this.setData({ generating: false, ctaText: CTA_GENERATE })
          go()
        })
        .catch((error) => {
          wx.showToast({ title: error.message || '生成暂时不可用', icon: 'none' })
          this.setData({ generating: false, ctaText: CTA_GENERATE })
        })
    }
    // 方案状态曾加载失败时，先重新拉取再决定跳 plans 还是触发生成，不盲目重复生成
    if (this.data.suggestionsError) { this.loadSuggestions().then(decide); return }
    decide()
  },
  reanalyze() { wx.navigateTo({ url: '/pages/capture/index?scene=general&replace=1' }) },
  startAnalysis() { wx.navigateTo({ url: '/pages/capture/index?scene=general' }) },
  onShareAppMessage() { return { title: '我的形象分析报告｜怎么打扮', path: '/pages/home/index' } }
})
