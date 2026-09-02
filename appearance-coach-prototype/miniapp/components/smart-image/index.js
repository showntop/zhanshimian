const loadedSrcs = new Set()

Component({
  options: { addGlobalClass: true },
  properties: {
    src: { type: String, value: '' },
    placeholder: { type: String, value: '' },
    mode: { type: String, value: 'aspectFill' },
    className: { type: String, value: '' },
    placeholderMode: { type: String, value: 'aspectFill' }
  },
  data: { loaded: false, error: false },
  lifetimes: {
    attached() {
      this.tryFastLoad(this.data.src)
    }
  },
  observers: {
    src(newSrc) {
      this.tryFastLoad(newSrc)
    }
  },
  methods: {
    tryFastLoad(src) {
      if (!src) {
        this.setData({ loaded: false, error: false })
        return
      }
      if (loadedSrcs.has(src)) {
        this.setData({ loaded: true, error: false })
      } else {
        this.setData({ loaded: false, error: false })
      }
    },
    onLoad() {
      loadedSrcs.add(this.data.src)
      this.setData({ loaded: true, error: false })
    },
    onError() {
      this.setData({ loaded: true, error: true })
    }
  }
})
