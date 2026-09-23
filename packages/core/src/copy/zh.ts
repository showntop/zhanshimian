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

// ---------- 两条反馈闭环的固定标签（value 即契约枚举） ----------
export const GENERATION_FEEDBACK_TAGS = [
  { value: 'identity_mismatch', label: '不像本人' },
  { value: 'hair_mismatch', label: '发型不符' },
  { value: 'makeup_mismatch', label: '妆容不符' },
  { value: 'outfit_mismatch', label: '穿搭不符' },
  { value: 'anatomy_issue', label: '肢体异常' },
  { value: 'unnatural', label: '不够自然' }
] as const

export const EXECUTION_FEEDBACK_TAGS = [
  { value: 'easy_to_execute', label: '容易执行' },
  { value: 'too_formal', label: '太正式' },
  { value: 'too_complex', label: '太复杂' },
  { value: 'dislike_color', label: '颜色不喜欢' },
  { value: 'want_to_keep', label: '希望保留' }
] as const

// ---------- 反馈页 ----------
export const FEEDBACK_SCREEN_COPY = {
  generationEntry: '这张形象图像你吗',
  generationTitle: '这张形象图的反馈',
  generationNote: '反馈会用于改进生成质量',
  executionTitle: '这次执行感觉怎么样',
  tagsTitle: '选几个符合的（可多选）',
  commentTitle: '想说点什么（选填）',
  commentPlaceholder: '比如：刘海比想象中难打理',
  photoTitle: '拍一张实际效果（选填）',
  addPhoto: '添加实拍',
  retakePhoto: '重拍一张',
  photoUploading: '上传中',
  photoFailedNote: '照片没有上传成功，可以重试上传，或先提交文字和标签',
  submitWithoutPhoto: '先提交文字和标签',
  submitAction: '提交反馈',
  submitFailed: '反馈没有提交成功，请重试',
  needCompleted: '完成执行后才能反馈',
  loadFailed: '页面没有加载成功，请重试'
} as const


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
  report: { title: '还没有形象报告', body: '拍三张照片，几分钟拿到你的第一份形象分析。', action: '去形象分析' },
  plans: { title: '这个场合还没有方案', body: '选定一份形象报告后，即可生成对应场合的穿搭方案。', action: '去选报告' },
  plansNeedArchive: { title: '还没有方案', body: '生成方案前，需要先完成形象分析。', action: '去形象分析' },
  plansAnalyzing: { title: '正在分析你的照片', body: '分析完成后就可以生成方案，现在不用重新拍摄。', action: '查看分析进度' },
  checklist: { title: '清单已就绪', body: '按步骤准备，完成一项勾一项。', action: '查看方案' },
  today: { title: '今天还没有方案', body: '看看今天适合怎么穿，一分钟生成。', action: '生成今日方案' },
  wardrobe: { title: '衣橱还是空的', body: '拍两张单品照，让方案用上你已有的衣服。', action: '添加单品' },
  advisor: { title: '形象助手', body: '任何穿着上的疑问，直接问。', action: '开始提问' },
  hair: { title: '还没有发型设计', body: '选一个推荐发型，看看上身效果。', action: '去挑发型' },
  history: { title: '暂无记录', body: '完成后会出现在这里。', action: '' }
} as const

// ---------- 图片身份标注 ----------
// 「原本」保留在屏（用户本人照片的对比语义）；「风格参考」「效果示例」
// 按 2026-09-16 owner 决策不再上屏（见 AGENTS.md 红线 2），
// 文本仍由 source_kind 投影，供埋点与类型判别使用。
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

