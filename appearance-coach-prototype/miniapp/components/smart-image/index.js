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
  observers: {
    src(newSrc) {
      if (!newSrc) {
        this.setData({ loaded: false, error: false })
        return
      }
      // Reset loaded state when source changes so the placeholder shows again.
      this.setData({ loaded: false, error: false })
    }
  },
  methods: {
    onLoad() { this.setData({ loaded: true, error: false }) },
    onError() { this.setData({ loaded: true, error: true }) }
  }
})
