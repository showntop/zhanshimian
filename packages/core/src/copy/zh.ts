// 简体中文案单源（AGENTS.md 红线 5）。
// 红线：不出现「通勤」（场景叫「日常」）、不出现「颜值」「评分」；
// 错误不甩锅，空态/错误态必须给出下一步动作。

export const APP_NAME = 'uplook'

/** 默认昵称（空昵称 / 开发登录兜底，与 users.nickname 默认值一致） */
export const DEFAULT_NICKNAME = 'uplook用户'

/** 品牌标语（与 APP_NAME 配合使用，如分享卡/开屏副标题） */
export const APP_SLOGAN = '今天最好看'

/** 首页主标题（保留，不许改写） */
export const HOME_TITLE = '你好，我是你的私人形象顾问'

// ---------- 场景（固定四席） ----------
export interface SceneCopy {
  id: 'interview' | 'wedding' | 'date' | 'daily' | 'gathering'
  label: string
  note: string
}

export const SCENES: readonly SceneCopy[] = [
  { id: 'interview', label: '面试', note: '精神可信' },
  { id: 'wedding', label: '婚礼', note: '得体上镜' },
  { id: 'date', label: '约会', note: '自然有记忆点' },
  { id: 'daily', label: '日常', note: '省心耐看' },
  { id: 'gathering', label: '聚会', note: '轻松有型' }
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

// ---------- 反馈确认文案（服务端 acknowledgement_code 的唯一映射） ----------
// 只有确实写入偏好记忆时才承诺「下次」；纯记录一律不含任何承诺。
export const FEEDBACK_ACK_COPY = {
  feedback_recorded: '反馈已记录。',
  less_formal_saved: '已记住：下次方案会降低正式度。',
  simpler_saved: '已记住：下次方案会减少复杂步骤。',
  avoid_color_saved: '已记住：下次方案会避开你指定的颜色。',
  preserve_saved: '已记住：下次方案会保留你指定的做法。'
} as const

export type FeedbackAcknowledgementCode = keyof typeof FEEDBACK_ACK_COPY

export function feedbackAcknowledgement(code: FeedbackAcknowledgementCode): string {
  return FEEDBACK_ACK_COPY[code]
}

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
  plansNeedArchive: { title: '还没有方案', body: '先完成三图建档，或在场景页生成场合方案。', action: '去建档' },
  plansAnalyzing: { title: '正在分析你的照片', body: '分析完成后就可以生成方案，现在不用重新建档。', action: '查看分析进度' },
  checklist: { title: '清单已就绪', body: '按步骤准备，完成一项勾一项。', action: '查看方案' },
  today: { title: '今天还没有方案', body: '看看今天适合怎么穿，一分钟生成。', action: '生成今日方案' },
  wardrobe: { title: '衣橱还是空的', body: '拍两张单品照，让方案用上你已有的衣服。', action: '添加单品' },
  advisor: { title: '形象助手', body: '任何穿着上的疑问，直接问。', action: '开始提问' },
  hair: { title: '还没有发型预览', body: '选一个推荐发型，看看上身效果。', action: '去挑发型' },
  history: { title: '暂无记录', body: '完成后会出现在这里。', action: '' }
} as const

// ---------- 图片身份标注（红线 2：AI 生成图像必须显式标识） ----------
export const IMAGE_BADGE_COPY = {
  original: '原本',
  bundled: '风格参考',
  demo: '效果示例',
  aiPreview: '风格参考',
  current: '当前'
} as const

// ---------- 图片空态文案 ----------
// 投影不出可渲染图片时的兜底文字。绝不在这里放任何"示例图"字样：
// 空态就是空态，不拿内置模特图冒充用户内容。
export const SOURCE_IMAGE_COPY = {
  mediaEmpty: '图片暂不可用',
  userPhotoEmpty: '照片暂不可用'
} as const

