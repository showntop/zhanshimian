// 形象报告：来源照片可切换，findings 按真实 photo 归位；不展示评分，只给可提升点。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Swiper, SwiperItem, Text, View } from '@tarojs/components'
import {
  REPORT_COPY,
  isBundledAsset,
  lookImage,
  userImage,
  type Finding,
  type Report,
} from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }
const PHOTO_ORDER: Array<'body' | 'face' | 'side'> = ['body', 'face', 'side']
const PHOTO_LABEL: Record<string, string> = { face: '正脸', side: '侧脸', body: '全身' }

type PhotoKind = (typeof PHOTO_ORDER)[number]

// hero 全出血宽度（rpx，designWidth 750）。照片区限高 ~54% 视口：
// 照片按 aspectFit 完整放入，内容板叠上来，第一屏同时露出照片锚点与报告内容。
const HERO_FULL_W = 750
const { windowWidth = 375, windowHeight = 667 } = Taro.getSystemInfoSync()
const HERO_CAP = Math.round((windowHeight * 0.54 * HERO_FULL_W) / (windowWidth || 375))
const HERO_DEFAULT_H = Math.min(880, HERO_CAP)

function findingPhoto(finding: Finding): PhotoKind {
  return finding.photo === 'face' || finding.photo === 'side' ? finding.photo : 'body'
}

