const api = require('../../services/api')

Page({
  data: {
    conversationID: '', todayPlanID: '', input: '', sending: false, messages: [],
    suggestions: ['明天面试怎么穿？', '只用现有衣橱', '换得更轻松一点', '今天下雨怎么调整？']
  },
  onLoad(options) {
	api.trackEvent('page_view', { page: 'advisor' }).catch(() => {})
    const conversationID = wx.getStorageSync('jianwo_advisor_conversation_id') || ''
    this.setData({ conversationID, todayPlanID: options.todayPlanId || '' })
    if (conversationID) api.getAdvisorMessages(conversationID).then((messages) => this.setData({ messages })).catch(() => wx.removeStorageSync('jianwo_advisor_conversation_id'))
  },
  inputChange(event) { this.setData({ input: event.detail.value }) },
  chooseSuggestion(event) { this.setData({ input: event.currentTarget.dataset.value }); this.send() },
  send() {
    const content = this.data.input.trim()
    if (!content || this.data.sending) return
    const local = { id: `local-${Date.now()}`, role: 'user', content }
    this.setData({ input: '', sending: true, messages: [...this.data.messages, local] })
    api.sendAdvisorMessage({ conversation_id: this.data.conversationID, content, report_id: wx.getStorageSync('jianwo_report_id'), today_plan_id: this.data.todayPlanID })
      .then((message) => {
        const conversationID = message.conversation_id
        wx.setStorageSync('jianwo_advisor_conversation_id', conversationID)
        this.setData({ conversationID, messages: [...this.data.messages, message] })
		api.trackEvent('advisor_message_send', { conversation_id: conversationID, has_action: Boolean((message.actions || []).length) }).catch(() => {})
      })
      .catch((error) => { wx.showToast({ title: error.message, icon: 'none' }); this.setData({ messages: this.data.messages.filter((item) => item.id !== local.id), input: content }) })
      .finally(() => this.setData({ sending: false }))
  },
  applyAction(event) {
    const id = event.currentTarget.dataset.id
    api.applyAdvisorAction(id).then((action) => {
      const messages = this.data.messages.map((message) => ({ ...message, actions: (message.actions || []).map((item) => item.id === id ? action : item) }))
      this.setData({ messages }); api.trackEvent('advisor_action_apply', { action_id: action.id, kind: action.kind }).catch(() => {}); wx.showToast({ title: '已应用到今日调整', icon: 'success' })
    }).catch((error) => wx.showToast({ title: error.message, icon: 'none' }))
  }
})
