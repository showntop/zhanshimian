// Native mini-programs do not receive shell environment variables at runtime.
// Keep release endpoints explicit and override them through extConfig in CI when
// the project is managed by a third-party platform.
module.exports = {
  apiBaseURLs: {
    develop: 'http://127.0.0.1:58000',
    trial: 'https://prompt.wuyill.com/zhanshimian',
    release: 'https://prompt.wuyill.com/zhanshimian'
  }
}