export default function Report() {
  const [report, setReport] = useState<Report | null>(null)
  const [photoMap, setPhotoMap] = useState<Record<string, string>>({})
  const [photoDemoMap, setPhotoDemoMap] = useState<Record<string, boolean>>({})
  const [activePhoto, setActivePhoto] = useState<PhotoKind>('body')
  const [activeFindingId, setActiveFindingId] = useState('')
  // 各来源照片的真实宽高（onLoad 采集），用于 aspectFit 可视区锚点换算
  const [photoDims, setPhotoDims] = useState<Record<string, { w: number; h: number }>>({})
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [plansBusy, setPlansBusy] = useState(false)
  const [shownRef, setShownRef] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setFailed(false)
    try {
      let id = readStorage(STORAGE_KEYS.reportId)
      if (!id) {
        const current = await api.getCurrentReport()
        if (!current) {
          setReport(null)
          setPhotoMap({})
          setPhotoDemoMap({})
          return
        }
        id = current.id
      }
      const item = await api.getReport(id)
      setReport(item)
      writeStorage(STORAGE_KEYS.reportId, item.id)
      // 三张来源照片：findings.photo 归位，缺图时保持可见空态。
      const analysis = await api.getAnalysis(item.analysis_id)
      const map: Record<string, string> = {}
      const demoMap: Record<string, boolean> = {}
      analysis.media?.forEach((media) => {
        const demo = media.demo === true || isBundledAsset(media.url)
        const url = userImage(media.url) || (demo ? lookImage(media.url) : '')
        if (url) {
          map[media.kind] = url
          demoMap[media.kind] = demo
        }
      })
      setPhotoMap(map)
      setPhotoDemoMap(demoMap)
      const preferred = PHOTO_ORDER.find((kind) => map[kind]) ?? 'body'
      setActivePhoto(preferred)
      setActiveFindingId('')
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load().then(() => setShownRef(true))
  }, [load])

  useDidShow(() => {
    // 首次加载由 useEffect 负责；之后回到本页才静默刷新。
    if (shownRef) load()
  })

  const viewPlans = async () => {
    if (!report || plansBusy) return
    setPlansBusy(true)
    try {
      // 方案组幂等创建（PUT）：成功后再切 Tab，避免用户进入空方案页。
      await api.upsertPlans(report.id, { scene: 'general', answers: {} })
      Taro.switchTab({ url: '/pages/plans/index' })
    } catch {
      Taro.showToast({ title: '方案暂时没有生成，请稍后重试', icon: 'none' })
    } finally {
      setPlansBusy(false)
    }
  }

  if (loading) {
    return (
      <View className="page">
        <AppHeader title={REPORT_COPY.title} back />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed || !report) {
    return (
      <View className="page">
        <AppHeader title={REPORT_COPY.title} back />
        <ErrorState
          title={failed ? undefined : REPORT_COPY.noReportTitle}
          message={failed ? undefined : REPORT_COPY.noReportBody}
          retryText={failed ? '重试' : REPORT_COPY.goArchive}
          onRetry={() => {
            if (failed) load()
            else Taro.redirectTo({ url: '/pages/capture/index' })
          }}
        />
      </View>
    )
  }

  const findings = report.findings ?? []
  const fallbackBodyPhoto = userImage(report.current_image_url) || (isBundledAsset(report.current_image_url) ? lookImage(report.current_image_url) : '')
  const photoTabs = PHOTO_ORDER.filter((kind) => photoMap[kind] || (kind === 'body' && fallbackBodyPhoto))
  const currentPhotoKind: PhotoKind = photoTabs.includes(activePhoto) ? activePhoto : (photoTabs[0] ?? 'body')
  // 锚点 ⇄ 卡片联动：点锚点展开标签并滚动定位到对应卡片；再点一次收起。
  const tapAnchor = (finding: Finding) => {
    if (activeFindingId === finding.id) {
      setActiveFindingId('')
      return
    }
    setActiveFindingId(finding.id)
    Taro.pageScrollTo({ selector: `#finding-${finding.id}`, duration: 300 })
  }

  const providerIsDemo = (report.provider_version ?? '').startsWith('demo')
  // 每张照片的 URL 与示例身份：hero 逐张判定（不同照片可能身份不同），缩略图复用
  const photoUrlFor = (kind: PhotoKind): string =>
    photoMap[kind] || (kind === 'body' ? fallbackBodyPhoto : '')
  const photoIsDemo = (kind: PhotoKind, url: string): boolean =>
    photoDemoMap[kind] === true ||
    isBundledAsset(url) ||
    (kind === 'body' && url === fallbackBodyPhoto && providerIsDemo)
  const visibleFindings = findings.filter((finding) => findingPhoto(finding) === currentPhotoKind)

  // 相框高度 = min(照片铺满全宽的自然高, 视口上限)。超过上限的超高照片按高度
  // aspectFit，两侧出现氛围模糊衬底；锚点坐标相对照片本身，要换算进框内位置。
  const frameHeightFor = (kind: PhotoKind): number => {
    const dims = photoDims[kind]
    if (!dims) return HERO_DEFAULT_H
    return Math.min(HERO_CAP, Math.round((HERO_FULL_W * dims.h) / dims.w))
  }

  const anchorStyle = (kind: PhotoKind, finding: Finding) => {
    const ax = finding.anchor_x ?? 0.5
    const ay = finding.anchor_y ?? 0.5
    const dims = photoDims[kind]
    if (!dims) {
      return {
        left: `${Math.min(91, Math.max(9, ax * 100))}%`,
        top: `${Math.min(89, Math.max(11, ay * 100))}%`,
      }
    }
    const photoH = (HERO_FULL_W * dims.h) / dims.w
    const frameH = Math.min(HERO_CAP, photoH)
    const scale = frameH / photoH
    const dispW = HERO_FULL_W * scale
    const offX = (HERO_FULL_W - dispW) / 2
    return {
      left: `${Math.min(93, Math.max(7, ((offX + ax * dispW) / HERO_FULL_W) * 100))}%`,
      top: `${Math.min(92, Math.max(8, ay * 100))}%`,
    }
  }

  return (
    <View className="page">
      <AppHeader title={REPORT_COPY.title} back />
      <View className="report">
        <View className="report__hero fade-up">
          <Swiper
            className="report__swiper"
            style={{ height: `${frameHeightFor(currentPhotoKind)}rpx` }}
            current={Math.max(0, photoTabs.indexOf(currentPhotoKind))}
            onChange={(e) => {
              const kind = photoTabs[e.detail.current]
              if (kind) {
                setActivePhoto(kind)
                setActiveFindingId('')
              }
            }}
          >
            {photoTabs.map((kind) => {
              const url = photoUrlFor(kind)
              const demo = photoIsDemo(kind, url)
              const kindFindings = findings.filter((finding) => findingPhoto(finding) === kind)
              return (
                <SwiperItem key={kind} className="report__slide">
                  <View className="report__hero-frame">
                    {/* 氛围模糊衬底：仅超高照片 aspectFit 时露出，收拢两侧视线 */}
                    <ExampleImage className="report__hero-bg" src={url} user={!demo} mode="aspectFill" />
                    <ExampleImage
                      className="report__hero-img"
                      src={url}
                      user={!demo}
                      mode="aspectFit"
                      badgeText={demo ? REPORT_COPY.demoMark : ''}
                      onLoad={(e) => {
                        const w = Number(e.detail.width)
                        const h = Number(e.detail.height)
                        if (!w || !h) return
                        setPhotoDims((prev) =>
                          prev[kind]?.w === w && prev[kind]?.h === h
                            ? prev
                            : { ...prev, [kind]: { w, h } }
                        )
                      }}
                    />
                    {/* 渐变压暗层：角标与低位锚点在任何照片上保持可读 */}
                    <View className="report__hero-scrim report__hero-scrim--top" />
                    <View className="report__hero-scrim report__hero-scrim--bottom" />
                    <Text className="report__hero-mark">
                      {REPORT_COPY.sourceTitle} · {PHOTO_LABEL[kind]}
                    </Text>
                    <Text className={providerIsDemo ? 'report__provider report__provider--demo' : 'report__provider'}>
                      {providerIsDemo ? REPORT_COPY.demoMark : REPORT_COPY.aiMark}
                    </Text>
                    {kindFindings.map((finding, index) => (
                      <View
                        key={finding.id}
                        className={`report__anchor ${activeFindingId === finding.id ? 'report__anchor--active' : ''}`}
                        style={anchorStyle(kind, finding)}
                        onClick={() => tapAnchor(finding)}
                      >
                        <Text className="report__anchor-index">{index + 1}</Text>
                        <Text className="report__anchor-chip">{finding.label}</Text>
                      </View>
                    ))}
                  </View>
                </SwiperItem>
              )
            })}
          </Swiper>

        </View>

        {/* 内容板：向上叠住照片底边，胶片条骑跨接缝作为照片与报告的铰链 */}
        <View className="report__sheet fade-up delay-1">
          {photoTabs.length > 1 ? (
            <View className="report__film">
              {photoTabs.map((kind) => {
                const url = photoUrlFor(kind)
                const active = kind === currentPhotoKind
                return (
                  <View
                    key={kind}
                    className={`report__film-item pressable ${active ? 'report__film-item--active' : ''}`}
                    onClick={() => {
                      setActivePhoto(kind)
                      setActiveFindingId('')
                    }}
                  >
                    <View className="report__film-thumb">
                      <ExampleImage
                        className="report__film-img"
                        src={url}
                        user={!photoIsDemo(kind, url)}
                        mode="aspectFill"
                      />
                    </View>
                    <Text className="report__film-label">{PHOTO_LABEL[kind]}</Text>
                  </View>
                )
              })}
            </View>
          ) : null}

        {(report.impression_tags ?? []).length > 0 ? (
          <View className="report__section">
            <View className="report__section-head">
              <Text className="section-title">{REPORT_COPY.tagsTitle}</Text>
              <View className="section-rule" />
            </View>
            <View className="report__tags">
              {(report.impression_tags ?? []).map((tag) => (
                <Text key={tag} className="report__tag">
                  {tag}
                </Text>
              ))}
            </View>
          </View>
        ) : null}

        <View className="report__priority fade-up delay-2">
          <Text className="report__priority-label">{REPORT_COPY.priorityTitle}</Text>
          <Text className="report__priority-title">{report.priority_title}</Text>
          <Text className="report__priority-copy">{report.priority_copy}</Text>
        </View>

        <View className="report__section">
          <View className="report__section-head fade-up delay-2">
            <Text className="section-title">
              {REPORT_COPY.findingsTitle} · {findings.length}
            </Text>
            <View className="section-rule" />
          </View>
          {visibleFindings.length > 0 ? (
            visibleFindings.map((finding, index) => (
              <View
                key={finding.id}
                id={`finding-${finding.id}`}
                className={`report__finding fade-up pressable ${activeFindingId === finding.id ? 'report__finding--active' : ''}`}
                style={{ animationDelay: `${0.16 + Math.min(index, 5) * 0.08}s` }}
                onClick={() => setActiveFindingId(finding.id)}
              >
                <View className="report__finding-head">
                  <View className="report__finding-title">
                    <Text className="report__finding-index">{index + 1}</Text>
                    <Text className="report__finding-cat">
                      {CATEGORY_LABEL[finding.category] || finding.category}
                    </Text>
                  </View>
                </View>
                <Text className="report__finding-label">{finding.label}</Text>
                <Text className="report__finding-detail">{finding.detail || finding.label}</Text>
              </View>
            ))
          ) : (
            <View className="report__empty-findings">
              <Text>{REPORT_COPY.emptyFindings}</Text>
            </View>
          )}
        </View>
        </View>

        <View className="report__cta fade-up delay-3">
          <PrimaryButton text={REPORT_COPY.viewPlans} loading={plansBusy} onClick={viewPlans} />
          <Text className="report__cta-note">{REPORT_COPY.viewPlansNote}</Text>
        </View>
      </View>
    </View>
  )
}
