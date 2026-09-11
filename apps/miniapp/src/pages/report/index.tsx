// 形象报告：来源照片可切换，findings 按真实 photo 归位；不展示评分，只给可提升点。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Swiper, SwiperItem, Text, View } from '@tarojs/components'
import {
  REPORT_COPY,
  isBundledAsset,
  lookImage,
  userImage,
  type Finding,
  type Report,
} from '@zsm/core'
import { usePageShell, useShowOnce } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import PhotoAnnotationLayer, { type AnnotationItem } from '../../components/photo-annotation'
import './index.scss'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }
const PHOTO_ORDER: Array<'body' | 'face' | 'side'> = ['body', 'face', 'side']
const PHOTO_LABEL: Record<string, string> = { face: '正脸', side: '侧脸', body: '全身' }

type PhotoKind = (typeof PHOTO_ORDER)[number]

// 照片按 aspectFill 铺满全宽（超出部分上下裁切），切换来源照片时
// 只有相框内的图片滑动。hero 高度约 72% 视口，给标注与人物留足空间。
const HERO_FULL_W = 750
const { windowWidth = 375, windowHeight = 667 } = Taro.getSystemInfoSync()
const HERO_H = Math.round((windowHeight * 0.72 * HERO_FULL_W) / (windowWidth || 375))

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
  const reportRef = useRef<Report | null>(null)
  reportRef.current = report
  const { pageClass, enter } = usePageShell(!loading || Boolean(report), '', 'report')

  const load = useCallback(async () => {
    const cached = Boolean(reportRef.current)
    if (!cached) {
      setLoading(true)
      setFailed(false)
    }
    try {
      let id = readStorage(STORAGE_KEYS.reportId)
      if (!id) {
        const current = await api.getCurrentReport()
        if (!current) {
          if (!cached) {
            setReport(null)
            setPhotoMap({})
            setPhotoDemoMap({})
          }
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
      if (!cached) {
        const preferred = PHOTO_ORDER.find((kind) => map[kind]) ?? 'body'
        setActivePhoto(preferred)
        setActiveFindingId('')
      }
    } catch {
      if (!reportRef.current) setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  // 首次由 useEffect 拉取；回页只后台校验，不清空已渲染照片
  useShowOnce(() => {
    load()
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
      <View className={pageClass}>
        <AppHeader title={REPORT_COPY.title} back />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed || !report) {
    return (
      <View className={pageClass}>
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
  // 锚点 ⇄ 详情条联动：点 tag 在 hero 内就地展开详情（不整页滚动，
  // 上下文不丢）；再点一次收起。activeFindingId 同时驱动 tag/卡片高亮。
  const tapAnchor = (finding: Finding) => {
    const nextKind = findingPhoto(finding)
    if (nextKind !== currentPhotoKind) setActivePhoto(nextKind)
    setActiveFindingId(activeFindingId === finding.id ? '' : finding.id)
  }

  const toAnnotation = (finding: Finding): AnnotationItem => ({
    id: finding.id,
    label: finding.label,
    detail: finding.detail || finding.label,
    category: finding.category,
    categoryLabel: CATEGORY_LABEL[finding.category] || finding.category,
    anchorX: finding.anchor_x ?? 0.5,
    anchorY: finding.anchor_y ?? 0.5,
  })

  const providerIsDemo = (report.provider_version ?? '').startsWith('demo')
  // 每张照片的 URL 与示例身份：hero 逐张判定（不同照片可能身份不同），缩略图复用
  const photoUrlFor = (kind: PhotoKind): string =>
    photoMap[kind] || (kind === 'body' ? fallbackBodyPhoto : '')
  const photoIsDemo = (kind: PhotoKind, url: string): boolean =>
    photoDemoMap[kind] === true ||
    isBundledAsset(url) ||
    (kind === 'body' && url === fallbackBodyPhoto && providerIsDemo)
  const visibleFindings = findings.filter((finding) => findingPhoto(finding) === currentPhotoKind)

  return (
    <View className={pageClass}>
      <AppHeader title={REPORT_COPY.title} back />
      <View className="report">
        <View className={`report__hero ${enter()}`}>
          <Swiper
            className="report__swiper"
            style={{ height: `${HERO_H}rpx` }}
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
                    <ExampleImage
                      className="report__hero-img"
                      src={url}
                      user={!demo}
                      mode="aspectFill"
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
                    {providerIsDemo ? (
                      <Text className="report__provider report__provider--demo">
                        {REPORT_COPY.demoMark}
                      </Text>
                    ) : null}
                    <PhotoAnnotationLayer
                      items={kindFindings.map(toAnnotation)}
                      activeId={kind === currentPhotoKind ? activeFindingId : ''}
                      frameW={HERO_FULL_W}
                      frameH={HERO_H}
                      photoDims={photoDims[kind]}
                      onTap={(item) => {
                        const found = kindFindings.find((f) => f.id === item.id)
                        if (found) tapAnchor(found)
                      }}
                    />
                  </View>
                </SwiperItem>
              )
            })}
          </Swiper>

        </View>

        {/* 内容板：向上叠住照片底边，胶片条骑跨接缝作为照片与报告的铰链 */}
        <View className={`report__sheet ${enter(1)}`}>
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
          <View className="report__summary-row">
            <Text className="report__summary-label">{REPORT_COPY.tagsTitle}</Text>
            <View className="report__summary-chips">
              {(report.impression_tags ?? []).slice(0, 3).map((tag) => (
                <Text key={tag} className="report__chip">
                  {tag}
                </Text>
              ))}
            </View>
          </View>
        ) : null}

        <View className="report__priority">
          <Text className="report__priority-copy">
            {report.priority_title ? (
              <Text className="report__priority-lead">{report.priority_title}</Text>
            ) : null}
            {report.priority_copy}
          </Text>
        </View>

        <View className="report__section">
          <View className="report__section-head">
            <Text className="section-title">
              {PHOTO_LABEL[currentPhotoKind]} · {visibleFindings.length} 个可提升点
            </Text>
            <View className="section-rule" />
          </View>
          {visibleFindings.length > 0 ? (
            visibleFindings.map((finding, index) => (
              <View
                key={finding.id}
                id={`finding-${finding.id}`}
                className={`report__finding pressable ${activeFindingId === finding.id ? 'report__finding--active' : ''}`}
                onClick={() => tapAnchor(finding)}
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

        <View className={`report__cta ${enter(2)}`}>
          <PrimaryButton text={REPORT_COPY.viewPlans} loading={plansBusy} onClick={viewPlans} />
          <Text className="report__cta-note">{REPORT_COPY.viewPlansNote}</Text>
        </View>
      </View>
    </View>
  )
}
