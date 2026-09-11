// 简体中文案单源（AGENTS.md 红线 5）。
// 红线：不出现「通勤」（场景叫「日常」）、不出现「颜值」「评分」；
// 错误不甩锅，空态/错误态必须给出下一步动作。

export const APP_NAME = 'UP一下'

/** 品牌标语（与 APP_NAME 配合使用，如分享卡/开屏副标题） */
export const APP_SLOGAN = '今天最好看'

/** 首页主标题（保留，不许改写） */
export const HOME_TITLE = '你好，我是你的私人形象顾问'

// ---------- 场景（固定四席） ----------
export interface SceneCopy {
  id: 'interview' | 'wedding' | 'date' | 'daily'
  label: string
  note: string
}

export const SCENES: readonly SceneCopy[] = [
  { id: 'interview', label: '面试', note: '精神可信' },
  { id: 'wedding', label: '婚礼', note: '得体上镜' },
  { id: 'date', label: '约会', note: '自然有记忆点' },
  { id: 'daily', label: '日常', note: '省心耐看' }
] as const

// ---------- 问候语（分时段，纯函数便于测试） ----------
export function greetingByHour(hour: number): string {
  if (hour < 6) return '夜深了'
  if (hour < 9) return '早上好'
  if (hour < 12) return '上午好'
  if (hour < 14) return '中午好'
  if (hour < 18) return '下午好'
  return '晚上好'
}

export function greetingForNow(getHour: () => number = () => new Date().getHours()): string {
  return greetingByHour(getHour())
}

// ---------- 方案反馈词（多选 chips） ----------
export const FEEDBACK_WORDS = ['很像我', '更有精神', '容易做到', '不够自然'] as const

// ---------- 隐私说明 ----------
export const PRIVACY_NOTE = '照片仅用于生成分析，可随时删除'
export const PRIVACY_SECTION_TITLE = '隐私与数据'

// ---------- 错误文案骨架 ----------
export const ERROR_COPY = {
  network: '网络连接不上，请检查网络后重试',
  server: '服务暂时不可用，请稍后重试',
  unauthorized: '登录已过期，正在为你重新登录',
  notFound: '内容不存在或已被删除',
  photoRejected: '照片不满足分析要求，请按引导重新拍摄',
  uploadFailed: '上传失败，点击重试这张',
  deleteConfirmTitle: '确认删除？',
  deleteConfirmBody: '删除后无法恢复，与它相关的报告和方案也会一起移除。',
  retryAction: '重试',
  backAction: '返回'
} as const

// ---------- 空态文案骨架（必须带下一步动作） ----------
export const EMPTY_COPY = {
  report: { title: '还没有形象报告', body: '拍三张照片，几分钟拿到你的第一份形象分析。', action: '开始分析' },
  plans: { title: '这个场合还没有方案', body: '选定一份形象报告后，即可生成对应场合的穿搭方案。', action: '去选报告' },
  checklist: { title: '清单已就绪', body: '按步骤准备，完成一项勾一项。', action: '查看方案' },
  today: { title: '今天还没有方案', body: '看看今天适合怎么穿，一分钟生成。', action: '生成今日方案' },
  wardrobe: { title: '衣橱还是空的', body: '拍两张单品照，让方案用上你已有的衣服。', action: '添加单品' },
  advisor: { title: '和顾问聊聊', body: '任何穿着上的疑问，直接问。', action: '开始提问' },
  hair: { title: '还没有发型预览', body: '选一个推荐发型，看看上身效果。', action: '去挑发型' },
  history: { title: '暂无记录', body: '完成后会出现在这里。', action: '' }
} as const

// ---------- 图片身份标注（红线 2：AI 生成图像必须显式标识） ----------
export const IMAGE_BADGE_COPY = {
  bundled: '风格参考',
  demo: '效果示例',
  aiPreview: 'AI 风格预览',
  current: '当前'
} as const

// ---------- 分析页阶段文案 ----------
export const ANALYSIS_STAGE_COPY = {
  queued: '已提交，排队中',
  uploading: '整理照片',
  analyzing: '分析身形与色彩',
  composing: '撰写报告',
  generating: '生成方案',
  fallback: '正在分析，请稍候'
} as const

export function analysisStageText(stage: string | undefined): string {
  if (stage && stage in ANALYSIS_STAGE_COPY) {
    const key = stage as keyof typeof ANALYSIS_STAGE_COPY
    return ANALYSIS_STAGE_COPY[key]
  }
  return ANALYSIS_STAGE_COPY.fallback
}

