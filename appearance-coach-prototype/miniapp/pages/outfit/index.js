const api = require('../../services/api')

const sceneCodes = { '日常': 'daily', '面试': 'interview', '约会': 'date' }

Page({
  data: {
    photo: '', mediaID: '', context: '日常', contexts: ['日常', '面试', '约会'],
	loading: false, analyzed: false, result: null, error: '', diagnosisLabel: '', demo: false
  },
  choosePhoto() {
    wx.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: ({ tempFiles }) => this.setData({
        photo: tempFiles[0].tempFilePath, mediaID: '', analyzed: false, result: null, error: '', demo: false
      })
    })
  },
  useDemo() {
    if (this.data.loading) return
    this.setData({ loading: true, error: '' })
    api.createDemoMedia('outfit').then((asset) => this.setData({
      photo: '/assets/looks/natural.jpg', mediaID: asset.id, analyzed: false, result: null, loading: false, demo: true
    })).catch((error) => this.setData({ error: error.message, loading: false }))
  },
  selectContext(event) {
    this.setData({ context: event.currentTarget.dataset.value, analyzed: false, result: null, error: '' })
  },
  analyze() {
    if (!this.data.photo || this.data.loading) return
    this.setData({ loading: true, error: '' })
    const mediaReady = this.data.mediaID
      ? Promise.resolve({ id: this.data.mediaID })
      : api.uploadMedia('outfit', this.data.photo)
    mediaReady.then((asset) => {
      this.setData({ mediaID: asset.id })
      return api.runTool({
        kind: 'outfit',
        media_id: asset.id,
        report_id: wx.getStorageSync('jianwo_report_id') || '',
        scene: sceneCodes[this.data.context] || 'daily'
      })
	}).then((result) => {
		const findings = (result.findings || []).slice(0, 3).map((item, index) => {
			const x = Math.max(8, Math.min(72, Math.round((item.anchor_x || [.2, .68, .28][index]) * 100)))
			const y = Math.max(12, Math.min(78, Math.round((item.anchor_y || [.3, .46, .68][index]) * 100)))
			return { ...item, position: `left:${x}%;top:${y}%`, side: index % 2 ? 'right' : 'left' }
		})
		const diagnosisLabel = (result.provider_version || '').indexOf('demo') === 0 ? '效果示例' : '实拍诊断'
		this.setData({ result: { ...result, findings }, diagnosisLabel, analyzed: true, loading: false })
	})
      .catch((error) => this.setData({ error: error.message, loading: false }))
  },
  generateAlternative() {
    const continueFlow = () => {
      const reportID = wx.getStorageSync('jianwo_report_id')
      const scene = sceneCodes[this.data.context] || 'daily'
      if (reportID) {
        wx.setStorageSync('jianwo_plans_scene', scene)
        wx.switchTab({ url: '/pages/plans/index' })
      } else {
        wx.navigateTo({ url: `/pages/capture/index?scene=${scene}` })
      }
    }
    if (!this.data.result || !this.data.result.id) {
      continueFlow()
      return
    }
    this.setData({ loading: true })
    api.saveToolResult(this.data.result.id).then(() => {
      this.setData({ loading: false, 'result.saved': true })
      continueFlow()
    }).catch((error) => this.setData({ error: error.message, loading: false }))
  },
  reset() {
	this.setData({ photo: '', mediaID: '', analyzed: false, result: null, error: '', diagnosisLabel: '' })
  }
})