export const LAB_COPY = {
  title3d: '3D 形象 Lite',
  desc3d: '表达比例和穿搭轮廓，不承诺精确测量。',
  generate: '生成 3D 形象',
  regenerate: '再生成一圈',
  generating: '正在生成…',
  empty: '先拍正脸和正面全身，才能转起来看。',
  emptyAction: '去拍摄',
  failed: '这一圈没生成成功，再试一次或先返回。',
  retry: '再试一次',
  viewLast: '看上一圈',
  noCompare: '对比需要更完整的静帧，先转着看。',
  badgeAI: 'AI 风格预览',
  waitlist: '已加入候补',
  dragHint: '左右拖，看不同角度',
  angleFront: '正面',
  angleLeft: '左侧',
  angleBack: '背面',
  angleRight: '右侧',
  prevAngle: '上一角度',
  nextAngle: '下一角度'
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
  // 拱形相框上的状态标记与进度旁的静态预期：只说一次，不随进度跳变
  markAnalyzing: 'AI 分析中',
  markSettling: '即将完成',
  eta: '通常需要 1-2 分钟',
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
  // 分析进行中的主按钮：状态 + 动作，与纯链接 viewProgress 区分开
  analyzingAction: '正在分析，查看进度',
  archiveTitle: '三张照片，建立只属于你的形象档案',
  archiveBody: '正脸、45° 侧脸、正面全身。不用化妆，也不需要刻意摆姿势。',
  photoPrivacy: '照片与建议只对你可见，可随时删除',
  processTitle: '分析后会得到什么',
  process: [
    { title: '当前形象标签', desc: '看清现在的整体印象' },
    // 不写死数字：findings 真实数量是 3~6 条（schema minItems~maxFindings），
    // 方案按报告与偏好派生，数量词一概不出现在承诺里
    { title: '可提升点', desc: '每条都标回来源照片' },
    { title: '定制专属方案', desc: '按场景、预算生成' },
  ],
  // 新用户 hero 右侧三张堆叠图的角注（照片 → 理解 → 方案）
  previewCaptions: ['照片', '理解', '方案'],
  toolsTitle: '直接解决眼前的一件事',
  // 首页工具卡（1 张发型主卡 + 2 张安静卡）；key 与页面路由表对应，路由不进文案
  tools: [
    { key: 'hair', label: '发型设计', desc: '先看效果再决定', badge: '推荐' },
    { key: 'outfit', label: '穿搭诊断', desc: '只指出最值得改的一处', badge: '' },
    { key: 'purchase', label: '购买判断', desc: '买之前先看适不适合', badge: '' },
  ],
  // 工具卡实时徽章：在途任务覆盖静态 badge（live 样式）；「上次结果」沿用各工具自己的 lastResult 文案
  toolLiveHair: '生成中',
  scenesTitle: '按场合开始',
  sceneReadyNote: '已复用你的形象档案，不会再要照片',
  recentTitle: '最近方案',
  // 报告 hero 行动行里「N 个可提升点」的量词后缀，数字由页面填
  findingsSuffix: '个可提升点',
  continuePlan: '继续这套方案 ›',
  viewProgress: '查看进度 ›',
  planningLink: '你的三套方案正在规划 ›',
  dailyRemaining: '今日剩余',
  analysisLabel: '分析',
  looksLabel: '形象图',
  billingUnit: '次',
  todayEyebrow: '今日造型',
  emptyTodayLink: '先看今天怎么穿 ›',
  lifeTitle: '顾问与衣橱',
  advisorEntry: '和顾问聊聊',
  advisorEntryDesc: '任何穿着上的疑问，直接问',
  wardrobeEntry: '我的衣橱',
  wardrobeEntryDesc: '让方案用上你已有的衣服'
} as const