// 分析页细粒度阶段时间线：at 为进度百分比，页面按补间进度取「at <= 进度」的最后一条。
// 覆盖服务端各上报点（15/22/32/42/48/56/64/72/82/95），中间档让文案持续细粒度推进。
export const ANALYSIS_STAGE_TIMELINE: ReadonlyArray<{ at: number; text: string }> = [
  { at: 0, text: '正在安全上传照片' },
  { at: 8, text: '正在核对照片清晰度' },
  { at: 15, text: '正在排队等待分析' },
  { at: 22, text: '正在读取三张照片' },
  { at: 30, text: '正在确认照片是否符合要求' },
  { at: 38, text: '正在提取面部轮廓' },
  { at: 46, text: '正在分析正脸比例' },
  { at: 54, text: '正在分析侧脸线条' },
  { at: 62, text: '正在分析全身比例' },
  { at: 70, text: '正在整理你的形象特点' },
  { at: 78, text: '正在匹配场景与预算' },
  { at: 86, text: '正在组合发型、妆容与穿搭' },
  { at: 94, text: '正在保存形象档案' },
  { at: 100, text: '三套方案已经准备好' }
] as const

/** 按显示进度取时间线文案；进度越界时取首/末条。 */
export function analysisTimelineText(progress: number): string {
  let text = ANALYSIS_STAGE_TIMELINE[0]?.text ?? ''
  for (const item of ANALYSIS_STAGE_TIMELINE) {
    if (progress >= item.at) text = item.text
    else break
  }
  return text
}

// ---------- 分析失败态（照片被拒 vs 超时未完成，标题与安抚文案分开） ----------
export const ANALYSIS_FAIL_COPY = {
  photoTitle: '照片没有通过检查',
  timeoutTitle: '分析时间有点长',
  timeoutBody: '这次分析没有完成，重新发起通常就能解决。',
  photoFallback: '请按拍摄指引重新提交'
} as const

// ---------- 首页工作台 ----------
export const HOME_COPY = {
  returningTitle: '继续今天的形象计划',
  reportReady: '形象档案已就绪',
  viewReport: '查看报告与建议 ›',
  startArchive: '开始形象档案',
  startAnalysis: '开始形象分析',
  archiveTitle: '三张照片，建立只属于你的形象档案',
  archiveBody: '正脸、45° 侧脸、正面全身。不用化妆，也不需要刻意摆姿势。',
  photoPrivacy: '照片与建议只对你可见，可随时删除',
  processTitle: '建档后会得到什么',
  process: [
    { title: '当前形象标签', desc: '先看清现在的整体印象' },
    { title: '4 个可提升点', desc: '每条都标回来源照片' },
    { title: '3 套可执行方案', desc: '按场景、预算和现实条件生成' },
  ],
  toolsTitle: '直接解决眼前的一件事',
  scenesTitle: '按场合开始',
  sceneReadyNote: '已复用你的形象档案，不会再要照片',
  recentTitle: '最近方案',
  emptyTodayLink: '先看今天怎么穿 ›',
  lifeTitle: '顾问与衣橱',
  advisorEntry: '和顾问聊聊',
  advisorEntryDesc: '任何穿着上的疑问，直接问',
  wardrobeEntry: '我的衣橱',
  wardrobeEntryDesc: '让方案用上你已有的衣服'
} as const

// ---------- 形象档案补充资料 ----------
export const PROFILE_SETUP_COPY = {
  eyebrow: '最后一步，少填一点',
  title: '告诉我你的现实条件',
  lede: '这些问题都不会强制填写；跳过时我们会只根据照片与场景判断。',
  photosReady: '三张照片已上传',
  height: '身高',
  heightNote: '拖动滑杆，或用按钮微调',
  role: '日常身份',
  budget: '置装预算',
  skip: '暂不填写',
  primaryAction: '生成形象报告',
  skipAction: '跳过并生成',
  privacy: '资料可随时删除；不会用于评分或身材判断',
  whyTitle: '为什么要问这些？',
  whyBody: '身份和预算只用来约束建议的可执行性，避免推荐不适合日常场景或超出预算的选择。'
} as const

// ---------- 形象报告 ----------
export const REPORT_COPY = {
  title: '形象报告',
  currentMark: '当前形象',
  demoMark: '效果示例',
  aiMark: 'AI 分析',
  sourceTitle: '来源照片',
  tagsTitle: '当前印象',
  priorityTitle: '最优先建议',
  findingsTitle: '可提升点',
  emptyFindings: '这次没有必须调整的项目，可以按方案逐步尝试。',
  viewPlans: '查看我的 3 套方案',
  viewPlansNote: '方案基于你的照片与现实条件生成',
  noReportTitle: '还没有形象报告',
  noReportBody: '拍三张照片，几分钟拿到你的第一份形象分析。',
  goArchive: '去建档'
} as const
