// 发型设计：四步线性流程——确认照片(S0) → 选方向(S1) → 生成中(S2) → 结果·对比(S3)。
// hero 一屏一事：S0 将使用的正脸（示例模特图已从主流程退役）/ S2 源图+沉浸等待 /
// S3 效果图（长按看原图）。生成历史在结果页底部横滑回放，「换个方向」不再丢结果。
// 预览是异步受理（202 + 公开 Operation）；恢复先问服务端 /v1/hair-previews/active，
// 端点异常退回列表；没有进行中则回放最近一次成果（list 新到旧）。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, ScrollView, Text, View } from '@tarojs/components'
import { HAIR_COPY, LOCAL_LOOK_SLUGS, type DisplayMedia, type HairPreview, type HairStyle } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { uploadMedia } from '../../../../app/api/media-upload'
import { normalizeUploadableImage, readLocalImage } from '../../../../features/capture/local-file'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { useOperationPolling } from '../../../../app/operations/use-operation-polling'
import { handleBillingError } from '../../../../services/billing'
import AppHeader from '../../../../components/app-header'
import { useDisplayablePath } from '../../../../hooks/use-displayable-path'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import './index.scss'

const IN_FLIGHT = new Set(['queued', 'generating', 'checking'])
// 空态的正脸拍照示范（包内资产，JPEG）
const FACE_GUIDE_IMAGE = '/assets/capture/face.jpg'
// 服务端目录缺席时的兜底三款；desc 是选中前的差异依据（缩略图看不清发型差别）
const STYLES = [
  { id: 'sharp', name: '锁骨层次发', tag: '中长 · 层次', desc: '修饰脸型线条，利落不挑人' },
  { id: 'warm', name: '空气微卷', tag: '微卷 · 蓬松', desc: '蓬松显发量，柔和日常感' },
  { id: 'natural', name: '自然偏分', tag: '偏分 · 利落', desc: '干净利落，省心百搭' },
] as const

// hero 照片统一「直出」：单层真图 anchor=top 按宽铺满、顶对齐、底部越界裁切，
// 无模糊无羽化。框随图走——onLoad 拿真实宽高，hero 高度 = 内容宽 × 高宽比，
// 钳在 [515, 900]rpx：4:3 头肩生成图落在下限附近，竖版正脸照到上限不再切下巴
//（S0 是「确认将用哪张脸」，看不到全脸就无从确认；S3 长发型的发尾同样不能被裁）。
function HeroPhoto(props: {
  media?: DisplayMedia | null
  localPath?: string
  onHeightChange?: (rpx: number) => void
}) {
  const { media, localPath, onHeightChange } = props
  // 工具新渲染层按 CORS 拦截 http://tmp/：本地路径渲染前换出（真机原样）
  const localDisplay = useDisplayablePath(localPath ?? '')
  const handleLoad = (event: { detail: { width: number | string; height: number | string } }) => {
    const width = Number(event.detail.width)
    const height = Number(event.detail.height)
    if (!onHeightChange || width <= 0 || height <= 0) return
    const raw = (686 * height) / width // 686rpx：页面内容区宽（750 − 两侧页边距）
    onHeightChange(Math.round(Math.min(900, Math.max(515, raw))))
  }
  return (
    <View className="hair__photo">
      {media ? (
        <SourceImage
          className="hair__photo-img"
          media={media}
          anchor="top"
          frameAspect={4 / 3}
          onLoad={handleLoad}
        />
      ) : localPath ? (
        <Image className="hair__photo-img" src={localDisplay} mode="widthFix" onLoad={handleLoad} />
      ) : null}
    </View>
  )
}

