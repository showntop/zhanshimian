Component({
  properties: {
    text: { type: String, value: '继续' },
    loading: { type: Boolean, value: false },
    disabled: { type: Boolean, value: false }
  },
  methods: {
    tap() {
      if (!this.data.loading && !this.data.disabled) this.triggerEvent('tap')
    }
  }
})
