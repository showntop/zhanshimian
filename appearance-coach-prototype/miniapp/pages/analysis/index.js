const api = require('../../services/api')
const { userImage } = require('../../utils/media')

const PHOTO_STEPS = [
  { kind: 'face', index: 1, label: '正脸', shortFocus: '轮廓', focus: '面部轮廓与五官比例' },
  { kind: 'side', index: 2, label: '侧脸', shortFocus: '线条', focus: '侧面线条与头颈关系' },
  { kind: 'body', index: 3, label: '全身', shortFocus: '比例', focus: '肩颈、身形与整体比例' }
]

const SCAN_POINTS = {
  face: [
    { id: 'forehead', x: 50, y: 19, delay: 0 },
    { id: 'brow-left', x: 36, y: 29, delay: 120 },
    { id: 'brow-right', x: 64, y: 29, delay: 240 },
    { id: 'nose', x: 50, y: 43, delay: 360 },
    { id: 'cheek-left', x: 30, y: 50, delay: 480 },
    { id: 'cheek-right', x: 70, y: 50, delay: 600 },
    { id: 'jaw', x: 50, y: 66, delay: 720 }
  ],
  side: [
    { id: 'forehead', x: 48, y: 20, delay: 0 },
    { id: 'brow', x: 57, y: 30, delay: 140 },
    { id: 'nose', x: 65, y: 41, delay: 280 },
    { id: 'lip', x: 62, y: 51, delay: 420 },
    { id: 'jaw', x: 54, y: 63, delay: 560 },
    { id: 'neck', x: 45, y: 76, delay: 700 }
  ],
  body: [
    { id: 'head', x: 50, y: 14, delay: 0 },
    { id: 'shoulder-left', x: 35, y: 28, delay: 120 },
    { id: 'shoulder-right', x: 65, y: 28, delay: 240 },
    { id: 'waist-left', x: 40, y: 48, delay: 360 },
    { id: 'waist-right', x: 60, y: 48, delay: 480 },
    { id: 'knee-left', x: 43, y: 72, delay: 600 },
    { id: 'knee-right', x: 57, y: 72, delay: 720 }
  ]
}

function analysisView(progress, kind) {
  const activeIndex = Math.max(0, PHOTO_STEPS.findIndex((item) => item.kind === kind))
  const step = PHOTO_STEPS[activeIndex] || PHOTO_STEPS[0]
  let focus = step.focus
  if (kind === 'face' && progress < 32) focus = '照片清晰度与拍摄角度'
  if (kind === 'face' && progress >= 48) focus = '眉眼关系、脸型与面部重心'
  if (kind === 'side' && progress >= 64) focus = '发型轮廓与侧脸重心'
  if (kind === 'body' && progress >= 82) focus = '整合三张照片的形象特点'
  return {
    currentPhotoIndex: step.index,
    analysisFocus: focus,
    analysisSummary: step.focus,
    scanPoints: SCAN_POINTS[kind] || SCAN_POINTS.face,
    photoSteps: PHOTO_STEPS.map((item, index) => ({
      ...item,
      state: index < activeIndex ? 'done' : (index === activeIndex ? 'active' : 'pending')
    }))
  }
}

// Photo-gate rejections arrive as stage="照片未通过检查" with a message shaped
// like "正脸照片：…；侧脸照片：…。请按拍摄指引重新提交"
// (see PhotoRejectedError.UserMessage on the server). Splitting is
// presentation-only and falls back to the raw message when it does not parse.
function failureView(stage, message) {
  const generic = { failTitle: '这次没有分析完成', failureItems: [], failTip: '', failAction: '返回重试', failMessage: message || '照片不会丢失，你可以返回后重新提交。' }
  if (stage !== '照片未通过检查' || !message) return generic
  const items = []
  message.replace(/。?请按拍摄指引重新提交$/, '').split('；').forEach((part) => {
    const sep = part.indexOf('：')
    if (sep > 0 && part.slice(0, sep).indexOf('照片') >= 0) items.push({ label: part.slice(0, sep), reason: part.slice(sep + 1) })
  })
  if (!items.length) return { ...generic, failTitle: '照片需要重新拍摄', failAction: '重新拍摄', failMessage: message }
  return { failTitle: '照片需要重新拍摄', failureItems: items, failTip: '请按拍摄指引重新提交', failAction: '重新拍摄', failMessage: '' }
}

