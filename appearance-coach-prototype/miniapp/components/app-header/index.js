Component({
  properties: {
    title: { type: String, value: '' },
    back: { type: Boolean, value: false },
    share: { type: Boolean, value: false },
    bookmark: { type: Boolean, value: false },
    help: { type: Boolean, value: false },
    profile: { type: Boolean, value: false },
    transparent: { type: Boolean, value: false }
  },
  methods: {
    goBack() {
      if (getCurrentPages().length > 1) wx.navigateBack()
      else wx.switchTab({ url: '/pages/home/index' })
    },
    openProfile() { wx.switchTab({ url: '/pages/profile/index' }) },
    emitHelp() { this.triggerEvent('help') },
    emitBookmark() { this.triggerEvent('bookmark') }
  }
})