// ---------- 每日内容（今天这一条） ----------
// 红线沿用：不评价身体、不制造清单式压迫、不做连续天数与断签提醒。
// 池子不够时宁可不推，也不用「塞衣角」这类通用条目凑数。
export const DAILY_COPY = {
  eyebrow: '今天这一条',
  // 等待动画：巡游的文案优先用服务端随脚本下发的那份（改文案不用发版）；
  // 收敛这两句是过程状态，与具体分类无关，所以留在客户端。
  motionSettling: '在为你挑',
  motionLocked: '就是这套',
  // 换装洗牌（原型 daily-dressup-asset.html）：试衣间文案 + 揭晓面板
  dressCaptionIdle: '今天穿什么',
  dressCaptionSub: '正在为你搭配',
  dressCaptionSettled: '今天这一身',
  dressRevealTitle: '今天这一身',
  dressRevealSub: 'uplook · 为你搭好的一身',
  dressRevealGhost: 'TODAY · ONE LOOK',
  dressStamp: '今日',
  // 洗牌落地后：内容海报退到卡片下方的一行入口（内容主入口仍在今日页）
  dressContentLabel: '今日内容',
  // 引导进今日页：页里有「为什么这样搭」+ 收进手册，文案按页内实有的东西写
  dressContentHint: '为什么这样搭，都写在里面',
  dressContentLink: '看今日详情 ›',
  // 巡游内置主题：巡游是通用内容，不该等网络才有——prepare 没回来时（首屏
  // loading）也要立刻有东西可播，否则等待期会退成「一个圆圈」，像卡住了。
  // 服务端脚本到达后覆盖这份；form 名要与 daily-motion/forms.tsx 的注册表对上。
  motionRoamThemes: [
    { form: 'swatch_bars', label: '在看颜色' },
    { form: 'silhouette_shape', label: '在看版型' },
    { form: 'ratio_blocks', label: '在看比例' },
    { form: 'texture_lines', label: '在看面料' },
    { form: 'scene_panel', label: '在看场合' },
    { form: 'fold_lines', label: '在看穿法' },
    { form: 'outfit_blocks', label: '在看搭配' },
    { form: 'silhouette_shape', label: '在看发型' },
    { form: 'swatch_bars', label: '在看妆容' },
    { form: 'outfit_blocks', label: '在看配饰' },
  ],
  handbookTitle: '我的手册',
  handbookEntry: '我的手册',
  handbookEntryDesc: '收下的每一条，都在这里',
  seeItAction: '看看我穿这样 ›',
  // 按钮统一「收下」：分类名里的"场合 / 技巧"组合成「收进我的场合」会拗口。
  // 分类在手册里呈现，toast 补一句「已收进 · 颜色」。
  saveAction: '收下',
  savedToastPrefix: '已收进 · ',
  // 已收下后按钮置为已完成态（今日页）
  savedPrefix: '已收进 · ',
  // 兜底内容的角标：内容来源不同（服务端 source=fallback），不写「AI 生成」
  fallbackBadge: '今日精选',
  // 离线：本地有缓存就直接呈现；无缓存给静默文案 + 下一步动作
  offlineTitle: '今天的内容还没取到',
  offlineBody: '网络恢复后重新进入，或先看看收下的手册。',
  retryAction: '重新加载 ›',
  emptyTitle: '今天没有可推的内容',
  emptyBody: '内容池在当前条件下没有合适的条目。宁可不推，也不凑数。',
  handbookEmptyTitle: '手册还是空的',
  handbookEmptyBody: '收下今天这一条，手册就开始变厚了。',
  // 手册的分类：去掉"库"字、用两个字的常用词，不用"色卡 / 廓形"这类专业词。
  // 「色卡」和「配色库」用户分不清，合并为「颜色」。
  bucketNames: {
    color: '颜色',
    fit: '版型',
    proportion: '比例',
    fabric: '面料',
    occasion: '场合',
    howto: '技巧',
    outfit: '搭配',
    hair: '发型',
    makeup: '妆容',
    accessory: '配饰',
    general: '综合',
  },
  typeNames: {
    color: '颜色',
    silhouette: '版型',
    proportion: '比例',
    fabric: '面料',
    item: '单品',
    occasion: '场合',
    howto: '技巧',
    hair: '发型',
    makeup: '妆容',
    accessory: '配饰',
    general: '综合',
  },
} as const

export function dailyBucketName(bucket: string): string {
  return DAILY_COPY.bucketNames[bucket as keyof typeof DAILY_COPY.bucketNames] ?? '手册'
}

export function dailyTypeName(type: string): string {
  return DAILY_COPY.typeNames[type as keyof typeof DAILY_COPY.typeNames] ?? ''
}

// ---------- 首页任务完成轻提醒 ----------
// 轮询到终态时按 kind（render 再按 subject_type）给具体文案；execution_feedback 是后台写入，不打扰。
export const TASK_DONE_COPY = {
  assessment: '形象分析完成，去看看报告',
  planSet: '三套方案已生成，去看看',
  renderRun: '方案形象图已生成',
  hairPreview: '发型设计已生成',
  todayPlan: '今日搭配图已生成',
  bodyOrbit: '3D 形象已生成',
  fallback: '任务已完成'
} as const