// ---------- 三图建档 ----------
// 建档页只说拍摄本身：补充资料归 pages/profile，这里再放一遍身份/预算表单
// 会造出第二个编辑入口，用户改哪边生效将无从判断。
export const CAPTURE_COPY = {
  headerTitle: '创建形象档案',
  eyebrow: '三张自然光照片',
  headline: '先有真实照片，再有可靠建议',
  lede: '不用化妆，也不需要刻意摆姿势；每一张都可以重拍。',
  shots: {
    face: { label: '正脸', desc: '自然表情，看清五官与肤色' },
    side: { label: '45° 侧脸', desc: '头发不挡轮廓，判断发型空间' },
    body: { label: '正面全身', desc: '全身入镜，看清头肩与比例' }
  },
  slotEmptyHint: '轻触拍摄',
  slotReadyHint: '已上传',
  slotDemoHint: '示例已选',
  phaseHashing: '读取中',
  phaseUploading: '上传中',
  slotFailedLabel: '上传失败 · 轻触重试',
  slotFailed: '这张没有上传成功，可以重试或换一张。',
  pickerFailed: '没有打开相机或相册，请再试一次。',
  batchPartialFailure: '部分照片未上传成功，请在对应照片上重试',
  actionRetry: '重试',
  actionReplace: '换一张',
  actionViewLarge: '查看大图',
  actionShootAgain: '重新拍摄',
  actionFromAlbum: '从相册换一张',
  actionContinuousShoot: '连续拍摄',
  actionBatchPick: '从相册批量选择',
  demoAction: '先用「效果示例」体验完整流程 ›',
  demoUnavailable: '示例照片暂时不可用，请重试',
  submitAction: '开始形象分析',
  submitFailed: '提交没有成功，请重试',
  privacy: '照片与建议只对你可见，可随时删除'
} as const

/** 主按钮上的「已选 N / 3 张」。数字与量词都在文案里，页面不拼字符串。 */
export function captureSelectedText(done: number, total: number): string {
  return `已选 ${done} / ${total} 张`
}

/** 空槽快捷补齐入口上的「补齐剩余 N 张 ›」。 */
export function captureFillMissingText(missing: number): string {
  return `补齐剩余 ${missing} 张 ›`
}

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

// ---------- 分析失败态（照片被拒 vs 超时未完成，标题与安抚文案分开） ----------
export const ANALYSIS_FAIL_COPY = {
  photoTitle: '照片没有通过检查',
  timeoutTitle: '分析时间有点长',
  timeoutBody: '这次分析没有完成，重新发起通常就能解决。',
  photoFallback: '请按拍摄指引重新提交'
} as const

// ---------- 分析进度页（事件驱动，只有服务端说的三件事） ----------
export const ASSESSMENT_COPY = {
  headerTitle: '形象分析',
  // 三步指示器：与服务端阶段码一一对应，见 ASSESSMENT_STAGE_STEPS
  steps: ['检查照片', '分析形象', '整理报告'],
  photosTitle: '这次分析用的照片',
  photosMissing: '这张照片暂不可用',
  openingReport: '报告已经准备好，正在打开…',
  retryingNote: '服务端正在重试这一步',
  privacy: '照片全程加密，只有你能看到',
  wander: '先去逛逛，不用守在这里 ›',
  requestIdLabel: '请求编号',
  retryAction: '重新发起',
  reshootAction: '重新拍摄',
  homeAction: '返回首页',
  endedTitle: '这次分析已结束',
  endedCancelled: '你已经取消了这次分析，可以重新提交照片。',
  endedSuperseded: '这次分析已经被新的分析替代，去看最新的一次吧。',
  noResultTitle: '分析完成了，但没有拿到报告',
  noResultBody: '报告可能已经被替换。返回首页可以看到最新的形象报告。',
  networkTitle: '网络连接不上',
  networkBody: '连续几次都没有连上服务，请检查网络后重试。',
  stageFallback: '正在分析，请稍候'
} as const

// 服务端阶段码 → 中文。键必须与 apps/server 的 assessment policy 完全一致：
// 这里列不出某个码时宁可退回兜底文案，也不猜它大概在哪一步。
export const ASSESSMENT_STAGE_COPY: Record<string, string> = {
  'photo.technical_check': '正在核对照片清晰度',
  'photo.content_check': '正在确认照片是否符合要求',
  'photo.identity_check': '正在确认三张照片是同一个人',
  'report.generating': '正在分析形象并撰写报告',
  'report.evidence_check': '正在核对每条建议的来源照片',
  'report.publishing': '正在保存形象报告'
}