Page({
  data: {
    id: '', scene: 'general', progress: 8, displayProgress: 8, stage: '正在安全上传照片', failed: false,
    errorMessage: '', failTitle: '', failureItems: [], failTip: '', failAction: '返回重试', failMessage: '',
    media: [], previewImage: '', previewKind: 'face', previewLabel: '正脸照', previewMode: 'aspectFill',
    currentPhotoIndex: 1, analysisFocus: '照片清晰度与拍摄角度', analysisSummary: '面部轮廓与五官比例',
    scanPoints: SCAN_POINTS.face, photoSteps: analysisView(8, 'face').photoSteps
  },
  onLoad(options) {
    const app = getApp()
    const initialMedia = app.globalData.analysisMedia || []
    const initialProgress = this.data.progress
    this.setData({ id: options.id || app.globalData.analysisID, scene: options.scene || 'general', displayProgress: initialProgress })
    this.updateMedia(initialMedia, initialProgress)
    this.poll()
  },
  onUnload() {
    if (this.timer) clearTimeout(this.timer)
    if (this._progressTimer) {
      clearInterval(this._progressTimer)
      this._progressTimer = null
    }
  },
  poll() {
    api.getAnalysis(this.data.id).then((analysis) => {
      this.failures = 0
      const failed = analysis.status === 'failed'
      const errorMessage = analysis.error_message || ''
      const targetProgress = analysis.progress
      this.setData({ progress: targetProgress, stage: analysis.stage, failed, errorMessage, ...(failed ? failureView(analysis.stage, errorMessage) : {}) })
      this.animateProgressTo(targetProgress)
      const nextMedia = Array.isArray(analysis.media) && analysis.media.length ? analysis.media : this.data.media
      if (Array.isArray(analysis.media) && analysis.media.length) getApp().globalData.analysisMedia = analysis.media
      this.updateMedia(nextMedia, targetProgress)
      if (analysis.status === 'completed') {
        wx.removeStorageSync('jianwo_active_analysis_id')
        getApp().globalData.reportID = analysis.report_id
        wx.setStorageSync('jianwo_report_id', analysis.report_id)
        this.createPendingScenePlans(analysis.report_id)
        return
      }
      if (!failed) this.timer = setTimeout(() => this.poll(), 700)
    }).catch((error) => {
      // A 404 means the analysis is gone (data was cleared mid-run); anything
      // else may be transient. Either way, stop retrying silently forever —
      // surface the failure UI so the page never looks stuck.
      if (error && error.statusCode === 404) {
        this.setData({ failed: true, ...failureView('', '') })
        return
      }
      this.failures = (this.failures || 0) + 1
      if (this.failures >= 5) {
        this.setData({ failed: true, ...failureView('', '') })
        return
      }
      this.timer = setTimeout(() => this.poll(), 1200)
    })
  },
  animateProgressTo(target) {
    if (this._progressTimer) clearInterval(this._progressTimer)
    const start = this.data.displayProgress
    const distance = target - start
    if (distance === 0) return
    const duration = 500
    const startTime = Date.now()
    this._progressTimer = setInterval(() => {
      const elapsed = Date.now() - startTime
      if (elapsed >= duration) {
        clearInterval(this._progressTimer)
        this._progressTimer = null
        this.setData({ displayProgress: target })
        return
      }
      const next = Math.round(start + distance * (elapsed / duration))
      this.setData({ displayProgress: next })
    }, 30)
  },
  updateMedia(media, progress) {
    const normalized = (media || []).map((item) => ({ ...item, url: userImage(item.url) }))
    const desiredKind = progress < 55 ? 'face' : (progress < 68 ? 'side' : 'body')
    const preview = normalized.find((item) => item.kind === desiredKind && item.url) || normalized.find((item) => item.kind === 'face' && item.url) || normalized.find((item) => item.url)
    const previewKind = preview ? preview.kind : desiredKind
    this.setData({
      media: normalized,
      previewImage: preview ? preview.url : '',
      previewKind,
      previewLabel: ({ face: '正脸照', side: '侧脸照', body: '全身照' })[previewKind] || '照片',
      previewMode: previewKind === 'body' ? 'aspectFit' : 'aspectFill',
      ...analysisView(progress, previewKind)
    })
  },
  createPendingScenePlans(reportID) {
    const brief = wx.getStorageSync('jianwo_scene_brief')
    const shouldCreate = this.data.scene !== 'general' && brief && brief.scene === this.data.scene
    const destination = (scene) => setTimeout(() => wx.redirectTo({ url: `/pages/report/index?id=${reportID}${scene ? `&scene=${scene}` : ''}` }), 500)
    if (!shouldCreate) {
      destination('')
      return
    }
    this.setData({ stage: '正在为这次场合组合三套方案' })
    api.createScenePlans(reportID, brief)
      .then(() => destination(this.data.scene))
      .catch(() => {
        wx.showToast({ title: '场合方案稍后可重新生成', icon: 'none' })
        destination('')
      })
  },
  retry() { wx.removeStorageSync('jianwo_active_analysis_id'); wx.navigateBack({ delta: 1 }) }
})