/** 完成提醒文案；返回空串表示这类操作不提醒（execution_feedback 是后台一次性写入）。 */
export function taskDoneText(kind: string, subjectType?: string): string {
  if (kind === 'execution_feedback') return ''
  if (kind === 'render') {
    if (subjectType === 'hair_preview') return TASK_DONE_COPY.hairPreview
    if (subjectType === 'today_plan') return TASK_DONE_COPY.todayPlan
    return TASK_DONE_COPY.renderRun
  }
  if (kind === 'assessment') return TASK_DONE_COPY.assessment
  if (kind === 'plan_set') return TASK_DONE_COPY.planSet
  if (kind === 'body_orbit') return TASK_DONE_COPY.bodyOrbit
  return TASK_DONE_COPY.fallback
}

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
  deleteAction: '删除我的数据',
  deleteDone: '已删除',
  deleteFailed: '删除没有成功，请重试',
  weight: '体重',
  bust: '胸围',
  waist: '腰围',
  hip: '臀围',
  optional: '选填',
  roles: ['产品经理', '设计师', '咨询顾问', '学生'],
  budgets: ['500 以内', '500–1500', '1500 以上']
} as const

// ---------- 我的（Tab） ----------
// 账户 hero、任务中心、形象档案、基本资料、更多 五个分区的页面级文案；
// 编辑资料/改名弹层的文案沿用 PROFILE_SETUP_COPY，权益沿用 BILLING_COPY。
export const ME_COPY = {
  // hero 身份行：没有报告时不编造标签，只说这是什么
  archiveIdentity: '你的形象档案',
  viewReportLink: '查看最近报告 ›',
  startArchiveLink: '开始形象分析 ›',
  // 任务中心（简化版）：公开 OperationRef 只有 kind/status，没有进度百分比。
  // 键集合与契约 kind 枚举一致；execution_feedback 是后台一次性写入，页面不列出。
  tasksTitle: '进行中的任务',
  taskKindLabels: {
    assessment: '形象分析',
    plan_set: '形象方案',
    render: '形象图',
    execution_feedback: '反馈提交',
    body_orbit: '3D 形象'
  },
  taskWorking: '进行中',
  taskFailed: '未完成，点击查看',
  // 卡片只展示最近一条；其余收进「全部」弹层
  taskAllAction: '全部',
  taskAllTitle: '全部任务',
  archiveTitle: '形象档案',
  latestReport: '最近的分析报告',
  viewAction: '查看',
  noArchive: '未分析',
  myPlans: '我的方案',
  updateArchive: '更新形象档案',
  updateArchiveHint: '重拍三张',
  basicsTitle: '基本资料',
  measurements: '三围',
  unfilled: '未填写',
  moreTitle: '更多',
  labEntry: '体验实验室',
  labEntryHint: 'AR / 3D / 试衣',
  wardrobeEntry: '我的衣橱',
  wardrobeEntryHint: '轻量版',
  deleteAllHint: '全部删除',
  privacyNote: '照片与建议只对你可见',
  nicknameRequired: '请填写称呼',
  saveFailed: '保存没有成功，请重试',
  avatarFailed: '头像没有更新成功，请重试'
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
  generatePlans: '三套造型，为你量身定制',
  viewPlansNote: '方案基于你的照片与现实条件生成',
  noReportTitle: '还没有形象报告',
  noReportBody: '拍三张照片，几分钟拿到你的第一份形象分析。',
  noReportAction: '重新拍摄',
  goArchive: '去形象分析',
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
  toPlans: '去看搭配方案',
  // 改衣单排版（量体/裁缝隐喻）：标题区、须知、结果各节的编辑式标签
  mastheadMeta: '量体 · 改衣',
  adviceLabel: '改衣单',
  tipsLabel: '拍摄须知',
  // 选完照片后须知区不撤：换成「怎么读这张单」，段落常驻填实版心
  readingTipsLabel: '读单须知',
  readingTips: ['最值得调整的一处，钉在照片上', '只谈穿搭，不评长相', '换个场景，随时再诊'],
  keepNoteLabel: '边注'
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
  saved: '已保存',
  // 检验单排版（送检/票据隐喻）：题头、送检位、盖章与单据行
  mastheadMeta: '工具 · 检验',
  adviceLabel: '检验结论',
  tipsLabel: '送检须知',
  // 选完商品图后须知区不撤：换成「怎么读这张单」，票据行常驻填实版心
  readingTipsLabel: '读单须知',
  readingTips: ['结论盖章，落在照片右上', '要注意的点，列在单据上', '判断可保存，回头再对'],
  stampText: '已检验',
  serialLabel: '单号',
  dateLabel: '日期',
  uploadSlotLabel: '送检处'
} as const