/** 阶段码 → 进度页文案；服务端没给或给了不认识的码时用兜底文案。 */
export function assessmentStageText(stageCode: string | undefined): string {
  if (stageCode && stageCode in ASSESSMENT_STAGE_COPY) {
    return ASSESSMENT_STAGE_COPY[stageCode] as string
  }
  return ASSESSMENT_COPY.stageFallback
}

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
  whyBody: '身份和预算只用来约束建议的可执行性，避免推荐不适合日常场景或超出预算的选择。',
  editTitle: '修改资料',
  editBody: '身份和预算只用来约束建议，不会评分或做身材判断。',
  editAction: '修改',
  editName: '修改称呼',
  nickname: '称呼',
  changePhoto: '更换形象照片',
  save: '保存',
  saved: '已保存',
  weight: '体重',
  bust: '胸围',
  waist: '腰围',
  hip: '臀围',
  optional: '选填',
  roles: ['产品经理', '设计师', '咨询顾问', '学生'],
  budgets: ['500 以内', '500–1500', '1500 以上']
} as const

// ---------- 形象报告 ----------
export const REPORT_COPY = {
  title: '形象报告',
  currentMark: '当前形象',
  demoMark: '效果示例',
  aiMark: 'AI 分析',
  sourceTitle: '来源照片',
  tagsTitle: '综合印象',
  priorityTitle: '综合建议',
  findingsTitle: '可提升点',
  emptyFindings: '这次没有必须调整的项目，可以按方案逐步尝试。',
  viewPlans: '查看我的 3 套方案',
  viewPlansNote: '方案基于你的照片与现实条件生成',
  noReportTitle: '还没有形象报告',
  noReportBody: '拍三张照片，几分钟拿到你的第一份形象分析。',
  noReportAction: '重新拍摄',
  goArchive: '去建档',
  // 证据缺失时的空位说明：报告宁可留缺口，也不拿别的照片顶上
  evidenceEmpty: '这张来源照片暂不可用',
  evidenceEmptyHint: '锚点是在这张照片上量出来的，换一张就不作数',
  findingAnchorNote: '标在来源照片上的位置',
  loadFailed: '报告没有加载成功，请重试',
  planFailed: '方案没有生成成功，请重试',
  // finding.category 的中文标签。键集合与契约的六类一致；
  // 出现表外值时调用方显示原值（uuid 级别的兜底），不抛错。
  categoryLabels: {
    hair: '发型',
    makeup: '妆容',
    outfit: '穿搭',
    proportion: '比例',
    color: '色彩',
    overall: '整体'
  },
  findingsObservationTitle: '看得到的现状',
  findingsAdviceTitle: '建议这样做'
} as const

/** finding.category → 中文标签；表外值显示原值，不抛错。 */
export function reportCategoryLabel(category: string): string {
  return REPORT_COPY.categoryLabels[category as keyof typeof REPORT_COPY.categoryLabels] ?? category
}

// ---------- 工具页：穿搭诊断 / 购买判断 ----------
export const OUTFIT_COPY = {
  title: '今天这身，先改哪一处',
  desc: '一张全身照，只指出最值得调整的一处',
  uploadTitle: '拍一张全身照',
  uploadTips: ['站远一步，从头到脚都入镜', '自然光下正面站立', '穿今天真实的搭配'],
  sceneLabel: '诊断场景',
  start: '开始诊断',
  busy: '正在诊断…',
  busyHint: '正在看你这身搭配',
  choosePhoto: '选择照片',
  demo: '用示例照片体验',
  reselect: '重选照片',
  again: '再诊断一次',
  lastResult: '查看结果',
  findingsKeep: '已经合适',
  findingsLift: '还可以改',
  save: '保存这条建议',
  saved: '已保存',
  toPlans: '去看三套方案'
} as const

