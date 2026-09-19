// 购买判断：单品图上传 + 同步诊断。呈现走「检验单」票据排版——送检处、
// 巨号判断、盖章、双栏对峙、点线收据、撕票孔 CTA；照片本体仍是满幅标本，
// 硬边语言全在纸面（单据）上。
// 同步请求跨页存活：会话见 services/purchase-session（模块级 inflight + storage 草稿），
// 退回首页再进入可恢复进行中/结论；复访先展示草稿，服务端 latest 只在更新时接管。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { BILLING_COPY, PURCHASE_COPY, type Diagnosis, type DisplayMedia } from '@zsm/core'
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
  sharedPurchaseSession,
  type PurchaseResumeEvent,
} from '../../../../services/purchase-session'
import { readStorage, writeStorage } from '../../../../services/storage'
import { splitAdviceTitle } from '../../../../services/advice-title'
import AppHeader, { getNavMetrics } from '../../../../components/app-header'
import { useDisplayablePath } from '../../../../hooks/use-displayable-path'
import SourceImage from '../../../../components/source-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 会话单例挂在页面模块上：模块只装载一次，inflight 与草稿因此跨页存活。
const purchaseSession = sharedPurchaseSession({ read: readStorage, write: writeStorage })

// 结果态沉浸 hero（照片标本层，与穿搭诊断同一套机械）：
// 满宽出血 + 透明导航；stage 上下各让导航呼吸缝 56rpx / 底部停靠带 120rpx，
// 锐图四边按 photoDims 实测羽化。
const HERO_H = 640
const HERO_DONE_H = 820
const HERO_DONE_GAP = 56
const HERO_DONE_DOCK = 120
// 空态 hero 相框 750rpx 宽 × 640rpx 高：anchor=top 自动填充据此决定铺宽还是铺高
const HERO_ASPECT = 750 / 640