// ---------- 工具页：发型设计 ----------
// 四步线性流程：确认照片 → 选方向 → 生成中 → 结果·对比（历史在结果页底部回放）
export const HAIR_COPY = {
  title: '先看效果，再决定剪不剪',
  desc: '选一个方向，基于你的正脸照生成上身效果',
  guideBadge: '拍照示范',
  uploadTitle: '选一张正脸照',
  useThisPhoto: '将用这张照片生成',
  reselect: '换一张',
  demo: '先看看示例效果',
  generating: '正在生成…',
  genStages: ['分析脸型轮廓…', '正在试戴发型…', '最后调整发丝…'],
  genNote: '需要 20 秒左右，可以离开，好了会帮你留着',
  holdOriginal: '长按看原图',
  save: '保存这个效果',
  saved: '已存到我的',
  // 换方向由结果页横滑承担；这条链接只负责换照片
  retakePhoto: '换张照片再试',
  // 结果页边界说明：只改发型，其余保持原样（与提示词的约束同一件事）
  resultNote: '只改了发型；五官、妆容、服装和背景保持原样',
  // 结果页下半屏的横滑：全部方向铺开，点了就换（试过的回放、没试过的直接生成）
  directionLabel: '换个方向看看',
  changePhoto: '换一张照片',
  retry: '再试一次',
  generateFailed: '生成没有完成，请重试',
  generateStart: '生成没有开始，请重试',
  // 性别分段：方向按性别分组，男女各有各的目录（unisex 两侧都出现）
  genderLabel: '按性别看方向',
  genderWomen: '女士',
  genderMen: '男士',
  // 自定义方向：用户自己写一句话当方向（≤40 字，服务端同样校验）
  customName: '自定义',
  customCardHint: '用一句话描述',
  customTitle: '说说你想要的样子',
  customLabel: '你想要的发型',
  customPlaceholder: '比如：两侧推短，顶部留一点长度',
  customHelper: '越具体越好；只改发型，脸和衣服不会变',
  customCta: '用这个描述生成',
  customEmpty: '先写一句你想要的样子',
  customTooLong: '描述最多 40 个字'
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
  eventRejected: '这一下没有记上，已还原，可以再点一次',
  completeFailed: '完成没有成功，请重试',
  selectFailed: '选择没有成功，请重试'
} as const

// ---------- 衣橱（只放新增文案；页面既有字符串的迁移归页面自己的任务） ----------
export const WARDROBE_COPY = {
  // 服务端可能下发 items=null 的组合：归一成空数组后的空态，必须带下一步动作
  outfitEmpty: '这次组合没有配上单品，点这里去添加单品 ›'
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
  wander: '先去逛逛，不用守在这里 ›',
  // 规划进度视图：三步指示与快照缺失时的安静态文案
  progressSteps: ['读你的报告', '定制三套造型', '写好每一步'],
  progressEta: '通常需要 1-2 分钟',
  progressRetrying: '服务端正在重试这一步',
  progressFallback: '正在为你定制三套造型',
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
  // 坞内缩略图的无图占位（重试在 hero 相框的状态区，小图里只如实标状态）
  renderThumbFailed: '未生成',
  renderThumbUnavailable: '暂不可用',
  // 列表为空 / 加载失败
  generalEmptyTitle: '还没有形象方案',
  generalEmptyBody: '顾问读完了你的报告，三套造型照着就能穿。',
  sceneEmptyBody: '回答几个小问题（约 30 秒），按你的档案定制三套。',
  generateGeneral: '三套造型，为你量身定制',
  generateScenePrefix: '穿什么？为你定制',
  generateSceneSuffix: '三套',
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
  // 悬浮坞 CTA 下方的来源说明（红线 2：生成来源必须显式说，整句放这里不拼串）
  ctaNote: '形象图由 AI 基于你的照片生成',
  ctaNoteDemo: '当前为效果示例，接入真实图像模型后展示本人效果',
  // 场景空态里的生成中行 / 切场景载入行
  sceneGenerating: '正在从你的报告生成三套方案，通常需要 1-2 分钟',
  sceneLoading: '正在载入该场合的方案…',
  planOfPrefix: '第',
  planOfSuffix: '套',
  // 已发布方案集的「重新设计」：场景回 Brief 页预填改答案；general 只能重拍（brief 固定，见 PlansScreen）
  redesignAction: '重新设计',
  updateGeneralLink: '照片或状态变了？重新拍摄后会生成新方案 ›',
  // 场景方案受理在途：tab 标记后缀 + 顶部全局提示（定位不到场景的在途操作）
  tabInFlightSuffix: '· 制作中',
  inFlightBanner: '有方案正在制作中，完成后会自动更新',
  // 卡堆决策台：满幅卡片堆叠，左滑跳过、右滑喜欢，按钮与手势等价
  deckSkip: '跳过',
  deckLike: '喜欢',
  deckUndo: '撤销',
  deckHint: '左滑跳过 · 右滑喜欢',
  deckRoundPrefix: '本轮',
  deckRoundSuffix: '套',
  resultLikedTitle: '这一轮你喜欢',
  resultLikedEmpty: '这轮没有留下喜欢的方案',
  resultSkippedLink: '再看看跳过的',
  resultRegenerate: '生成新的一轮',
  historyTitle: '往期方案',
  historyEntry: '往期',
  historyBadge: '往期',
  historyBackLatest: '回到最新',
  historyLikeCount: '个喜欢',
  deckDecisionFailed: '这条态度没有保存成功，请重试'
} as const

