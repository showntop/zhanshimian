const TOKEN_KEY = 'jianwo_token'

function appData() {
  const app = getApp()
  return app.globalData
}

function request(path, options = {}) {
  const data = appData()
  return new Promise((resolve, reject) => {
    wx.request({
      url: `${data.apiBaseURL}${path}`,
      method: options.method || 'GET',
      data: options.data,
      timeout: options.timeout || 15000,
      header: {
        'content-type': 'application/json',
        ...(data.token ? { Authorization: `Bearer ${data.token}` } : {}),
        ...(options.header || {})
      },
      success(response) {
        if (response.statusCode >= 200 && response.statusCode < 300) {
          resolve(response.data && response.data.data)
          return
        }
        if (response.statusCode === 401 && !options.retried) {
          data.token = ''
          data.user = null
          wx.removeStorageSync(TOKEN_KEY)
          ensureSession().then(() => request(path, { ...options, retried: true })).then(resolve).catch(reject)
          return
        }
        const error = response.data && response.data.error
        const requestError = new Error(error ? error.message : '服务暂时不可用')
        requestError.statusCode = response.statusCode
        reject(requestError)
      },
      fail(error) { reject(new Error(error.errMsg || '网络连接失败')) }
    })
  })
}

function ensureSession() {
  const data = appData()
  if (data.token) return Promise.resolve(data.user)
  return new Promise((resolve) => {
    wx.login({ success: resolve, fail: () => resolve({ code: 'dev' }) })
  }).then(({ code }) => request('/v1/auth/wechat', { method: 'POST', data: { code: code || 'dev', nickname: '见我用户' } }))
    .catch(() => request('/v1/auth/dev', { method: 'POST', data: { nickname: '见我用户' } }))
    .then((session) => {
      data.token = session.token
      data.user = session.user
      wx.setStorageSync(TOKEN_KEY, session.token)
      return session.user
    })
}

function uploadMedia(kind, filePath) {
  const data = appData()
  return ensureSession().then(() => new Promise((resolve, reject) => {
    wx.uploadFile({
      url: `${data.apiBaseURL}/v1/media`,
      filePath,
      name: 'file',
      formData: { kind },
      header: { Authorization: `Bearer ${data.token}` },
      timeout: 30000,
      success(response) {
        try {
          const body = JSON.parse(response.data)
          if (response.statusCode >= 200 && response.statusCode < 300) resolve(body.data)
          else reject(new Error(body.error ? body.error.message : '上传失败'))
        } catch (error) { reject(error) }
      },
      fail(error) { reject(new Error(error.errMsg || '上传失败')) }
    })
  }))
}

module.exports = {
  ensureSession,
  uploadMedia,
  createDemoMedia: (kind) => ensureSession().then(() => request('/v1/media/demo', { method: 'POST', data: { kind } })),
  createAnalysis: (data) => request('/v1/analyses', { method: 'POST', data }),
  getAnalysis: (id) => request(`/v1/analyses/${id}`),
  getReport: (id) => request(`/v1/reports/${id}`),
  getPlans: (id) => request(`/v1/reports/${id}/plans`),
  getPlan: (id) => request(`/v1/plans/${id}`),
  selectPlan: (id) => request(`/v1/plans/${id}/select`, { method: 'POST' }),
  getChecklist: (id) => request(`/v1/plans/${id}/checklist`),
  setChecklistItem: (id, completed) => request(`/v1/checklist/${id}`, { method: 'PATCH', data: { completed } }),
  addFeedback: (data) => request('/v1/feedback', { method: 'POST', data }),
	runTool: (data) => ensureSession().then(() => request('/v1/tools/run', { method: 'POST', data, timeout: data.kind === 'outfit' ? 65000 : 15000 })).catch((error) => {
    if (error.statusCode === 404 && data.report_id) {
      wx.removeStorageSync('jianwo_report_id')
      const currentApp = getApp()
      if (currentApp && currentApp.globalData) currentApp.globalData.reportID = ''
      return request('/v1/tools/run', { method: 'POST', data: { ...data, report_id: '' } })
    }
    throw error
  }),
  saveToolResult: (id) => ensureSession().then(() => request(`/v1/tools/${id}/save`, { method: 'POST' })),
	createHairPreview: (data) => ensureSession().then(() => request('/v1/hair-previews', { method: 'POST', data })),
	getHairPreview: (id) => ensureSession().then(() => request(`/v1/hair-previews/${id}`)),
	getSavedHairPreviews: () => ensureSession().then(() => request('/v1/hair-previews')),
	saveHairPreview: (id) => ensureSession().then(() => request(`/v1/hair-previews/${id}/save`, { method: 'POST' })),
  deleteData: () => request('/v1/me/data', { method: 'DELETE' })
}
