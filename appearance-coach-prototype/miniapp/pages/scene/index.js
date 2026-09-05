const scenes = {
  interview: { id: 'interview', label: '面试', icon: '/assets/icons/briefcase.png' },
  wedding: { id: 'wedding', label: '婚礼', icon: '/assets/icons/heart.png' },
  date: { id: 'date', label: '约会', icon: '/assets/icons/bookmark.png' },
  daily: { id: 'daily', label: '日常', icon: '/assets/icons/sun.png' }
}

const impressionOptions = [
  { value: 'energetic', label: '更有精神' }, { value: 'reliable', label: '更可信' },
  { value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }
]

const briefs = {
  interview: {
    copy: '用四个选择，确定这次面试的形象策略',
    fields: [
      { key: 'when', label: '什么时候面试？', grid: 'compact', default: 'week', options: [{ value: 'today', label: '今天' }, { value: 'three-days', label: '3 天内' }, { value: 'week', label: '1 周后' }, { value: 'later', label: '还没确定' }] },
      { key: 'format', label: '面试形式', grid: '', default: 'onsite', options: [{ value: 'onsite', label: '线下面试' }, { value: 'video', label: '视频面试' }, { value: 'final', label: '终面 / 见客户' }] },
      { key: 'preparation', label: '准备范围', grid: '', default: 'key-piece', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '补一件关键单品' }, { value: 'complete', label: '可完整准备' }] },
      { key: 'impression', label: '最想呈现的感觉', grid: 'two-col', default: 'reliable', options: impressionOptions }
    ]
  },
  wedding: {
    copy: '先确认你的身份与仪式氛围，避开用力过猛',
    fields: [
      { key: 'role', label: '你在婚礼中的身份', grid: 'two-col', default: 'guest', options: [{ value: 'guest', label: '普通宾客' }, { value: 'bridal-party', label: '伴娘 / 伴郎' }, { value: 'family', label: '重要亲友' }, { value: 'speaker', label: '需要上台' }] },
      { key: 'timing', label: '婚礼时间', grid: 'compact', default: 'dinner', options: [{ value: 'lunch', label: '午间' }, { value: 'afternoon', label: '下午' }, { value: 'dinner', label: '晚宴' }, { value: 'unknown', label: '还没确定' }] },
      { key: 'dress-code', label: '着装分寸', grid: '', default: 'elegant', options: [{ value: 'relaxed', label: '轻松婚礼' }, { value: 'elegant', label: '得体优雅' }, { value: 'formal', label: '正式礼服' }] },
      { key: 'impression', label: '最想呈现的感觉', grid: 'two-col', default: 'natural', options: [{ value: 'energetic', label: '明亮有精神' }, { value: 'reliable', label: '端庄可信' }, { value: 'natural', label: '温柔得体' }, { value: 'memorable', label: '有记忆点' }] }
    ]
  },
  date: {
    copy: '让搭配与活动、时段和你想传达的气氛一致',
    fields: [
      { key: 'activity', label: '这次约会做什么？', grid: 'two-col', default: 'dinner', options: [{ value: 'coffee', label: '咖啡 / 散步' }, { value: 'dinner', label: '晚餐' }, { value: 'exhibition', label: '展览 / 电影' }, { value: 'outdoor', label: '户外活动' }] },
      { key: 'timing', label: '约会时间', grid: 'compact', default: 'evening', options: [{ value: 'afternoon', label: '下午' }, { value: 'evening', label: '傍晚' }, { value: 'night', label: '晚上' }, { value: 'unknown', label: '还没确定' }] },
      { key: 'preparation', label: '准备范围', grid: '', default: 'key-piece', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '添一点新意' }, { value: 'complete', label: '可完整准备' }] },
      { key: 'impression', label: '最想呈现的感觉', grid: 'two-col', default: 'natural', options: [{ value: 'energetic', label: '轻快有精神' }, { value: 'reliable', label: '松弛有分寸' }, { value: 'natural', label: '自然亲近' }, { value: 'memorable', label: '有一点心动' }] }
    ]
  },
  daily: {
    copy: '结合今天的环境和安排，给你一套真的会穿的搭配',
    fields: [
      { key: 'activity', label: '今天主要去哪里？', grid: 'two-col', default: 'commute', options: [{ value: 'commute', label: '通勤上班' }, { value: 'weekend', label: '周末出门' }, { value: 'friends', label: '朋友见面' }, { value: 'walk', label: '城市漫游' }] },
      { key: 'weather', label: '今天的环境', grid: 'two-col', default: 'office', options: [{ value: 'office', label: '空调房为主' }, { value: 'walking', label: '室外走很多' }, { value: 'rain', label: '可能下雨' }, { value: 'mild', label: '温和舒适' }] },
      { key: 'preparation', label: '今天怎么准备？', grid: '', default: 'closet', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '可添一件单品' }, { value: 'complete', label: '不设限' }] },
      { key: 'impression', label: '今天最想解决什么？', grid: 'two-col', default: 'energetic', options: [{ value: 'energetic', label: '显精神' }, { value: 'reliable', label: '更利落' }, { value: 'natural', label: '舒服不费力' }, { value: 'memorable', label: '有一点亮点' }] }
    ]
  }
}

const api = require('../../services/api')

function buildAnswers(sceneID, saved) {
  return briefs[sceneID].fields.reduce((answers, field) => {
    answers[field.key] = saved && saved[field.key] ? saved[field.key] : field.default
    return answers
  }, {})
}

function buildQuestions(sceneID, answers) {
  return briefs[sceneID].fields.map((field, index) => ({
    ...field,
    position: `${index + 1} / ${briefs[sceneID].fields.length}`,
    last: index === briefs[sceneID].fields.length - 1,
    options: field.options.map((option) => ({ ...option, active: answers[field.key] === option.value }))
  }))
}

Page({
  data: { scene: scenes.interview, hasProfile: false, creating: false, briefCopy: '', answers: {}, questions: [] },
  onLoad(options) {
    const scene = scenes[options.scene] || scenes.interview
    const saved = wx.getStorageSync('jianwo_scene_brief')
    const answers = buildAnswers(scene.id, saved && saved.scene === scene.id ? saved.answers : null)
    this.setData({ scene, answers, questions: buildQuestions(scene.id, answers), briefCopy: briefs[scene.id].copy, hasProfile: Boolean(wx.getStorageSync('jianwo_report_id')) })
  },
  selectOne(event) {
    const field = event.currentTarget.dataset.field
    const answers = { ...this.data.answers, [field]: event.currentTarget.dataset.value }
    this.setData({ answers, questions: buildQuestions(this.data.scene.id, answers) })
  },
  continueFlow() {
    const brief = { scene: this.data.scene.id, answers: this.data.answers }
    wx.setStorageSync('jianwo_scene_brief', brief)
    const reportID = wx.getStorageSync('jianwo_report_id')
    if (!reportID) {
      wx.navigateTo({ url: `/pages/capture/index?scene=${this.data.scene.id}` })
      return
    }
    this.setData({ creating: true })
    api.createScenePlans(reportID, brief)
      .then(() => {
        wx.setStorageSync('jianwo_plans_scene', this.data.scene.id)
        wx.switchTab({ url: '/pages/plans/index' })
      })
      .catch((error) => wx.showToast({ title: error.message || '方案生成失败，请重试', icon: 'none' }))
      .finally(() => this.setData({ creating: false }))
  }
})