export const PURCHASE_COPY = {
  title: '这件，适不适合你',
  desc: '买之前，先看它和你的匹配度',
  uploadTitle: '上传想买的单品图',
  uploadTips: ['白底或干净背景更清晰', '正面平铺或上身图', '看得清材质与版型'],
  start: '这件适合我吗',
  busy: '正在判断…',
  busyHint: '正在看这件和你合不合适',
  choosePhoto: '选择商品图',
  demo: '用示例图体验',
  reselect: '重选',
  again: '再判断一次',
  lastResult: '查看结果',
  findingsKeep: '已经合适',
  findingsLift: '还要注意',
  save: '保存这条判断',
  saved: '已保存'
} as const

export const ADVISOR_COPY = {
  title: '形象助手',
  emptyTitle: '有什么形象问题，直接问',
  emptyDesc: '我会结合你的档案、今日方案和衣橱给建议。',
  placeholder: '问点什么…',
  send: '发送'
} as const

export const CHECKLIST_COPY = {
  title: '执行清单',
  hint: '完成一项勾一项',
  doneOf: '已完成',
  remainSuffix: '项未完成',
  allDoneHint: '可以去反馈了',
  celebrate: '清单全部完成',
  cta: '完成后回来反馈',
  ctaDone: '去反馈',
  footNote: '你的反馈会让下一次建议更准确',
  emptyTitle: '清单还是空的',
  emptyBody: '先在方案详情页选择一套方案。',
  emptyAction: '去看方案',
  loadFailed: '清单没有加载成功，请重试',
  completeAction: '完成执行',
  completedNote: '这份执行已完成，可以聊聊哪里省时间、哪里别扭',
  syncConflict: '清单刚在其他地方更新过，已同步最新进度，请重试',
  syncFailed: '同步没有成功，已还原，再点一次即可',
  completeFailed: '完成没有成功，请重试',
  selectFailed: '选择没有成功，请重试'
} as const

export const PLAN_DETAIL_COPY = {
  title: '方案详情',
  cta: '生成清单',
  salon: '发给发型师',
  emptyStep: '这一步暂无内容',
  specLength: '长度',
  specFringe: '刘海',
  specTexture: '卷度',
  boardHint: '上滑看发型、妆容与穿搭细节',
  stepActionKeep: '保持',
  stepActionAdjust: '调整',
  detailTarget: '部位',
  detailIntensity: '幅度',
  detailSilhouette: '版型',
  detailPalette: '色板',
  detailLayers: '层次',
  detailAvoid: '避开',
  detailFormality: '正式度',
  loadFailed: '方案没有加载成功，请重试',
  renderNotePrefix: '形象图'
} as const

export const PLANS_COPY = {
  cta: '选这套 · 查看执行清单',
  ctaGenerating: '正在生成形象图'
} as const

// ---------- 方案集（Task 8：分阶段就绪） ----------
export const PLANNING_COPY = {
  generalTab: '形象方案',
  recommended: '推荐',
  viewDetail: '查看方案',
  boundNote: '基于你当前的报告与照片',
  // 方案集五态的页面文案
  planningTitle: '正在规划你的三套方案',
  planningBody: '从你的报告出发，通常需要一两分钟。可以先去逛逛。',
  wander: '先去逛逛，不用守在这里 ›',
  // 单套渲染的六态（与契约 RenderStatusView.state 一一对应）
  renderQueued: '排队等待生成',
  renderGenerating: '正在生成形象图',
  renderChecking: '正在检查图像质量',
  renderReady: '形象图已生成',
  renderFailed: '这一套的形象图没有生成',
  renderUnavailable: '当前没有可用的同能力生成服务',
  renderRetry: '重试这一套',
  renderFailedNote: '文字方案不受影响，可以先照着准备',
  renderUnavailableNote: '文字方案不受影响；服务恢复后这里会自动可以重试',
  // 列表为空 / 加载失败
  generalEmptyTitle: '还没有形象方案',
  generalEmptyBody: '基于你的形象报告生成三套可执行的方案。',
  sceneEmptyBody: '回答几个选择（约 30 秒），复用档案不重复要照片。',
  generateGeneral: '生成形象方案',
  generateScenePrefix: '生成',
  generateSceneSuffix: '方案',
  loadFailed: '方案没有加载成功，请重试',
  generateFailed: '方案暂时没有生成，请稍后重试',
  retryFailedTitle: '这一组方案没有生成成功',
  retryFailedBody: '通常是服务繁忙。重新发起一般就能解决。',
  regenerateAction: '重新生成',
  outcomeTitle: '能得到什么',
  whyLabel: '为什么适合你',
  whyClose: '收起',
  currentLabel: '原本',
  planLabel: '方案',
  compareHint: '左右拖动，看原本和方案',
  planOfPrefix: '第',
  planOfSuffix: '套'
} as const

