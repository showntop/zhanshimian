// 发型设计：四步线性流程——确认照片(S0) → 选方向(S1) → 生成中(S2) → 结果·对比(S3)。
// hero 一屏一事：S0 将使用的正脸（示例模特图已从主流程退役）/ S2 源图+沉浸等待 /
// S3 效果图（长按看原图）。生成历史在结果页底部横滑回放，「换个方向」不再丢结果。
// 预览是异步受理（202 + 公开 Operation）；恢复先问服务端 /v1/hair-previews/active，
// 端点异常退回列表；没有进行中则回放最近一次成果（list 新到旧）。
//
// S1 方向：先按性别分段（女士/男士），再在横滑卡里选方向，末尾一张「自定义」卡
// 让用户自己写一句话。方向名随创建请求的 direction 上送——它就是生成提示词，
// 所以「选了哪个方向」和「生成出来的样子」是同一件事（目录未收录时尤其如此）。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, ScrollView, Text, Textarea, View } from '@tarojs/components'
import {
  CUSTOM_DIRECTION_ID,
  HAIR_COPY,
  HAIR_DIRECTIONS,
  hairDirectionViews,
  type DisplayMedia,
  type HairGender,
  type HairPreview,
  type HairStyle
} from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { uploadMedia } from '../../../../app/api/media-upload'
import { normalizeUploadableImage, readLocalImage } from '../../../../features/capture/local-file'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { useOperationPolling } from '../../../../app/operations/use-operation-polling'
import { handleBillingError } from '../../../../services/billing'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import { useDisplayablePath } from '../../../../hooks/use-displayable-path'
import BottomSheet from '../../../../components/bottom-sheet'
import Pill from '../../../../components/pill'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import './index.scss'

const IN_FLIGHT = new Set(['queued', 'generating', 'checking'])
// 空态的正脸拍照示范（包内资产，JPEG）
const FACE_GUIDE_IMAGE = '/assets/capture/face.jpg'
// 没有生成图的方向卡用这个线性图标占位（内置示例模特已从主流程退役，不再借模特图）
const HAIR_ICON = '/assets/icons/tool-hair.png'
// 自定义描述字数上限（服务端同一上限 40 字）
const CUSTOM_MAX = 40
const GENDERS: readonly { id: HairGender; label: string }[] = [
  { id: 'women', label: HAIR_COPY.genderWomen },
  { id: 'men', label: HAIR_COPY.genderMen }
]

/** 卡面只有一行 tag 的宽度：自定义描述在卡上截断，完整文本留给判词与输入层 */
const truncateLabel = (text: string): string => (text.length > 8 ? `${text.slice(0, 8)}…` : text)

/** 结果页横滑槽位：目录方向与「目录外的历史」共用一种形状（有 preview = 已生成过） */
interface ResultSlot {
  key: string
  styleId: string
  name: string
  tag?: string
  desc?: string
  media?: DisplayMedia | null
  preview?: HairPreview
}

// hero 照片统一「直出」：单层真图，永远按宽铺满、顶对齐、底部越界裁切——
// 不给 frameAspect（不出现「按高铺满裁两侧」），任何入图都不裁脸。
// 框高两页一致（.hair__hero 写死 4:3 的 562rpx），照片超出相框的下半截裁掉。
function HeroPhoto(props: { media?: DisplayMedia | null; localPath?: string }) {
  const { media, localPath } = props
  // 工具新渲染层按 CORS 拦截 http://tmp/：本地路径渲染前换出（真机原样）
  const localDisplay = useDisplayablePath(localPath ?? '')
  return (
    <View className="hair__photo">
      {media ? (
        <SourceImage className="hair__photo-img" media={media} anchor="top" mode="widthFix" />
      ) : localPath ? (
        <Image className="hair__photo-img" src={localDisplay} mode="widthFix" />
      ) : null}
    </View>
  )
}

