// 穿搭诊断：场景 3 选 + 单图上传 + 同步诊断（锚点标注）+ 先改哪一处。
// 空态：照片 hero → 导语 → 场景。结果：照片 → 一句建议 → 观察清单，不套报告卡。
// 同步请求跨页存活：会话见 services/outfit-session（模块级 inflight + storage 草稿），
// 退回首页再进入可恢复进行中/结论；复访先展示草稿，服务端 latest 只在更新时接管。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { BILLING_COPY, OUTFIT_COPY, type Diagnosis, type DisplayMedia } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { isPublicErrorCode } from '../../../../app/api/result'
import { uploadMedia } from '../../../../app/api/media-upload'
import { normalizeUploadableImage, readLocalImage } from '../../../../features/capture/local-file'
import { handleBillingError } from '../../../../services/billing'
import { billingErrorMessage } from '../../../../services/billing-error'
import {
  isDiagnosticDailyLimit,
  sharedOutfitSession,
  type OutfitResumeEvent,
} from '../../../../services/outfit-session'
import { readStorage, writeStorage } from '../../../../services/storage'
import { splitAdviceTitle } from '../../../../services/advice-title'
import AppHeader, { getNavMetrics } from '../../../../components/app-header'
import { useDisplayablePath } from '../../../../hooks/use-displayable-path'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 空态 hero 的拍照示范图（包内资产，JPEG；人物为内置模特，叠「拍照示范」角标）
const OUTFIT_GUIDE_IMAGE = '/assets/capture/outfit-guide.jpg'

// hero 相框比例（rpx，与 index.scss 一致）：锚点按 aspectFit 可视区重映射。
// 竖版全身照在 aspectFit 下由框高决定显影尺寸——框越高，全身照越大：
// 空态框高 820rpx，结果态满宽出血 750rpx × 框高 1020rpx。
// 结果态的坐标框必须跟着状态走，否则锚点偏移。
const HERO_W = 686
const HERO_H = 820
const HERO_DONE_W = 750
const HERO_DONE_H = 1020
// 结果态照片与导航栏之间的呼吸缝：aspectFit 高度受限时头顶必然贴 stage 顶，
// stage 顶 = 导航底 + 这道缝，头才不会顶着导航栏（stage 内照片区 = 1020-56=964rpx）
const HERO_DONE_GAP = 56
// 结果态 hero 底部的停靠带：竖版全身照高度受限，脚底必然贴 stage 底——
// stage 底边抬高 120rpx，板子上叠（88rpx）与底部融化只盖停靠带，不盖脚
const HERO_DONE_DOCK = 120

const CONTEXTS = [
  { key: 'daily', label: '日常' },
  { key: 'interview', label: '面试' },
  { key: 'date', label: '约会' },
] as const

// 会话单例挂在页面模块上：模块只装载一次，inflight 与草稿因此跨页存活。
const outfitSession = sharedOutfitSession({ read: readStorage, write: writeStorage })