/** 方案名 + 序号：「第 2 套 · 暖意」。序号来自 slot，不靠列表位置。 */
export function planSlotLabel(name: string, slot: number): string {
  return `第 ${slot} 套 · ${name}`
}

// ---------- 场合 Brief（Task 8：答案只在页面与 POST body 里） ----------
// 每个场景的问题与选项。value 必须与契约对应 Brief 的枚举完全一致——
// sceneBriefRequest 会按这份表校验答案，表错了请求会被服务端 400 拒收。
export const SCENE_BRIEF_COPY = {
  title: '场合需求',
  reuseBadge: '复用档案',
  lede: '补充几个选择，约 30 秒。不会重复索要照片和身体数据。',
  generateAction: '生成方案',
  needArchiveTitle: '还没有形象报告',
  needArchiveBody: '先完成三图建档，才能生成场合方案。',
  needArchiveAction: '去建档',
  loadFailed: '页面没有加载成功，请重试',
  submitFailed: '方案没有生成成功，请重试',
  scenes: {
    interview: {
      label: '面试',
      fields: [
        {
          key: 'when',
          label: '什么时候需要',
          options: [
            { value: 'today', label: '今天' },
            { value: 'three_days', label: '3 天内' },
            { value: 'week', label: '1 周后' },
            { value: 'later', label: '还没确定' }
          ]
        },
        {
          key: 'format',
          label: '面试形式',
          options: [
            { value: 'onsite', label: '线下面试' },
            { value: 'video', label: '视频面试' },
            { value: 'final', label: '终面 / 见客户' }
          ]
        },
        {
          key: 'preparation',
          label: '准备方式',
          options: [
            { value: 'closet', label: '只用现有衣橱' },
            { value: 'key_piece', label: '补一件关键单品' },
            { value: 'complete', label: '可完整准备' }
          ]
        },
        {
          key: 'impression',
          label: '最想呈现',
          options: [
            { value: 'energetic', label: '更有精神' },
            { value: 'reliable', label: '更可信' },
            { value: 'natural', label: '更自然' },
            { value: 'memorable', label: '有记忆点' }
          ]
        }
      ]
    },
    wedding: {
      label: '婚礼',
      fields: [
        {
          key: 'role',
          label: '你的角色',
          options: [
            { value: 'guest', label: '普通宾客' },
            { value: 'bridal_party', label: '伴娘 / 伴郎' },
            { value: 'family', label: '重要亲友' },
            { value: 'speaker', label: '需要上台' }
          ]
        },
        {
          key: 'timing',
          label: '婚礼时段',
          options: [
            { value: 'lunch', label: '午间' },
            { value: 'afternoon', label: '下午' },
            { value: 'dinner', label: '晚宴' },
            { value: 'unknown', label: '还没确定' }
          ]
        },
        {
          key: 'dress_code',
          label: '婚礼风格',
          options: [
            { value: 'relaxed', label: '轻松婚礼' },
            { value: 'elegant', label: '得体优雅' },
            { value: 'formal', label: '正式礼服' }
          ]
        },
        {
          key: 'impression',
          label: '最想呈现',
          options: [
            { value: 'energetic', label: '更有精神' },
            { value: 'reliable', label: '更可信' },
            { value: 'natural', label: '更自然' },
            { value: 'memorable', label: '有记忆点' }
          ]
        }
      ]
    },
    date: {
      label: '约会',
      fields: [
        {
          key: 'activity',
          label: '约会活动',
          options: [
            { value: 'coffee', label: '咖啡 / 散步' },
            { value: 'dinner', label: '正餐' },
            { value: 'exhibition', label: '电影 / 展览' },
            { value: 'outdoor', label: '户外' }
          ]
        },
        {
          key: 'timing',
          label: '什么时候',
          options: [
            { value: 'afternoon', label: '下午' },
            { value: 'evening', label: '傍晚' },
            { value: 'night', label: '晚上' },
            { value: 'unknown', label: '还没确定' }
          ]
        },
        {
          key: 'preparation',
          label: '准备方式',
          options: [
            { value: 'closet', label: '只用现有衣橱' },
            { value: 'key_piece', label: '补一件关键单品' },
            { value: 'complete', label: '可完整准备' }
          ]
        },
        {
          key: 'impression',
          label: '最想呈现',
          options: [
            { value: 'natural', label: '更自然' },
            { value: 'memorable', label: '有记忆点' },
            { value: 'energetic', label: '更有精神' }
          ]
        }
      ]
    },
    daily: {
      label: '日常',
      fields: [
        {
          key: 'activity',
          label: '今天主要做',
          options: [
            { value: 'office', label: '上班' },
            { value: 'weekend', label: '周末休息' },
            { value: 'friends', label: '朋友小聚' },
            { value: 'city_walk', label: '出门走走' }
          ]
        },
        {
          key: 'weather',
          label: '所处环境',
          options: [
            { value: 'air_conditioned', label: '室内空调为主' },
            { value: 'walking', label: '户外行走为主' },
            { value: 'rain', label: '下雨天' },
            { value: 'mild', label: '温和舒适' }
          ]
        },
        {
          key: 'preparation',
          label: '准备方式',
          options: [
            { value: 'closet', label: '只用现有衣橱' },
            { value: 'key_piece', label: '补一件关键单品' },
            { value: 'complete', label: '可完整准备' }
          ]
        },
        {
          key: 'impression',
          label: '最想呈现',
          options: [
            { value: 'natural', label: '更自然' },
            { value: 'energetic', label: '更有精神' },
            { value: 'reliable', label: '更可靠' }
          ]
        }
      ]
    },
    gathering: {
      label: '聚会',
      fields: [
        {
          key: 'activity',
          label: '聚会类型',
          options: [
            { value: 'friends', label: '朋友局' },
            { value: 'dinner', label: '聚餐' },
            { value: 'birthday', label: '生日 / 庆祝' },
            { value: 'drinks', label: '酒会 / 酒吧' }
          ]
        },
        {
          key: 'timing',
          label: '什么时候',
          options: [
            { value: 'afternoon', label: '下午' },
            { value: 'evening', label: '傍晚' },
            { value: 'night', label: '晚上' },
            { value: 'unknown', label: '还没确定' }
          ]
        },
        {
          key: 'preparation',
          label: '准备方式',
          options: [
            { value: 'closet', label: '只用现有衣橱' },
            { value: 'key_piece', label: '补一件关键单品' },
            { value: 'complete', label: '可完整准备' }
          ]
        },
        {
          key: 'impression',
          label: '最想呈现',
          options: [
            { value: 'natural', label: '更自然' },
            { value: 'memorable', label: '有记忆点' },
            { value: 'energetic', label: '更有精神' }
          ]
        }
      ]
    }
  }
} as const

export type SceneBriefScene = keyof typeof SCENE_BRIEF_COPY.scenes


export const BILLING_COPY = {
  section: '权益额度',
  remaining: '剩余额度',
  hint: '可用于形象分析、形象方案制作，各消耗 1 次',
  welcomeAnalysis: '另有 1 次免费形象分析',
  welcomePlanSet: '另有 1 次免费形象方案制作',
  buyAction: '购买次数',
  insufficientTitle: '额度不足',
  insufficientBody: '购买次数后可继续形象分析或形象方案制作。',
  rateLimited: '今日额度已用完，明天再来',
  paymentUnavailable: '购买暂未开通',
  buySuccess: '次数已到账',
  buyCancel: '已取消支付',
  buyPending: '支付已提交，次数稍后到账',
  buyNow: '去购买',
  paying: '支付中',
  exhausted: '次数已用完',
  buyOnMiniapp: '请到微信小程序购买次数',
  packUnit: '次'
} as const

/** 诊断发现语气标签（positive/improve/optional/caution → 尊重表达，无警示红） */
export const FINDING_TONE_COPY: Record<string, string> = {
  positive: '适合',
  improve: '可提升',
  optional: '可参考',
  caution: '注意'
}