export default function Hair() {
  const [styles, setStyles] = useState<HairStyle[]>([])
  const [styleId, setStyleId] = useState<string>('sharp')
  // 方向按性别分组：选过一次就记住（本地只是 UI 偏好，业务事实仍在服务端）
  const [gender, setGender] = useState<HairGender>(() =>
    readStorage(STORAGE_KEYS.hairGender) === 'men' ? 'men' : 'women'
  )
  // 自定义方向：文本已生效，draft 是弹层里的草稿（点了生成才落到 customText）
  const [customText, setCustomText] = useState('')
  const [customDraft, setCustomDraft] = useState('')
  const [customOpen, setCustomOpen] = useState(false)
  const [preview, setPreview] = useState<HairPreview | null>(null)
  // 生成历史（新到旧）：结果页底部横滑回放，「换个方向」的结果都留在这里
  const [history, setHistory] = useState<HairPreview[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pendingPath, setPendingPath] = useState('')
  // 档案正脸回退：服务端 create 支持 report_id 缺省 media_id（media_id 优先）。
  const [reportId, setReportId] = useState('')
  // 档案正脸本体（带签名 URL）：S0 的 hero 默认显示它——生成默认用这张，先让用户看到自己的脸。
  const [reportFace, setReportFace] = useState<DisplayMedia | null>(null)
  // S3 长按看原图
  const [holdOriginal, setHoldOriginal] = useState(false)
  // S2 生成阶段文案（按时间推进，不是真实进度——真实进度看 Operation）
  const [genStage, setGenStage] = useState(0)
  // 用户已亲手选了照片 = 新意图：异步 resume 落地时不得把旧结果 adopt 回来劫持页面
  const freshIntentRef = useRef(false)
  const { pageClass, enter } = usePageShell(!loading || Boolean(preview), '', 'hair')

  const touchHistory = (item: HairPreview) => {
    setHistory((prev) => [item, ...prev.filter((p) => p.id !== item.id)])
  }

  // 这一侧的方向列表：服务端目录优先，缺项由包内目录补齐（当前 baseline 没有
  // 目录表，所以实际上就是包内目录 + 性别过滤）
  const directions = useMemo(() => hairDirectionViews(gender, styles), [gender, styles])
  const customActive = styleId === CUSTOM_DIRECTION_ID
  const activeDirection = directions.find((item) => item.id === styleId)

  // 选中项必须落在当前这一侧：切性别、服务端目录换 id 都在这里校正（不留隐形选中）
  useEffect(() => {
    if (styleId === CUSTOM_DIRECTION_ID) return
    if (directions.some((item) => item.id === styleId)) return
    const first = directions[0]
    if (first) setStyleId(first.id)
  }, [directions, styleId])

  // 自定义方向与性别无关：切性别时它保持选中，其余情况切到新一侧的第一个方向
  const switchGender = (next: HairGender) => {
    if (next === gender) return
    setGender(next)
    writeStorage(STORAGE_KEYS.hairGender, next)
  }

  const openCustom = () => {
    setCustomDraft(customText)
    setCustomOpen(true)
  }

  // 恢复/回放历史时把方向一起带回来：自定义方向从 style_name 还原（服务端存的就是
  // 那句描述），目录方向按 id 选中（不落在当前一侧时由上面的校正 effect 兜底）
  const adoptDirection = (item: HairPreview) => {
    if (!item.style_id) return
    if (item.style_id === CUSTOM_DIRECTION_ID && item.style_name) setCustomText(item.style_name)
    setStyleId(item.style_id)
  }

  const loadOptions = useCallback(async () => {
    setLoading(true)
    try {
      const items = await peripherals.listHairstyles()
      setStyles(items)
    } catch {
      setStyles([])
    } finally {
      setLoading(false)
    }
  }, [])

  const loadPreview = useCallback(async (id: string) => {
    const item = await peripherals.getHairPreview(id)
    setPreview(item)
    adoptDirection(item)
    touchHistory(item)
    return item
  }, [])

  // 恢复：优先问服务端的进行中端点（最准）；它暂时不可用才退回列表里找
  // （无参列表的语义是返回全部，含进行中）。进行中接回轮询；没有进行中但
  // 有已完成的，直接回放最近一次成果——别让用户生成的结果「消失」成再生成。
  const resume = useCallback(async () => {
    const adopt = (item: HairPreview) => {
      // 用户已经选了新照片：旧任务（含进行中）一概不接管页面
      if (freshIntentRef.current) return
      setPreview(item)
      adoptDirection(item)
    }
    let list: HairPreview[] | null = null
    try {
      const active = await peripherals.getActiveHairPreview()
      // 404 已归一成 null：明确「没有进行中」
      if (active) {
        adopt(active)
        return
      }
    } catch {
      /* 端点异常：退回列表恢复 */
    }
    try {
      list = await peripherals.listHairPreviews()
      setHistory(list)
    } catch {
      /* 无历史：保持新任务态 */
    }
    if (!list) return
    const inflight = list.find((item) => IN_FLIGHT.has(item.state))
    if (inflight) {
      adopt(inflight)
      return
    }
    const ready = list.find((item) => item.state === 'ready' && item.media)
    if (ready) adopt(ready)
  }, [])

  useEffect(() => {
    void loadOptions()
    void resume()
  }, [loadOptions, resume])

  useShowOnce(() => {
    void resume()
  })

  // 受理后的状态只通过公开 Operation 观察；终态后拉一次完整预览
  const running = Boolean(preview && IN_FLIGHT.has(preview.state))
  const { operations } = useOperationPolling({
    operationIds: preview?.operation?.id ? [preview.operation.id] : [],
    enabled: running,
    onSettled: () => {
      if (!preview) return
      void loadPreview(preview.id)
    },
  })

  // S2 阶段文案：生成期间每 6 秒推进一句，终句停住（真实状态以轮询为准）
  useEffect(() => {
    if (!running) return
    setGenStage(0)
    const timer = setInterval(() => {
      setGenStage((s) => Math.min(s + 1, HAIR_COPY.genStages.length - 1))
    }, 6000)
    return () => clearInterval(timer)
  }, [running])

  useEffect(() => {
    void qualityApi
      .getCurrentReport()
      .then((report) => {
        if (!report) return
        setReportId(report.id)
        setReportFace(report.source_media?.face?.media ?? null)
      })
      .catch(() => {})
  }, [])

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      camera: 'front',
      sizeType: ['compressed'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (!file) return
        // 新照片 = 新意图：清掉展示中的旧结果（旧源图/旧成果都不再占 hero），
        // 并挡住还在飞行中的 resume adopt——选完照片页面绝不能跳去旧结果页。
        // HEIC 等非 JPEG/PNG 会被服务端按字节拒：选完先归一成 JPEG。
        freshIntentRef.current = true
        setPreview(null)
        void normalizeUploadableImage(file.tempFilePath).then((path) => setPendingPath(path))
      },
    })
  }

  /**
   * direction 是方向事实：目录收录的风格用它上屏（目录名）与服务端对照，
   * 目录没收录（自定义方向、目录表缺失）时它就是唯一的生成依据——
   * 服务端把它直接拼进提示词（见 service/hair previewPrompt）。
   */
  const generate = async (demo = false, directionOverride?: string, styleIdOverride?: string) => {
    if (busy) return
    // 卡片点选会连带生成，此时 setStyleId 还没落地：方向 id 显式传进来，不用下一次渲染的状态
    const targetStyleId = styleIdOverride ?? styleId
    const isCustom = targetStyleId === CUSTOM_DIRECTION_ID
    const direction = (directionOverride ?? (isCustom ? customText : activeDirection?.name ?? '')).trim()
    if (isCustom && !direction) {
      // 自定义方向没有描述：退回输入层，而不是发一个空方向
      openCustom()
      return
    }
    setBusy(true)
    try {
      let mediaId: string | undefined
      let fallbackReportId: string | undefined
      if (demo) {
        const media = await qualityApi.createDemoMedia('face', `hair-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPendingPath('')
      } else if (pendingPath) {
        const image = await readLocalImage(pendingPath)
        mediaId = (await uploadMedia(mediaUpload, image, 'face')).id
      } else if (reportId) {
        fallbackReportId = reportId
      } else {
        // 没有可用照片时主按钮不当死胡同：直接打开选照片，选完照片进 hero 再确认生成
        choosePhoto()
        return
      }
      const accepted = await peripherals.createHairPreview({
        media_id: mediaId,
        report_id: fallbackReportId,
        style_id: targetStyleId,
        direction,
      })
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      setPreview(accepted.data)
      touchHistory(accepted.data)
    } catch (e) {
      if (handleBillingError(e)) return
      Taro.showToast({ title: (e as Error).message || HAIR_COPY.generateStart, icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  // 自定义描述的提交：先落成当前方向，再拿它去生成（setState 是异步的，
  // 所以文本显式传给 generate，不靠下一次渲染）
  const submitCustom = () => {
    const text = customDraft.trim()
    if (!text || busy) return
    setCustomText(text)
    setStyleId(CUSTOM_DIRECTION_ID)
    setCustomOpen(false)
    // 自定义方向的 style_id 显式传：状态要下一次渲染才生效，不能等它
    void generate(false, text, CUSTOM_DIRECTION_ID)
  }

  const save = async () => {
    if (!preview || preview.saved) return
    try {
      await peripherals.saveHairPreview(preview.id)
      setPreview((prev) => (prev ? { ...prev, saved: true } : prev))
      setHistory((prev) => prev.map((item) => (item.id === preview.id ? { ...item, saved: true } : item)))
      Taro.showToast({ title: HAIR_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  // 历史卡切换：结果都留着，点哪张回放哪张
  const adoptFromHistory = (item: HairPreview) => {
    setPreview(item)
    adoptDirection(item)
    if (item.state === 'ready' && !item.media) void loadPreview(item.id)
  }

  // 当前方向的展示名：结果态以服务端为准（历史回放也是它），未生成时用选中的方向
  const directionName = customActive ? customText : activeDirection?.name ?? ''
  const styleName = preview?.style_name || directionName
  const hasResult = preview?.state === 'ready' && Boolean(preview?.media)
  const generating = running
  const failed = preview?.state === 'failed' || preview?.state === 'unavailable'
  // 主按钮要说实话：没有照片可按（档案未回或没现拍）时，点它发生的是「选照片」
  const needsPhoto = !pendingPath && !reportId
  // 失败原因优先用服务端愿意公开的那句（轮询到的 failed Operation），没有才回退固定文案
  const failedOperation = operations.find((operation) => operation.status === 'failed')
  const failureText = failedOperation?.public_message || HAIR_COPY.generateFailed
  const readyHistory = history.filter((item) => item.state === 'ready' && item.media)

  // 结果页下半屏的横滑槽位：当前性别的全部方向 + 目录外的历史（换过性别、方向已下架）。
  // 有结果的方向带生成图（点它回放，不再花一次生成），没试过的带参考图（点它直接生成）——
  // 4:3 横图只占屏高三分之一，下半屏靠这条轨和固定 CTA 撑住。
  const resultSlots: ResultSlot[] = useMemo(() => {
    const slots: ResultSlot[] = directions.map((view) => ({
      key: view.id,
      styleId: view.id,
      name: view.name,
      tag: view.tag,
      desc: view.desc,
      media: view.media,
      preview: readyHistory.find((item) => item.style_id === view.id),
    }))
    for (const item of readyHistory) {
      if (directions.some((view) => view.id === item.style_id)) continue
      // 目录外的历史：方向来自另一性别（换过性别）或自定义。目录里认得到的按目录补齐
      // 标签与说明——判词不能退回通用导语；认不到的（自定义）用用户那句话当卡面名。
      const known = HAIR_DIRECTIONS.find((view) => view.id === item.style_id)
      slots.push({
        key: item.id,
        styleId: item.style_id || item.id,
        name: known?.name ?? truncateLabel(item.style_name || HAIR_COPY.customName),
        tag: known?.tag,
        desc: known?.desc,
        preview: item,
      })
    }
    // 自定义也是一条方向：结果态横滑同样给入口（没生成过就是那张 ＋ 卡）
    const customPreview = readyHistory.find((item) => item.style_id === CUSTOM_DIRECTION_ID)
    slots.push({
      key: CUSTOM_DIRECTION_ID,
      styleId: CUSTOM_DIRECTION_ID,
      name: customPreview?.style_name ? truncateLabel(customPreview.style_name) : HAIR_COPY.customName,
      preview: customPreview,
    })
    return slots
  }, [directions, readyHistory])

  // 点卡片只做「选中」：已经生成过的顺带把它调出来看（零成本、即时），
  // 没生成过的只改选中态——主按钮随之变成「生成这个效果」，点了才发请求（不偷跑额度）。
  // 自定义方向没有描述可生成，点了直接开输入层。
  const openSlot = (slot: ResultSlot) => {
    if (busy || generating) return
    setStyleId(slot.styleId)
    if (slot.preview) {
      adoptFromHistory(slot.preview)
      return
    }
    if (slot.styleId === CUSTOM_DIRECTION_ID) openCustom()
  }

  const activeDesc = (customActive ? customText : activeDirection?.desc) || HAIR_COPY.desc
  // 判词说的是「这张结果」：说明与差异标签从结果自己的方向取——
  // 结果页上选中态可能已经变了，不能用当前选中项的文案冒充
  const activeSlot = resultSlots.find((slot) => slot.styleId === preview?.style_id)
  const verdictTag = hasResult ? activeSlot?.tag : undefined
  const verdictDesc = hasResult ? activeSlot?.desc || HAIR_COPY.desc : activeDesc
  // 主按钮说实话：选中的方向还没生成过 → 「生成这个效果」（点了才发请求，不偷跑额度）；
  // 选中的就是正在展示的那张结果 → 「保存这个效果」
  const selectedSlot = resultSlots.find((slot) => slot.styleId === styleId)
  const pendingDirection = hasResult && selectedSlot && !selectedSlot.preview ? selectedSlot : undefined
  const resultCtaText = pendingDirection
    ? `生成「${pendingDirection.name}」效果`
    : (preview?.saved ? HAIR_COPY.saved : HAIR_COPY.save)
  const customCardLabel = customActive && customText ? truncateLabel(customText) : HAIR_COPY.customCardHint
  const primaryText = generating
    ? HAIR_COPY.generating
    : needsPhoto
      ? HAIR_COPY.uploadTitle
      : customActive
        ? (customText ? HAIR_COPY.customCta : HAIR_COPY.customName)
        : `生成「${directionName}」预览`

  return (
    <View className={pageClass}>
      {/* 两页共用一套骨架：占位导航（透明栏 + 深色标题）+ 同高相框 + 同段信息区 +
          同尺寸方向卡 + 固定底部 CTA。以前两页各写各的（照片一个贴顶一个不贴、
          卡一个 224 一个 260），落差全落在视觉上 */}
      <AppHeader title="发型设计" back transparent />
      <View className="hair">
        {/* 相框两页一致：750×562rpx＝4:3，照片按宽铺满、顶对齐、超出裁底——
            生成图（服务端 4:3）几乎零裁切，正脸照按同一裁法出「头肩特写」 */}
        <View className={`hair__hero photo-hero photo-hero--bleed ${enter()}`}>
          <View className="hair__hero-frame">
            {hasResult ? (
              // S3 结果：长按看原图——对比是直觉动作，不是模式切换
              <View
                className="hair__compare"
                onTouchStart={() => setHoldOriginal(true)}
                onTouchEnd={() => setHoldOriginal(false)}
                onTouchCancel={() => setHoldOriginal(false)}
              >
                <HeroPhoto media={preview!.media} />
                {preview!.source_media ? (
                  <View className={`hair__compare-original${holdOriginal ? ' hair__compare-original--on' : ''}`}>
                    <HeroPhoto media={preview!.source_media} />
                    <View className="hair__badge">
                      <Text>原本</Text>
                    </View>
                  </View>
                ) : null}
                {preview!.source_media ? (
                  <View className="hair__hold-hint">
                    <Text>{HAIR_COPY.holdOriginal}</Text>
                  </View>
                ) : null}
              </View>
            ) : preview?.source_media ? (
              // S2 生成中：源图 + 沉浸等待（阶段文案 + 可离开明示），不是原地盖 mask
              <>
                <HeroPhoto media={preview.source_media} />
                <View className="hair__badge">
                  <Text>原本</Text>
                </View>
                {generating ? (
                  <View className="hair__gen">
                    <View className="scan-sweep" />
                    <View className="hair__gen-card">
                      <View className="hair__gen-spin spinner" />
                      <View className="hair__gen-texts">
                        <Text className="hair__gen-stage">{HAIR_COPY.genStages[genStage]}</Text>
                        <Text className="hair__gen-note">{HAIR_COPY.genNote}</Text>
                      </View>
                    </View>
                  </View>
                ) : null}
              </>
            ) : pendingPath ? (
              // S0 刚选的正脸照立刻上 hero（本地临时路径，不走 SourceImage 投影）
              <>
                <HeroPhoto localPath={pendingPath} />
                <View className="hair__badge">
                  <Text>原本</Text>
                </View>
              </>
            ) : reportFace ? (
              // S0 档案正脸：生成默认用这张，进来先看到自己的脸
              <>
                <HeroPhoto media={reportFace} />
                <View className="hair__badge">
                  <Text>原本</Text>
                </View>
              </>
            ) : (
              // S0 无照片空态：拍照示范引导，示例模特不再当主流程主角；
              // 示范图同样完整入镜（aspectFill 居中裁会把下巴切掉）
              <View className="hair__upload pressable" onClick={choosePhoto}>
                <HeroPhoto localPath={FACE_GUIDE_IMAGE} />
                <View className="hair__badge">
                  <Text>{HAIR_COPY.guideBadge}</Text>
                </View>
                <View className="hair__upload-bar">
                  <Text className="hair__upload-bar-plus">＋</Text>
                  <Text className="hair__upload-bar-text">{HAIR_COPY.uploadTitle}</Text>
                </View>
              </View>
            )}
            {!hasResult && !generating && !failed && (pendingPath || reportFace) ? (
              // 输入透明化：将用哪张脸生成必须上屏
              <>
                <View className="hair__use-photo">
                  <Text>{HAIR_COPY.useThisPhoto}</Text>
                </View>
                <View className="hair__hero-actions">
                  <Text className="hair__hero-alt pressable" onClick={choosePhoto}>{HAIR_COPY.reselect}</Text>
                </View>
              </>
            ) : null}
          </View>
        </View>

        {hasResult ? (
          <>
            <View className={`hair__head ${enter(1)}`}>
              <View className="hair__verdict card">
                <View className="hair__verdict-head">
                  <Text className="hair__verdict-title serif">{styleName}</Text>
                  {verdictTag ? <Text className="hair__verdict-tag">{verdictTag}</Text> : null}
                </View>
                <Text className="hair__verdict-desc">{verdictDesc}</Text>
                <View className="hair__verdict-foot">
                  <Text className="hair__verdict-note">{HAIR_COPY.resultNote}</Text>
                </View>
              </View>
            </View>

            {/* 换个方向看看：卡面语义只有两类——有生成图＝试过的（点它把它调出来看），
                没图的只是可选项（点它＝选中，主按钮随之变成「生成这个效果」）。
                高亮跟着「选中」走，不跟「正在展示」走。 */}
            <View className={`hair__turn ${enter(2)}`}>
              <View className="hair__turn-head">
                <Text className="hair__turn-label">{HAIR_COPY.directionLabel}</Text>
              </View>
              <ScrollView scroll-x enhanced showScrollbar={false} className="hair__cards">
                <View className="hair__cards-rail">
                  {resultSlots.map((slot) => (
                      <View
                        key={slot.key}
                        className={`hair__card${slot.styleId === styleId ? ' hair__card--active' : ''} pressable`}
                        onClick={() => openSlot(slot)}
                      >
                        {slot.preview?.media ? (
                          <SourceImage className="hair__card-img" media={slot.preview.media} anchor="top" />
                        ) : slot.media ? (
                          <SourceImage className="hair__card-img" media={slot.media} anchor="top" />
                        ) : slot.styleId === CUSTOM_DIRECTION_ID ? (
                          <View className="hair__card-blank">
                            <Text className="hair__card-blank-plus">＋</Text>
                          </View>
                        ) : (
                          <View className="hair__card-blank">
                            <Image className="hair__card-blank-icon" src={HAIR_ICON} mode="aspectFit" />
                          </View>
                        )}
                        <View className="hair__card-body">
                          <Text className="hair__card-name">{slot.name}</Text>
                          {slot.tag ? <Text className="hair__card-tag">{slot.tag}</Text> : null}
                        </View>
                      </View>
                    ))}
                </View>
              </ScrollView>
            </View>

            {/* CTA 固定底部（拇指位的主操作，页面滚动时它不动）：不带 enter——
                fade-up 的 transform 在动画期间会视觉偏移固定栏，穿搭页同此处理 */}
            <View className="hair__foot hair__foot--cta">
              <PrimaryButton
                text={resultCtaText}
                disabled={!pendingDirection && preview!.saved}
                onClick={() => {
                  if (pendingDirection) void generate(false, pendingDirection.name, pendingDirection.styleId)
                  else void save()
                }}
              />
              <View className="hair__foot-row">
                {/* 换方向已经由上面的横滑承担：这条链接只负责「换张照片」这条路 */}
                <Text className="hair__foot-alt pressable" onClick={() => setPreview(null)}>
                  {HAIR_COPY.retakePhoto}
                </Text>
              </View>
            </View>
          </>
        ) : (
          <>
            {/* 信息区：与结果页判词卡同一段高度同一位置 */}
            <View className={`hair__head ${enter(1)}`}>
              <Text className="hair__head-title">{HAIR_COPY.title}</Text>
              <Text className="hair__head-desc">{activeDesc}</Text>
            </View>

            {/* 方向区：标题行左侧＝这一刻在选什么（性别），右侧＝分段；卡行与结果页同一尺寸同一位置。
                末尾一张「自定义」卡：没有合适的方向时用自己的话描述 */}
            <View className={`hair__turn ${enter(2)}`}>
              <View className="hair__turn-head">
                <Text className="hair__turn-label">{HAIR_COPY.genderLabel}</Text>
                <View className="hair__gender-pills">
                  {GENDERS.map((item) => (
                    <Pill
                      key={item.id}
                      label={item.label}
                      active={gender === item.id}
                      onClick={() => !busy && switchGender(item.id)}
                    />
                  ))}
                </View>
              </View>
              <ScrollView scroll-x enhanced showScrollbar={false} className="hair__cards">
                <View className="hair__cards-rail">
                {directions.map((opt) => (
                  <View
                    key={opt.id}
                    className={`hair__card${styleId === opt.id ? ' hair__card--active' : ''} pressable`}
                    onClick={() => setStyleId(opt.id)}
                  >
                    {opt.media ? (
                      <SourceImage className="hair__card-img" media={opt.media} anchor="top" />
                    ) : (
                      // 内置示例模特已从主流程退役（红线修订）：没有服务端图的方向只出
                      // 文字卡，不再拿内置模特图当「风格参考」混在卡面里
                      <View className="hair__card-blank">
                        <Image className="hair__card-blank-icon" src={HAIR_ICON} mode="aspectFit" />
                      </View>
                    )}
                    <View className="hair__card-body">
                      <Text className="hair__card-name">{opt.name}</Text>
                      {opt.tag ? <Text className="hair__card-tag">{opt.tag}</Text> : null}
                      {opt.desc ? <Text className="hair__card-desc">{opt.desc}</Text> : null}
                    </View>
                  </View>
                ))}
                <View
                  className={`hair__card hair__card--custom${customActive ? ' hair__card--active' : ''} pressable`}
                  onClick={() => !busy && openCustom()}
                >
                  <View className="hair__card-blank">
                    <Text className="hair__card-blank-plus">＋</Text>
                  </View>
                  <View className="hair__card-body">
                    <Text className="hair__card-name">{HAIR_COPY.customName}</Text>
                    <Text className="hair__card-tag">{customCardLabel}</Text>
                  </View>
                </View>
                </View>
              </ScrollView>
            </View>

            {failed ? (
              <View className={`hair__failed ${enter()}`}>
                <Text className="hair__failed-text">{failureText}</Text>
                <View className="hair__failed-row">
                  <Text className="hair__failed-action pressable" onClick={choosePhoto}>{HAIR_COPY.changePhoto}</Text>
                  <Text className="hair__failed-action pressable" onClick={() => void generate(false)}>{HAIR_COPY.retry}</Text>
                </View>
              </View>
            ) : null}

            {/* 主按钮同样固定底部：两页的 CTA 位置一致（拇指位），内容从栏底渐显滚过 */}
            <View className="hair__foot hair__foot--cta">
              <PrimaryButton
                text={primaryText}
                loading={busy || generating}
                onClick={() => {
                  // 自定义方向还没写描述时，按钮的下一步是「写描述」而不是发请求
                  if (customActive && !customText) openCustom()
                  else void generate(false)
                }}
              />
              {needsPhoto ? (
                <View className="hair__foot-row">
                  <Text className="hair__foot-alt pressable" onClick={() => void generate(true)}>
                    {HAIR_COPY.demo}
                  </Text>
                </View>
              ) : null}
            </View>
          </>
        )}
      </View>

      {/* 自定义方向的输入层：文字描述直接当方向名与提示词（≤40 字，服务端同限） */}
      <BottomSheet
        open={customOpen}
        title={HAIR_COPY.customTitle}
        onClose={() => setCustomOpen(false)}
      >
        <View className="hair__custom-sheet">
          <Text className="hair__custom-label">{HAIR_COPY.customLabel}</Text>
          <Textarea
            className="hair__custom-input"
            value={customDraft}
            maxlength={CUSTOM_MAX}
            placeholder={HAIR_COPY.customPlaceholder}
            onInput={(event) => setCustomDraft(event.detail.value)}
          />
          <View className="hair__custom-meta">
            <Text className="hair__custom-helper">{HAIR_COPY.customHelper}</Text>
            <Text className="hair__custom-count">
              {customDraft.length >= CUSTOM_MAX ? HAIR_COPY.customTooLong : `${customDraft.length}/${CUSTOM_MAX}`}
            </Text>
          </View>
          <PrimaryButton
            text={HAIR_COPY.customCta}
            disabled={!customDraft.trim() || busy}
            loading={busy}
            onClick={submitCustom}
          />
          {!customDraft.trim() ? <Text className="hair__custom-empty">{HAIR_COPY.customEmpty}</Text> : null}
        </View>
      </BottomSheet>
    </View>
  )
}