export default function Outfit() {
  const [scene, setScene] = useState<string>('daily')
  const [photoPath, setPhotoPath] = useState('')
  const [photoMedia, setPhotoMedia] = useState<DisplayMedia | null>(null)
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 日限（429/402）体面错误态：明日再来 + 查看上次结果，不进计费购买链
  const [limit, setLimit] = useState<{ title: string; body: string } | null>(null)
  // 照片门禁拒识（422 photo_rejected）：照片保留在 hero，原因上屏，动作是「换一张」
  const [rejectedMsg, setRejectedMsg] = useState('')
  // 所选照片真实宽高（onLoad 采集）：aspectFit 可视区锚点换算
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const resumingRef = useRef(false)
  // 本页点了「再诊断一次」或重选照片：不要立刻用服务端旧结论盖回去
  const freshStartRef = useRef(false)
  // 空态/日限态一屏锁：外壳 .page--lock 锁 100vh 纵排、容器 flex:1 吃满余量，
  // 高度与安全区都在 CSS 里（安全区只垫一次）。结果态要滚动，去掉锁。
  const { pageClass, enter } = usePageShell(true, result ? '' : 'page--lock', 'outfit')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.scene) setScene(item.scene)
    if (item.source_media) setPhotoMedia(item.source_media)
  }, [])

  // 日限与通用错误分流：日限进体面错误态（明日再来/查看上次结果），其余照旧。
  const applyError = useCallback((e: unknown, fallback: string) => {
    if (isDiagnosticDailyLimit(e)) {
      const serverMsg = billingErrorMessage(e)
      setLimit({
        title: serverMsg || BILLING_COPY.rateLimited,
        body: serverMsg && serverMsg !== BILLING_COPY.rateLimited ? BILLING_COPY.rateLimited : '',
      })
      setError('')
      return
    }
    if (isPublicErrorCode(e, 'photo_rejected')) {
      // 拒识不是「失败重试」：同一张照片再诊一次结果一样，动作必须是换照片
      setRejectedMsg((e as Error)?.message || '这张照片不适合诊断，换一张试试')
      setError('')
      return
    }
    handleBillingError(e)
    setError((e as Error)?.message || fallback)
  }, [])

  // 复访恢复：会话统一编排（草稿水合 → inflight 接管 → pending 续跑 → 后台校验），
  // 事件在这里翻译成 setState；时序与防旧盖新判定都在 services/outfit-session。
  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      await outfitSession.resume(
        {
          getLatest: () => peripherals.getLatestDiagnosis('outfit'),
          diagnose: peripherals.diagnose,
        },
        (event: OutfitResumeEvent) => {
          if (event.type === 'hydrate') {
            const { draft } = event
            if (draft.scene) setScene(draft.scene)
            if (draft.photoPath) setPhotoPath(draft.photoPath)
            if (draft.result) applySession(draft.result)
            return
          }
          if (event.type === 'busy') {
            setBusy(event.busy)
            if (event.busy) {
              setError('')
              setLimit(null)
              setRejectedMsg('')
            }
            return
          }
          if (event.type === 'result') {
            applySession(event.item)
            return
          }
          applyError(event.error, '诊断没有成功，请重试')
        },
        { freshStart: () => freshStartRef.current },
      )
    } finally {
      resumingRef.current = false
    }
  }, [applySession, applyError])

  useEffect(() => {
    void resume()
  }, [resume])

  useShowOnce(() => {
    void resume()
  })

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sizeType: ['compressed'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (!file) return
        // HEIC 等非 JPEG/PNG 会被服务端按字节拒：选完先归一成 JPEG 再进状态与草稿
        void normalizeUploadableImage(file.tempFilePath).then((path) => {
          setPhotoPath(path)
          setPhotoMedia(null)
          setResult(null)
          setPhotoDims(null)
          setError('')
          setLimit(null)
          setRejectedMsg('')
          freshStartRef.current = true
          // 草稿同步换成新照片：进行中的旧请求落定时就不会再盖回来
          outfitSession.writeDraft({
            pending: false,
            scene,
            photoPath: path,
            mediaId: '',
            result: null,
          })
        })
      },
    })
  }

  const analyze = async (demo = false) => {
    // inflight 期间重复发起一律被会话挡下（busy 只是本页实例的第二道闸）
    if (outfitSession.getInflight() || busy) return
    setBusy(true)
    setError('')
    setLimit(null)
    setRejectedMsg('')
    // 提到 try 外：catch 要用它判断「草稿是否已换照片」
    let mediaId = ''
    try {
      if (demo) {
        const media = await qualityApi.createDemoMedia('body', `outfit-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPhotoMedia(media)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoMedia) {
          Taro.showToast({ title: '先上传一张全身照', icon: 'none' })
          return
        }
        if (photoPath) {
          const image = await readLocalImage(photoPath)
          mediaId = (await uploadMedia(mediaUpload, image, 'body')).id
        } else {
          mediaId = photoMedia!.asset_id
        }
      }
      outfitSession.markPending({ scene, photoPath: demo ? '' : photoPath, mediaId })
      const item = await outfitSession.runDiagnose(mediaId, () =>
        peripherals.diagnose({ kind: 'outfit', media_id: mediaId, scene }),
      )
      // 飞行中重选了照片：旧结论已被会话拒收，这里也不上屏
      if (outfitSession.read()?.result?.id === item.id) applySession(item)
    } catch (e) {
      // mediaId 为空说明上传/示例图就没走通，错误照常展示；非空但草稿已换照片则静默
      if (mediaId && outfitSession.read()?.mediaId !== mediaId) return
      applyError(e, '诊断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const saveResult = async () => {
    if (!result) return
    try {
      await peripherals.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: OUTFIT_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const toPlans = () => Taro.switchTab({ url: '/pages/plans/index' })

  // 日限错误态的「查看上次结果」：历史进来就替换掉当前未完成的照片位
  const viewHistory = async () => {
    try {
      const latest = await peripherals.getLatestDiagnosis('outfit')
      if (!latest) return
      outfitSession.writeDraft({ photoPath: '' })
      outfitSession.markDone(latest)
      setPhotoPath('')
      applySession(latest)
      setLimit(null)
      setError('')
      setRejectedMsg('')
    } catch {
      /* 拉不到历史：保持日限错误态 */
    }
  }

  const retryFresh = () => {
    freshStartRef.current = true
    outfitSession.clearResult()
    setResult(null)
    setError('')
    setLimit(null)
    setRejectedMsg('')
  }

  const shownMedia: DisplayMedia | null = photoMedia ?? result?.source_media ?? null
  // 工具新渲染层按 CORS 拦截 http://tmp/、http://usr/：渲染用换出值，
  // 状态与草稿里存原路径（上传、跨页恢复都靠它）
  const photoDisplayPath = useDisplayablePath(photoPath)
  // 结果态 hero 出血到屏幕顶：导航区 + 一道呼吸缝只铺模糊衬底，前景照片与
  // 锚点层整体下移——否则头部顶进状态栏/灵动岛，或贴着导航栏下沿，都不协调。
  // hero 总高不变（导航高 + (1020+120)rpx），stage 下移的 56rpx 从照片区扣（964rpx）。
  const nav = getNavMetrics()
  const rpxPx = nav.windowWidth / 750
  const doneHeroStyle = result
    ? { height: `${nav.navHeight + (HERO_DONE_H + HERO_DONE_DOCK) * rpxPx}px` }
    : undefined
  const doneStageStyle = result
    ? {
        top: `${nav.navHeight + HERO_DONE_GAP * rpxPx}px`,
        bottom: `${HERO_DONE_DOCK * rpxPx}px`,
      }
    : undefined
  // 锐图四边羽化：照片可见矩形（aspectFit letterbox）用 photoDims 实测，
  // mask 渐变精确压在照片四条边上，溶进底下的对焦模糊层——横图竖图、
  // 任何屏宽都没有矩形硬边。photoDims 未就绪前用 SCSS 里的兜底渐变。
  const FEATHER = 44
  const stageWpx = nav.windowWidth
  const stageHpx = (result ? HERO_DONE_H - HERO_DONE_GAP : HERO_H) * rpxPx
  let photoMaskVars: Record<string, string> | undefined
  if (photoDims && photoDims.w > 0 && photoDims.h > 0) {
    const sc = Math.min(stageWpx / photoDims.w, stageHpx / photoDims.h)
    const vw = photoDims.w * sc
    const vh = photoDims.h * sc
    const offL = (stageWpx - vw) / 2
    const offT = (stageHpx - vh) / 2
    photoMaskVars = {
      '--mv0': `${offT}px`,
      '--mv1': `${offT + FEATHER}px`,
      '--mv2': `${Math.max(offT + vh - FEATHER, offT + FEATHER)}px`,
      '--mv3': `${offT + vh}px`,
      '--mh0': `${offL}px`,
      '--mh1': `${offL + FEATHER}px`,
      '--mh2': `${Math.max(offL + vw - FEATHER, offL + FEATHER)}px`,
      '--mh3': `${offL + vw}px`,
    }
  }
  const photoStageStyle = { ...doneStageStyle, ...photoMaskVars } as React.CSSProperties
  const adviceTitle = result?.priority_title || result?.conclusion || OUTFIT_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  // 服务端 schema 收紧前的存量结论可能带发型/妆容观察：穿搭页只上穿搭域
  // （锚点层也从这里取，被滤掉的观察不能留着锚点飘在照片上）
  const OFF_DOMAIN_CATEGORIES = new Set(['hair', 'makeup'])
  const domainFindings = (result?.findings ?? []).filter((item) => !OFF_DOMAIN_CATEGORIES.has(item.category))
  const keepFindings = domainFindings.filter((item) => item.tone === 'positive')
  const liftFindings = domainFindings.filter((item) => item.tone !== 'positive')

  // 编号锚点：只有「还可以改」的观察上照片——页里的 ①②③ 与改衣单索引一一对应。
  // 坐标用 photoDims 实测的 aspectFit 可视区换算（与羽化 mask 同一套几何）。
  const CIRCLED = ['①', '②', '③', '④', '⑤', '⑥']
  let numberedPins: { key: string; mark: string; x: number; y: number }[] = []
  if (result && !busy && photoDims && photoDims.w > 0 && photoDims.h > 0 && photoMaskVars) {
    const sc = Math.min(stageWpx / photoDims.w, stageHpx / photoDims.h)
    const vw = photoDims.w * sc
    const vh = photoDims.h * sc
    const offL = (stageWpx - vw) / 2
    const offT = (stageHpx - vh) / 2
    numberedPins = liftFindings
      .map((finding, index) => {
        if (finding.anchor_x == null || finding.anchor_y == null) return null
        return {
          key: `${finding.category}-${finding.label}`,
          mark: CIRCLED[index] ?? String(index + 1),
          x: offL + finding.anchor_x * vw,
          y: offT + finding.anchor_y * vh,
        }
      })
      .filter((pin): pin is { key: string; mark: string; x: number; y: number } => pin !== null)
  }

  return (
    <View className={pageClass}>
      <AppHeader title="穿搭诊断" back onPhoto={Boolean(result)} />
      {/* 空态/日限态一屏钉死：外壳 .page--lock 锁 100vh 纵排，容器 flex:1
          吃满导航以下的全部余量（高度不进 JS，安全区只由 CSS env 垫一次）。
          结果态解除锁走「钉屏照片 + 单据上滚」 */}
      <View className={`od${result ? ' od--done' : ''}`}>
        <View className={`od__hero photo-hero photo-hero--bleed ${enter()}`} style={doneHeroStyle}>
          {shownMedia ? (
            <>
              <SourceImage className="od__photo-backdrop" media={shownMedia} mode="aspectFill" />
              <View className="od__stage od__stage--photo" style={photoStageStyle}>
                {/* 对焦模糊层：同一张照片、同一 aspectFit 几何，虚化放大垫在锐图底下——
                    锐图边缘落在「同一画面的失焦版」上，横图/竖图都没有硬边和分割线 */}
                <SourceImage className="od__stage-blur" media={shownMedia} mode="aspectFit" />
                <SourceImage
                  className="od__hero-img od__hero-img--fit"
                  media={shownMedia}
                  mode="aspectFit"
                  onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
                />
                {/* 裁切角标：量体裁衣的取景框——四角 L 形墨线压在照片四角 */}
              </View>
            </>
          ) : photoPath ? (
            <>
              <Image className="od__photo-backdrop" src={photoDisplayPath} mode="aspectFill" />
              <View className="od__stage od__stage--photo" style={photoStageStyle}>
                <Image className="od__stage-blur" src={photoDisplayPath} mode="aspectFit" />
                <Image
                  className="od__hero-img od__hero-img--fit"
                  src={photoDisplayPath}
                  mode="aspectFit"
                  onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
                />
              </View>
            </>
          ) : (
            <View className="od__upload pressable" onClick={choosePhoto}>
              <SourceImage className="od__upload-backdrop" reference={{ slug: 'natural', variant: 'full' }} />
              {/* 示范图讲的就是「站好、全身入镜」：aspectFit 完整入镜，
                  aspectFill 居中裁会把头顶和鞋子一起切掉 */}
              <SourceImage
                className="od__hero-img"
                reference={{ slug: 'natural', variant: 'full' }}
                mode="aspectFit"
              />
              <View className="od__upload-bar">
                <Text className="od__upload-bar-plus">＋</Text>
                <Text className="od__upload-bar-text">{OUTFIT_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownMedia || photoPath ? (
            // 结果态导航透明不占位：chip 让到导航栏下方（真机测量 px，同 AppHeader 做法）
            <View className="od__hero-actions" style={result ? { top: `${getNavMetrics().navHeight + 8}px` } : undefined}>
              <Text className="od__hero-alt pressable" onClick={choosePhoto}>{OUTFIT_COPY.reselect}</Text>
            </View>
          ) : null}
          {busy ? (
            <View className="od__mask">
              <View className="od__mask-spin spinner" />
              <Text className="od__mask-text">{OUTFIT_COPY.busyHint}</Text>
            </View>
          ) : null}
          {result ? (
            // 标记层：不进 photo stage（羽化 mask 会把角标一起渐隐），
            // 角标常驻取景框，编号锚点与改衣单索引一一对应
            <View className="od__stage od__stage--anno" style={doneStageStyle}>
              <View className="od__crop od__crop--tl" />
              <View className="od__crop od__crop--tr" />
              <View className="od__crop od__crop--bl" />
              <View className="od__crop od__crop--br" />
              {!busy
                ? numberedPins.map((pin) => (
                    <Text
                      key={pin.key}
                      className="od__pin"
                      style={{ left: `${pin.x}px`, top: `${pin.y}px` }}
                    >
                      {pin.mark}
                    </Text>
                  ))
                : null}
            </View>
          ) : null}
        </View>
        {/* hero 在结果态是 fixed 钉住的，文档流里放同高占位——
            卡片从钉住的照片上滚过（「hero 不滚，只滚卡片」） */}
        {result ? <View style={doneHeroStyle} /> : null}

        {result ? (
          <>
            <View className={`od__slip ${enter(1)}`}>
              {/* 改衣单：编辑排版的核心——小字眉题 + 衬线大标 + 细规线围出的引文区 */}
              <View className="od__advice">
                <View className="od__mast od__mast--slip">
                  <Text className="od__mast-meta">{OUTFIT_COPY.adviceLabel}</Text>
                  <Text className="od__mast-rule">按「{CONTEXTS.find((c) => c.key === scene)?.label ?? scene}」</Text>
                </View>
                {adviceLead ? <Text className="od__advice-lead">{adviceLead}</Text> : null}
                <Text className="od__advice-title serif">{adviceAction}</Text>
                {adviceBody ? <Text className="od__advice-body">{adviceBody}</Text> : null}
              </View>

              {/* 还可以改：编号索引——①②③ 与照片锚点一一对应，细规线分行 */}
              {liftFindings.length > 0 ? (
                <View className="od__index">
                  <Text className="od__index-label">{OUTFIT_COPY.findingsLift}</Text>
                  {liftFindings.map((finding) => {
                    const pinIndex = liftFindings.indexOf(finding)
                    return (
                      <View key={`${finding.category}-${finding.label}`} className="od__index-row">
                        <Text className="od__index-mark serif">{CIRCLED[pinIndex] ?? '·'}</Text>
                        <Text className="od__index-text">{finding.label}</Text>
                      </View>
                    )
                  })}
                </View>
              ) : null}

              {/* 已经合适：页边批注——竖规线 + 小字，安静退到页边 */}
              {keepFindings.length > 0 ? (
                <View className="od__keepnote">
                  <Text className="od__keepnote-label">{OUTFIT_COPY.keepNoteLabel} · {OUTFIT_COPY.findingsKeep}</Text>
                  <Text className="od__keepnote-text">{keepFindings.map((item) => item.label).join('、')}</Text>
                </View>
              ) : null}

              {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
            </View>

            {/* CTA 固定底部：必须是 sheet 的平级节点——sheet 的 fade-up 动画带
                transform，fixed 放它里面会被困成相对 sheet 定位 */}
            <View className="od__cta">
              <PrimaryButton text={OUTFIT_COPY.toPlans} onClick={toPlans} />
              <View className="od__result-row">
                <Text className="od__result-alt pressable" onClick={() => void saveResult()}>{OUTFIT_COPY.save}</Text>
                <Text className="od__result-alt pressable" onClick={retryFresh}>{OUTFIT_COPY.again}</Text>
              </View>
            </View>
          </>
        ) : limit ? (
          <>
            <View className={`od__hint ${enter(1)}`}>
              <Text className="od__hint-title">{limit.title}</Text>
              {limit.body ? <Text className="od__hint-desc">{limit.body}</Text> : null}
            </View>
            <View className={`od__foot ${enter(2)}`}>
              <PrimaryButton text={OUTFIT_COPY.lastResult} onClick={() => void viewHistory()} />
            </View>
          </>
        ) : (
          <>
            {/* 刊头：衬线大标两行叠排 + 眉题细字——杂志开篇的排版 */}
            <View className={`od__masthead ${enter(1)}`}>
              <View className="od__mast">
                <Text className="od__mast-meta">{OUTFIT_COPY.mastheadMeta}</Text>
                <Text className="od__mast-rule">№ 01</Text>
              </View>
              <Text className="od__masthead-title serif">{OUTFIT_COPY.title}</Text>
              <Text className="od__masthead-desc">{OUTFIT_COPY.desc}</Text>
            </View>

            {/* 须知/读单：编号细目常驻（旧版信息结构）——无照片教拍摄，
                有照片教读单；版心不因选完照片而空一段 */}
            <View className={`od__notice ${enter(1)}`}>
              <Text className="od__notice-label">
                {shownMedia || photoPath ? OUTFIT_COPY.readingTipsLabel : OUTFIT_COPY.tipsLabel}
              </Text>
              {(shownMedia || photoPath ? OUTFIT_COPY.readingTips : OUTFIT_COPY.uploadTips).map((tip, index) => (
                <View key={tip} className="od__notice-row">
                  <Text className="od__notice-no serif">{String(index + 1).padStart(2, '0')}</Text>
                  <Text className="od__notice-text">{tip}</Text>
                </View>
              ))}
            </View>

            {/* 场景章节：目录式选择——衬线序号 + 下划线激活 */}
            <View className={`od__context ${enter(2)}`}>
              <Text className="od__context-label">{OUTFIT_COPY.sceneLabel}</Text>
              <View className="od__context-list">
                {CONTEXTS.map((c, index) => (
                  <View
                    key={c.key}
                    className={`od__chapter pressable ${scene === c.key ? 'od__chapter--active' : ''}`}
                    onClick={() => !busy && setScene(c.key)}
                  >
                    <Text className="od__chapter-no serif">{['一', '二', '三'][index]}</Text>
                    <Text className="od__chapter-label">{c.label}</Text>
                  </View>
                ))}
              </View>
            </View>
            {rejectedMsg ? (
              <View className={`od__rejected ${enter(2)}`}>
                <Text className="od__rejected-text">{rejectedMsg}</Text>
              </View>
            ) : error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
          </>
        )}

        {!result && !limit ? (
          <View className={`od__foot ${enter(3)}`}>
            <PrimaryButton
              text={rejectedMsg ? '重新选择照片' : busy ? OUTFIT_COPY.busy : OUTFIT_COPY.start}
              loading={busy}
              disabled={!rejectedMsg && !photoPath && !photoMedia}
              onClick={() => (rejectedMsg ? choosePhoto() : void analyze(false))}
            />
            <View className="od__foot-row">
              <Text className="od__foot-alt pressable" onClick={() => void analyze(true)}>{OUTFIT_COPY.demo}</Text>
            </View>
          </View>
        ) : null}
      </View>
    </View>
  )
}
