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
  data: {
    // 状态栏高度与胶囊占位按真机测量，避免写死的 88rpx/118rpx 在某些机型上
    // 让标题钻进状态栏、右侧按钮被胶囊遮挡
    padTop: 44,
    rightPad: 96
  },
  lifetimes: {
    attached() {
      try {
        const win = wx.getWindowInfo()
        const capsule = wx.getMenuButtonBoundingClientRect()
        const update = {}
        if (win && win.statusBarHeight) update.padTop = win.statusBarHeight
        if (capsule && capsule.left) update.rightPad = Math.max(96, win.windowWidth - capsule.left + 8)
        this.setData(update)
      } catch (error) {}
    }
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
