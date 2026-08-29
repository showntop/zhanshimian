const scenes = {
  interview: { id: 'interview', label: '面试', icon: '/assets/icons/briefcase.svg' },
  wedding: { id: 'wedding', label: '婚礼', icon: '/assets/icons/sparkles.svg' },
  date: { id: 'date', label: '约会', icon: '/assets/icons/heart.svg' },
  daily: { id: 'daily', label: '日常', icon: '/assets/icons/sun.svg' }
}

Page({
  data: {
    scene: scenes.interview,
    hasProfile: false,
    time: 'week',
    budget: 'mid',
    formality: 'proper',
    impression: 'reliable',
    timeOptions: [
      { value: 'today', label: '今天' }, { value: 'three-days', label: '3 天内' },
      { value: 'week', label: '1 周后' }, { value: 'later', label: '还没确定' }
    ],
    budgetOptions: [
      { value: 'low', label: '500 以内' }, { value: 'mid', label: '500–1500' }, { value: 'high', label: '1500 以上' }
    ],
    formalOptions: [
      { value: 'relaxed', label: '轻松' }, { value: 'proper', label: '得体' }, { value: 'formal', label: '正式' }
    ],
    impressionOptions: [
      { value: 'energetic', label: '更有精神' }, { value: 'reliable', label: '更可信' },
      { value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }
    ]
  },
  onLoad(options) {
    this.setData({ scene: scenes[options.scene] || scenes.interview, hasProfile: Boolean(wx.getStorageSync('jianwo_report_id')) })
  },
  selectOne(event) {
    this.setData({ [event.currentTarget.dataset.field]: event.currentTarget.dataset.value })
  },
  continueFlow() {
    const brief = { scene: this.data.scene.id, time: this.data.time, budget: this.data.budget, formality: this.data.formality, impression: this.data.impression }
    wx.setStorageSync('jianwo_scene_brief', brief)
    const reportID = wx.getStorageSync('jianwo_report_id')
    if (reportID) wx.navigateTo({ url: `/pages/plans/index?reportId=${reportID}&scene=${this.data.scene.id}` })
    else wx.navigateTo({ url: `/pages/capture/index?scene=${this.data.scene.id}` })
  }
})