/** 服务端阶段码 → 中文。键必须与 apps/server 的 planning ports 完全一致：
 * 这里列不出某个码时宁可退回兜底文案，也不猜它大概在哪一步。 */
export const PLAN_STAGE_COPY: Record<string, string> = {
  'plan.reading_report': '正在阅读你的形象报告',
  'plan.checking': '正在检查三套造型'
}

/** 阶段码 → 规划进度文案；服务端没给或给了不认识的码时用兜底文案。 */
export function planStageText(stageCode: string | undefined): string {
  if (stageCode && stageCode in PLAN_STAGE_COPY) {
    return PLAN_STAGE_COPY[stageCode] as string
  }
  return PLANNING_COPY.progressFallback
}

/** 方案名 + 序号：「第 2 套 · 暖意」。序号来自 slot，不靠列表位置。 */
export function planSlotLabel(name: string, slot: number): string {
  return `第 ${slot} 套 · ${name}`
}

// ---------- 场合 Brief（Task 8：答案只在页面与 POST body 里） ----------
// 每个场景的问题与选项。value 必须与契约对应 Brief 的枚举完全一致——
// sceneBriefRequest 会按这份表校验答案，表错了请求会被服务端 400 拒收。
/** 场合 Brief 未答完时的提示：告诉用户还差几题（主按钮被点但答不完时用）。 */
export function sceneIncompleteText(count: number): string {
  return `还有 ${count} 题没选`
}

export const SCENE_BRIEF_COPY = {
  title: '场合需求',
  reuseBadge: '复用档案',
  lede: '补充几个选择，约 30 秒。不会重复索要照片和身体数据。',
  generateAction: '生成方案',
  needArchiveTitle: '还没有形象报告',
  needArchiveBody: '生成场合方案前，需要先完成形象分析。',
  needArchiveAction: '去形象分析',
  // 无档案但分析在途：引导看进度，不能引导发起第二次建档（旧线 scene 页行为）
  analyzingTitle: '正在分析你的照片',
  analyzingBody: '分析完成后就能生成场合方案，不用重新拍摄。',
  analyzingAction: '查看分析进度',
  loadFailed: '页面没有加载成功，请重试',
  submitFailed: '方案没有生成成功，请重试',
  // 答案与当前选项表对不上（预填了旧档案的值）：不能静默，让人重选
  answersStale: '有几个选项变了，请重新选择',
  // 点过「生成」后标在没选的题上
  missingTag: '未选',
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
  packUnit: '次',
  original: '原价',
  saved: '已优惠',
  zhe: '折',
  featured: '推荐',
  bestValue: '超值',
  perCredit: '/次'
} as const

/** 诊断发现语气标签（positive/improve/optional/caution → 尊重表达，无警示红） */
export const FINDING_TONE_COPY: Record<string, string> = {
  positive: '适合',
  improve: '可提升',
  optional: '可参考',
  caution: '注意'
}