export default function Purchase() {
  const [photoPath, setPhotoPath] = useState('')
  const [photoMedia, setPhotoMedia] = useState<DisplayMedia | null>(null)
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 日限（429/402）体面错误态：明日再来 + 查看上次结果，不进计费购买链
  const [limit, setLimit] = useState<{ title: string; body: string } | null>(null)
  // 照片门禁拒识（422 photo_rejected）：图片保留在 hero，原因上屏，动作是「换一张」
  const [rejectedMsg, setRejectedMsg] = useState('')
  // 所选图片真实宽高（onLoad 采集）：aspectFit 可见区羽化坐标换算
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const resumingRef = useRef(false)
  const freshStartRef = useRef(false)
  // 空态/日限态一屏锁：外壳 .page--lock 锁 100vh 纵排、容器 flex:1 吃满余量，
  // 高度与安全区都在 CSS 里（安全区只垫一次）。结果态要滚动，去掉锁。
  const { pageClass, enter } = usePageShell(true, result ? '' : 'page--lock', 'purchase')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
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
      // 拒识不是「失败重试」：同一张图再判一次结果一样，动作必须是换图
      setRejectedMsg((e as Error)?.message || '这张图片不适合判断，换一张试试')
      setError('')
      return
    }
    handleBillingError(e)
    setError((e as Error)?.message || fallback)
  }, [])

  // 复访恢复：会话统一编排（草稿水合 → inflight 接管 → pending 续跑 → 后台校验），
  // 事件在这里翻译成 setState；时序与防旧盖新判定都在 services/purchase-session。
  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      await purchaseSession.resume(
        {
          getLatest: () => peripherals.getLatestDiagnosis('purchase'),
          diagnose: peripherals.diagnose,
        },
        (event: PurchaseResumeEvent) => {
          if (event.type === 'hydrate') {
            const { draft } = event
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
          applyError(event.error, '判断没有成功，请重试')
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
          setError('')
          setLimit(null)
          setRejectedMsg('')
          freshStartRef.current = true
          // 草稿同步换成新图：进行中的旧请求落定时就不会再盖回来
          purchaseSession.writeDraft({
            pending: false,
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
    if (purchaseSession.getInflight() || busy) return
    setBusy(true)
    setError('')
    setLimit(null)
    setRejectedMsg('')
    // 提到 try 外：catch 要用它判断「草稿是否已换图」
    let mediaId = ''
    try {
      if (demo) {
        const media = await qualityApi.createDemoMedia('body', `purchase-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPhotoMedia(media)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoMedia) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        if (photoPath) {
          const image = await readLocalImage(photoPath)
          mediaId = (await uploadMedia(mediaUpload, image, 'wardrobe')).id
        } else {
          mediaId = photoMedia!.asset_id
        }
      }
      purchaseSession.markPending({ photoPath: demo ? '' : photoPath, mediaId })
      const item = await purchaseSession.runDiagnose(mediaId, () =>
        peripherals.diagnose({ kind: 'purchase', media_id: mediaId }),
      )
      // 飞行中重选了图：旧结论已被会话拒收，这里也不上屏
      if (purchaseSession.read()?.result?.id === item.id) applySession(item)
    } catch (e) {
      // mediaId 为空说明上传/示例图就没走通，错误照常展示；非空但草稿已换图则静默
      if (mediaId && purchaseSession.read()?.mediaId !== mediaId) return
      applyError(e, '判断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!result) return
    try {
      await peripherals.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: PURCHASE_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  // 日限错误态的「查看上次结果」：历史进来就替换掉当前未完成的图片位
  const viewHistory = async () => {
    try {
      const latest = await peripherals.getLatestDiagnosis('purchase')
      if (!latest) return
      purchaseSession.writeDraft({ photoPath: '' })
      purchaseSession.markDone(latest)
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
    purchaseSession.clearResult()
    setResult(null)
    setError('')
    setLimit(null)
    setRejectedMsg('')
  }

  const shownMedia: DisplayMedia | null = photoMedia ?? result?.source_media ?? null
  // 工具新渲染层按 CORS 拦截 http://tmp/、http://usr/：渲染用换出值，
  // 状态与草稿里存原路径（上传、跨页恢复都靠它）
  const photoDisplayPath = useDisplayablePath(photoPath)
  // 结果态 hero 出血到屏幕顶并 fixed 钉住：导航区+呼吸缝只铺衬底，前景图
  // 上下让位；文档流由同高 spacer 补上，卡片从钉住的照片上滚过。
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
  // 锐图四边羽化：可见矩形（aspectFit letterbox）用 photoDims 实测，
  // mask 渐变精确压在四条边上，溶进底下的对焦模糊层（横竖图都无硬边）
  const FEATHER = 44
  const stageWpx = nav.windowWidth
  // hero 总高加了停靠带，stage 上下各让 GAP/DOCK，照片区 = 820-56（DOCK 相消）
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
  const adviceTitle = result?.priority_title || result?.conclusion || PURCHASE_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')
  // 单据行：短单号 + 短日期（都是服务端真数据，票据感不是编的）
  const serialNo = result ? result.id.replace(/-/g, '').slice(0, 10).toUpperCase() : ''
  const serialDate = result ? result.created_at.slice(5, 10).replace('-', '.') : ''

  return (
    <View className={pageClass}>
      <AppHeader title="购买判断" back onPhoto={Boolean(result)} />
      {/* 空态/日限态一屏钉死：外壳 .page--lock 锁 100vh 纵排，容器 flex:1
          吃满导航以下的全部余量（高度不进 JS，安全区只由 CSS env 垫一次）。
          结果态解除锁走「钉屏照片 + 单据上滚」 */}
      <View className={`pk${result ? ' pk--done' : ''}`}>
        {/* 题头前置（选择态）：巨号问句开场，照片是「送检物」不是题图——
            票据叙事 = 题头 → 空托盘 → 须知 → 动作 */}
        {!result && !limit ? (
          <View className={`pk__masthead ${enter()}`}>
            <View className="pk__mast">
              <Text className="pk__mast-meta">{PURCHASE_COPY.mastheadMeta}</Text>
              <View className="pk__mast-dash" />
            </View>
            <Text className="pk__question">{PURCHASE_COPY.title}</Text>
            <Text className="pk__question-sub">{PURCHASE_COPY.desc}</Text>
          </View>
        ) : null}
        <View className={`pk__hero photo-hero photo-hero--bleed ${enter(1)}`} style={doneHeroStyle}>
          {shownMedia ? (
            <>
              <SourceImage className="pk__photo-backdrop" media={shownMedia} mode="aspectFill" />
              <View className="pk__photo-stage" style={photoStageStyle}>
                {/* 对焦模糊层 + 羽化锐图：照片是「标本层」，硬边语言全在下方单据上 */}
                <SourceImage className="pk__photo-blur" media={shownMedia} mode="aspectFit" />
                <SourceImage
                  className="pk__hero-img pk__hero-img--fit"
                  media={shownMedia}
                  mode="aspectFit"
                  onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
                />
              </View>
            </>
          ) : photoPath ? (
            <>
              <Image className="pk__photo-backdrop" src={photoDisplayPath} mode="aspectFill" />
              <View className="pk__photo-stage" style={photoStageStyle}>
                <Image className="pk__photo-blur" src={photoDisplayPath} mode="aspectFit" />
                <Image
                  className="pk__hero-img pk__hero-img--fit"
                  src={photoDisplayPath}
                  mode="aspectFit"
                  onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
                />
              </View>
            </>
          ) : (
            <View className="pk__upload pressable" onClick={choosePhoto}>
              <SourceImage className="pk__upload-backdrop" reference={{ slug: 'warm', variant: 'full' }} />
              <SourceImage
                className="pk__hero-img"
                reference={{ slug: 'warm', variant: 'full' }}
                anchor="top"
                frameAspect={HERO_ASPECT}
              />
              {/* 送检处：硬边取件口——墨描边 + 内侧虚线裁切线 + 骑框标签；
                  空态的主动作就是「放入检体」：居中大 ＋，整个托盘可点 */}
              <View className="pk__slot">
                <View className="pk__slot-frame" />
                <Text className="pk__slot-label">{PURCHASE_COPY.uploadSlotLabel}</Text>
                <View className="pk__slot-action">
                  <View className="pk__slot-plus-circle">
                    <Text className="pk__slot-plus">＋</Text>
                  </View>
                  <Text className="pk__slot-text">{PURCHASE_COPY.uploadTitle}</Text>
                </View>
              </View>
            </View>
          )}
          {shownMedia || photoPath ? (
            // 结果态导航透明不占位：chip 让到导航栏下方（真机测量 px，同穿搭诊断）
            <View className="pk__hero-alt-wrap" style={result ? { top: `${getNavMetrics().navHeight + 8}px` } : undefined}>
              <Text className="pk__hero-alt pressable" onClick={choosePhoto}>{PURCHASE_COPY.reselect}</Text>
            </View>
          ) : null}
          {/* 盖章：结果落在照片右上——双线框、苔绿印泥、微旋转 */}
          {result ? (
            <View className="pk__stamp" style={{ top: `${getNavMetrics().navHeight + 76 * rpxPx}px` }}>
              <Text className="pk__stamp-text">{PURCHASE_COPY.stampText}</Text>
              {serialDate ? <Text className="pk__stamp-date">{serialDate}</Text> : null}
            </View>
          ) : null}
          {busy ? (
            <View className="pk__mask">
              <View className="pk__mask-spin spinner" />
              <Text className="pk__mask-text">{PURCHASE_COPY.busyHint}</Text>
            </View>
          ) : null}
        </View>
        {/* hero 结果态 fixed 钉住，文档流同高占位：卡片从钉住的照片上滚过 */}
        {result ? <View style={doneHeroStyle} /> : null}

        {result ? (
          <>
            <View className={`pk__ticket ${enter(1)}`}>
              {/* 单据头：结论标签 + 单号（真数据） */}
              <View className="pk__mast">
                <Text className="pk__mast-meta">{PURCHASE_COPY.adviceLabel}</Text>
                <Text className="pk__mast-serial">
                  {PURCHASE_COPY.serialLabel} {serialNo}
                </Text>
              </View>

              {/* 巨号判断：黑体 800、紧字距——票据上的大字结论 */}
              <View className="pk__verdict">
                {adviceLead ? <Text className="pk__verdict-lead">{adviceLead}</Text> : null}
                <Text className="pk__verdict-title">{adviceAction}</Text>
                {adviceBody ? <Text className="pk__verdict-body">{adviceBody}</Text> : null}
              </View>

              {/* 双栏对峙：合适 vs 注意——硬边面板 + 大号计数 */}
              {keepFindings.length > 0 || liftFindings.length > 0 ? (
                <View className={`pk__versus ${keepFindings.length > 0 && liftFindings.length > 0 ? '' : 'pk__versus--single'}`}>
                  {keepFindings.length > 0 ? (
                    <View className="pk__panel pk__panel--keep">
                      <View className="pk__panel-head">
                        <Text className="pk__panel-count">{keepFindings.length}</Text>
                        <Text className="pk__panel-title">{PURCHASE_COPY.findingsKeep}</Text>
                      </View>
                      {keepFindings.map((finding) => (
                        <Text key={`${finding.category}-${finding.label}`} className="pk__panel-item">
                          {finding.label}
                        </Text>
                      ))}
                    </View>
                  ) : null}
                  {liftFindings.length > 0 ? (
                    <View className="pk__panel pk__panel--lift">
                      <View className="pk__panel-head">
                        <Text className="pk__panel-count">{liftFindings.length}</Text>
                        <Text className="pk__panel-title">{PURCHASE_COPY.findingsLift}</Text>
                      </View>
                      {liftFindings.map((finding) => (
                        <Text key={`${finding.category}-${finding.label}`} className="pk__panel-item">
                          {finding.label}
                        </Text>
                      ))}
                    </View>
                  ) : null}
                </View>
              ) : null}

              {/* 标签行：点线收据脚注 */}
              {result.tags.length > 0 ? (
                <View className="pk__tags">
                  {result.tags.map((tag) => (
                    <View key={tag} className="pk__tags-row">
                      <Text className="pk__tags-text">{tag}</Text>
                      <View className="pk__tags-dots" />
                    </View>
                  ))}
                </View>
              ) : null}

              {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
            </View>

            {/* 撕票孔 CTA：虚线撕边 + 双侧半圆缺口，固定底部（sheet 平级，不被 fade-up 的 transform 困住） */}
            <View className="pk__cta">
              <View className="pk__cta-notch pk__cta-notch--l" />
              <View className="pk__cta-notch pk__cta-notch--r" />
              <View
                className={`pk__go pressable ${busy ? 'pk__go--busy' : ''}`}
                onClick={() => void save()}
              >
                <Text className="pk__go-text">{PURCHASE_COPY.save}</Text>
              </View>
              <Text className="pk__cta-alt pressable" onClick={retryFresh}>{PURCHASE_COPY.again}</Text>
            </View>
          </>
        ) : limit ? (
          <>
            <View className={`pk__hint ${enter(1)}`}>
              <Text className="pk__hint-title">{limit.title}</Text>
              {limit.body ? <Text className="pk__hint-desc">{limit.body}</Text> : null}
            </View>
            <View className={`pk__foot ${enter(2)}`}>
              <View className="pk__go pk__go--block pressable" onClick={() => void viewHistory()}>
                <Text className="pk__go-text">{PURCHASE_COPY.lastResult}</Text>
              </View>
            </View>
          </>
        ) : (
          <>
            {!shownMedia && !photoPath ? (
              <View className={`pk__notice ${enter(1)}`}>
                <Text className="pk__notice-label">{PURCHASE_COPY.tipsLabel}</Text>
                {PURCHASE_COPY.uploadTips.map((tip, index) => (
                  <View key={tip} className="pk__notice-row">
                    <Text className="pk__notice-text">{tip}</Text>
                    <View className="pk__notice-dots" />
                    <Text className="pk__notice-no">{String(index + 1).padStart(2, '0')}</Text>
                  </View>
                ))}
              </View>
            ) : null}
            {rejectedMsg ? (
              <View className={`pk__rejected ${enter(1)}`}>
                <Text className="pk__rejected-text">{rejectedMsg}</Text>
              </View>
            ) : error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
            <View className={`pk__foot ${enter(2)}`}>
              <View
                className={`pk__go pk__go--block pressable ${busy ? 'pk__go--busy' : ''} ${
                  !rejectedMsg && !photoPath && !photoMedia ? 'pk__go--disabled' : ''
                }`}
                onClick={() => (rejectedMsg ? choosePhoto() : void analyze(false))}
              >
                {busy ? <View className="pk__go-spin spinner spinner--on-deep" /> : null}
                <Text className="pk__go-text">
                  {rejectedMsg ? '重新选择照片' : busy ? PURCHASE_COPY.busy : PURCHASE_COPY.start}
                </Text>
              </View>
              <View className="pk__foot-row">
                <Text className="pk__foot-alt pressable" onClick={() => void analyze(true)}>{PURCHASE_COPY.demo}</Text>
              </View>
            </View>
          </>
        )}
      </View>
    </View>
  )
}