export default function Hair() {
  const [styles, setStyles] = useState<HairStyle[]>([])
  const [styleId, setStyleId] = useState<string>('sharp')
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
  // 框随图走：hero 高度由 HeroPhoto onLoad 按真实宽高算出（钳位见 HeroPhoto）
  const [heroHeight, setHeroHeight] = useState<number | null>(null)
  // S2 生成阶段文案（按时间推进，不是真实进度——真实进度看 Operation）
  const [genStage, setGenStage] = useState(0)
  // 用户已亲手选了照片 = 新意图：异步 resume 落地时不得把旧结果 adopt 回来劫持页面
  const freshIntentRef = useRef(false)
  const { pageClass, enter } = usePageShell(!loading || Boolean(preview), '', 'hair')

  const touchHistory = (item: HairPreview) => {
    setHistory((prev) => [item, ...prev.filter((p) => p.id !== item.id)])
  }

  const loadOptions = useCallback(async () => {
    setLoading(true)
    try {
      const items = await peripherals.listHairstyles()
      setStyles(items)
      if (items[0]) setStyleId(items[0].id)
    } catch {
      setStyles([])
    } finally {
      setLoading(false)
    }
  }, [])

  const loadPreview = useCallback(async (id: string) => {
    const item = await peripherals.getHairPreview(id)
    setPreview(item)
    if (item.style_id) setStyleId(item.style_id)
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
      if (item.style_id) setStyleId(item.style_id)
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

  const generate = async (demo = false) => {
    if (busy) return
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
        style_id: styleId,
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
    if (item.style_id) setStyleId(item.style_id)
    if (item.state === 'ready' && !item.media) void loadPreview(item.id)
  }

  const styleName = preview?.style_name || STYLES.find((s) => s.id === styleId)?.name || ''
  const hasResult = preview?.state === 'ready' && Boolean(preview?.media)
  const generating = running
  const failed = preview?.state === 'failed' || preview?.state === 'unavailable'
  // 主按钮要说实话：没有照片可按（档案未回或没现拍）时，点它发生的是「选照片」
  const needsPhoto = !pendingPath && !reportId
  // 失败原因优先用服务端愿意公开的那句（轮询到的 failed Operation），没有才回退固定文案
  const failedOperation = operations.find((operation) => operation.status === 'failed')
  const failureText = failedOperation?.public_message || HAIR_COPY.generateFailed
  const readyHistory = history.filter((item) => item.state === 'ready' && item.media)
  const styleOptions: HairStyle[] = styles.length > 0
    ? styles
    : STYLES.map((s) => ({ id: s.id, name: s.name, media: undefined, reason: s.desc } as unknown as HairStyle))
  const activeDesc =
    styleOptions.find((o) => o.id === styleId)?.reason || HAIR_COPY.desc

  return (
    <View className={pageClass}>
      <AppHeader title="发型设计" back />
      <View className={`hair${hasResult ? ' hair--done' : ''}`}>
        {/* S3 结果态相框拉高成竖幅：竖版生成图近乎满框，不再挤成中间一条 */}
        <View
          className={`hair__hero photo-hero photo-hero--bleed${hasResult ? ' hair__hero--done' : ''} ${enter()}`}
          style={heroHeight ? { height: `${heroHeight}rpx` } : undefined}
        >
          <View className="hair__hero-frame">
            {hasResult ? (
              // S3 结果：长按看原图——对比是直觉动作，不是模式切换
              <View
                className="hair__compare"
                onTouchStart={() => setHoldOriginal(true)}
                onTouchEnd={() => setHoldOriginal(false)}
                onTouchCancel={() => setHoldOriginal(false)}
              >
                <HeroPhoto media={preview!.media} onHeightChange={setHeroHeight} />
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
                <HeroPhoto media={preview.source_media} onHeightChange={setHeroHeight} />
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
                <HeroPhoto localPath={pendingPath} onHeightChange={setHeroHeight} />
                <View className="hair__badge">
                  <Text>原本</Text>
                </View>
              </>
            ) : reportFace ? (
              // S0 档案正脸：生成默认用这张，进来先看到自己的脸
              <>
                <HeroPhoto media={reportFace} onHeightChange={setHeroHeight} />
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
            <View className={`hair__verdict ${enter(1)}`}>
              <Text className="hair__verdict-title serif">{styleName}</Text>
              <Text className="hair__verdict-desc">{activeDesc}</Text>
            </View>

            {readyHistory.length > 1 ? (
              <View className={`hair__history ${enter(2)}`}>
                <Text className="hair__history-label">{HAIR_COPY.historyLabel}</Text>
                <ScrollView scroll-x enhanced showScrollbar={false} className="hair__history-scroll">
                  <View className="hair__history-rail">
                    {readyHistory.map((item) => (
                      <View
                        key={item.id}
                        className={`hair__history-card${item.id === preview!.id ? ' hair__history-card--active' : ''} pressable`}
                        onClick={() => adoptFromHistory(item)}
                      >
                        <SourceImage className="hair__history-img" media={item.media} anchor="top" />
                        <Text className="hair__history-name">{item.style_name}</Text>
                      </View>
                    ))}
                  </View>
                </ScrollView>
              </View>
            ) : null}

            {/* CTA 固定底部（竖幅相框会把它挤下屏）：不带 enter——
                fade-up 的 transform 在动画期间会视觉偏移固定栏，穿搭页同此处理 */}
            <View className="hair__foot hair__foot--cta">
              <PrimaryButton
                text={preview!.saved ? HAIR_COPY.saved : HAIR_COPY.save}
                disabled={preview!.saved}
                onClick={() => void save()}
              />
              <View className="hair__foot-row">
                <Text className="hair__foot-alt pressable" onClick={() => setPreview(null)}>
                  {HAIR_COPY.tryAnother}
                </Text>
              </View>
            </View>
          </>
        ) : (
          <>
            <View className={`hair__hint ${enter(1)}`}>
              <Text className="hair__hint-title">{HAIR_COPY.title}</Text>
              <Text className="hair__hint-desc">{activeDesc}</Text>
            </View>

            {/* S1 方向卡：图 + 名 + 差异标签 + 一句适合谁，选中前的差异全部前置 */}
            <ScrollView scroll-x enhanced showScrollbar={false} className={`hair__cards ${enter(2)}`}>
              <View className="hair__cards-rail">
                {styleOptions.map((opt) => {
                  const tag = STYLES.find((s) => s.id === opt.id)?.tag
                  const desc = opt.reason || STYLES.find((s) => s.id === opt.id)?.desc
                  return (
                    <View
                      key={opt.id}
                      className={`hair__card${styleId === opt.id ? ' hair__card--active' : ''} pressable`}
                      onClick={() => setStyleId(opt.id)}
                    >
                      {opt.media ? (
                        <SourceImage className="hair__card-img" media={opt.media} anchor="top" />
                      ) : (
                        <SourceImage
                          className="hair__card-img"
                          reference={{ slug: (LOCAL_LOOK_SLUGS as readonly string[]).includes(opt.id) ? opt.id : 'sharp', variant: 'hair' }}
                          anchor="top"
                        />
                      )}
                      <View className="hair__card-body">
                        <Text className="hair__card-name">{opt.name}</Text>
                        {tag ? <Text className="hair__card-tag">{tag}</Text> : null}
                        {desc ? <Text className="hair__card-desc">{desc}</Text> : null}
                      </View>
                    </View>
                  )
                })}
              </View>
            </ScrollView>

            {failed ? (
              <View className={`hair__failed ${enter()}`}>
                <Text className="hair__failed-text">{failureText}</Text>
                <View className="hair__failed-row">
                  <Text className="hair__failed-action pressable" onClick={choosePhoto}>{HAIR_COPY.changePhoto}</Text>
                  <Text className="hair__failed-action pressable" onClick={() => void generate(false)}>{HAIR_COPY.retry}</Text>
                </View>
              </View>
            ) : null}

            <View className={`hair__foot ${enter(3)}`}>
              <PrimaryButton
                text={generating ? HAIR_COPY.generating : needsPhoto ? HAIR_COPY.uploadTitle : `生成「${styleName}」预览`}
                loading={busy || generating}
                onClick={() => void generate(false)}
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
    </View>
  )
}
