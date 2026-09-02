const api = require('../../services/api')
const { userImage } = require('../../utils/media')

const CATEGORY_LABEL = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }
const SEVERITY_LABEL = { low: '轻微', medium: '适中', high: '明显' }
const CATEGORY_ICON = { hair: '/assets/icons/sparkles.svg', makeup: '/assets/icons/user.svg', outfit: '/assets/icons/briefcase.svg' }

Page({
  data: { id: '', scene: '', loading: true, report: null, annotations: [], image: '', findingItems: [], suggestions: [], looksReady: false, ctaText: '生成 3 套本人方案', generating: false },
  onLoad(options) {
    const id = options.id || getApp().globalData.reportID || wx.getStorageSync('jianwo_report_id')
    this.setData({ id, scene: options.scene || '' })
    this.load()
  },
  load() {
    if (!this.data.id) { this.setData({ loading: false }); wx.showToast({ title: '请先完成形象分析', icon: 'none' }); return }
    api.getReport(this.data.id).then((report) => {
      wx.setStorageSync('jianwo_report_id', this.data.id)
      const findings = report.findings || []
      const annotations = findings.slice(0, 2).map((item, index) => {
        const x = (item.anchor_x == null ? 0.5 : item.anchor_x) * 100
        const y = (item.anchor_y == null ? 0.5 : item.anchor_y) * 100
        const placeRight = x < 50
        return {
          ...item,
          index,
          styleLeft: placeRight ? `${Math.min(92, x + 3)}%` : `${Math.max(8, x - 3)}%`,
          styleTop: `${Math.max(8, Math.min(92, y))}%`,
          alignClass: placeRight ? 'finding-right' : 'finding-left'
        }
      })
      this.setData({
        report: { ...report, findings },
        annotations,
        findingItems: findings.map((item, index) => ({
          ...item,
          index: index + 1,
          categoryLabel: CATEGORY_LABEL[item.category] || '形象',
          severityLabel: SEVERITY_LABEL[item.severity] || ''
        })),
        image: userImage(report.current_image_url),
        loading: false
      })
      this.loadSuggestions()
    }).catch((error) => {
      wx.showToast({ title: error.message, icon: 'none' }); this.setData({ loading: false })
    })
  },
  loadSuggestions() {
    api.getPlans(this.data.id, this.data.scene).then((plans) => {
      const list = plans || []
      const looksReady = list.length > 0 && list.every((item) => item.generation_status === 'completed')
      this.setData({ looksReady, ctaText: looksReady ? '查看 3 套本人方案' : '生成 3 套本人方案' })
      const plan = list.find((item) => item.recommended) || list[0]
      if (!plan) return
      return api.getPlan(plan.id)
    }).then((detail) => {
      if (!detail) return
      const suggestions = (detail.steps || []).map((step) => ({
        category: step.category,
        icon: CATEGORY_ICON[step.category] || CATEGORY_ICON.hair,
        type: CATEGORY_LABEL[step.category] || '建议',
        title: step.title,
        summary: step.summary
      }))
      this.setData({ suggestions })
    }).catch(() => {})
  },
  viewPlans() {
    if (this.data.generating) return
    const go = () => {
      const scene = this.data.scene ? `&scene=${this.data.scene}` : ''
      wx.navigateTo({ url: `/pages/plans/index?reportId=${this.data.id}${scene}` })
    }
    const active = wx.getStorageSync('jianwo_active_plan_generation')
    if (active && (active.reportId !== this.data.id || (active.scene || '') !== (this.data.scene || ''))) {
      wx.showToast({ title: '已有一组方案正在生成', icon: 'none' })
      wx.navigateTo({ url: `/pages/plans/index?reportId=${active.reportId}${active.scene ? `&scene=${active.scene}` : ''}` })
      return
    }
    if (this.data.looksReady || this.data.generating) { go(); return }
    this.setData({ generating: true, ctaText: '正在加入生成队列…' })
    wx.setStorageSync('jianwo_active_plan_generation', { reportId: this.data.id, scene: this.data.scene })
    api.generatePlanLooks(this.data.id, this.data.scene)
      .catch((error) => wx.showToast({ title: error.message || '生成暂时不可用', icon: 'none' }))
      .finally(() => { this.setData({ generating: false, ctaText: '生成 3 套本人方案' }); go() })
  },
  reanalyze() { wx.navigateTo({ url: '/pages/capture/index?scene=general&replace=1' }) },
  onShareAppMessage() { return { title: '我的形象分析报告｜怎么打扮', path: '/pages/home/index' } }
})
