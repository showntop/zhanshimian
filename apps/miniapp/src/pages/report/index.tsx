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

// hero 全出血宽度（rpx，designWidth 750）。相框固定高度 ~56% 视口，
// 照片按 aspectFill 铺满全宽（超出部分上下裁切），切换来源照片时
// 只有相框内的图片滑动，下方内容纹丝不动（不会像整页切换）。
const HERO_FULL_W = 750
const { windowWidth = 375, windowHeight = 667 } = Taro.getSystemInfoSync()
const HERO_H = Math.round((windowHeight * 0.56 * HERO_FULL_W) / (windowWidth || 375))

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

  // 锚点坐标相对原始照片，aspectFill 会被居中裁切：按缩放 + 裁切偏移
  // 重映射到固定相框的像素位置（用于引导线端点 + 标签定位）。
  // 返回 null 表示尚未拿到原图尺寸，回退到按原始比例直接映射。
  const anchorInFrame = (
    dims: { w: number; h: number } | undefined,
    ax: number,
    ay: number
  ): { x: number; y: number } => {
    if (!dims) return { x: ax * HERO_FULL_W, y: ay * HERO_H }
    const scale = Math.max(HERO_FULL_W / dims.w, HERO_H / dims.h)
    const scaledW = dims.w * scale
    const scaledH = dims.h * scale
    const offX = (HERO_FULL_W - scaledW) / 2
    const offY = (HERO_H - scaledH) / 2
    return { x: offX + ax * scaledW, y: offY + ay * scaledH }
  }

  // 同侧 finding 按 anchor_y 排序后错开，避免上下重叠
  const layoutSide = (sideFindings: Finding[]) => {
    const sorted = [...sideFindings].sort(
      (a, b) => (a.anchor_y ?? 0.5) - (b.anchor_y ?? 0.5)
    )
    const out: { finding: Finding; topPct: number }[] = []
    let last = 0
    for (const f of sorted) {
      const y = (f.anchor_y ?? 0.5) * 100
      let top = Math.min(86, Math.max(12, y))
      if (top - last < 16) top = Math.min(86, last + 16)
      out.push({ finding: f, topPct: top })
      last = top
    }
    return out
  }

  return (
    <View className="page">
      <AppHeader title={REPORT_COPY.title} back />
      <View className="report">
        <View className="report__hero fade-up">
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
              const leftLayout = layoutSide(kindFindings.filter((f) => (f.anchor_x ?? 0.5) < 0.5))
              const rightLayout = layoutSide(kindFindings.filter((f) => (f.anchor_x ?? 0.5) >= 0.5))
              const dims = photoDims[kind]
              // 标签几何：胶囊缩小到 180rpx，只显示标签一行（副标题在下方卡片），
              // 避免大面积遮挡人物；引导线起点加胶囊边距偏移。
              const CAP_W = 180
              const CAP_H = 56
              const EDGE = 16
              const renderTag = (item: { finding: Finding; topPct: number }, side: 'left' | 'right') => {
                const f = item.finding
                const ax = f.anchor_x ?? 0.5
                const ay = f.anchor_y ?? 0.5
                const a = anchorInFrame(dims, ax, ay)
                // 引导线起点：胶囊面向照片那一侧的中点（含边距偏移）
                const startX = side === 'left' ? EDGE + CAP_W : HERO_FULL_W - EDGE - CAP_W
                const centerY = (item.topPct / 100) * HERO_H + CAP_H / 2
                const dx = a.x - startX
                const dy = a.y - centerY
                const dist = Math.sqrt(dx * dx + dy * dy)
                const angle = (Math.atan2(dy, dx) * 180) / Math.PI
                const active = activeFindingId === f.id
                return (
                  <View key={f.id} className="report__annotation">
                    <View
                      className="report__leader"
                      style={{
                        left: `${(startX / HERO_FULL_W) * 100}%`,
                        top: `${(centerY / HERO_H) * 100}%`,
                        width: `${dist}rpx`,
                        transform: `rotate(${angle}deg)`,
                      }}
                    />
                    <View
                      className="report__anchor-dot"
                      style={{
                        left: `${(a.x / HERO_FULL_W) * 100}%`,
                        top: `${(a.y / HERO_H) * 100}%`,
                      }}
                    />
                    <View
                      className={`report__tag report__tag--${side} ${active ? 'report__tag--active' : ''}`}
                      style={{ top: `${item.topPct}%` }}
                      onClick={() => tapAnchor(f)}
                    >
                      <Text className="report__tag-label">{f.label}</Text>
                    </View>
                  </View>
                )
              }
              return (
                <SwiperItem key={kind} className="report__slide">
                  <View className="report__hero-frame">
                    {/* 氛围模糊衬底：极端比例照片加载瞬间的兜底底色 */}
                    <ExampleImage className="report__hero-bg" src={url} user={!demo} mode="aspectFill" />
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
                    {/* 渐变压暗层：保证低位标签在深色照片上仍可读 */}
                    <View className="report__hero-scrim report__hero-scrim--top" />
                    <View className="report__hero-scrim report__hero-scrim--bottom" />
                    <Text className="report__hero-mark">
                      {REPORT_COPY.sourceTitle} · {PHOTO_LABEL[kind]}
                    </Text>
                    <Text className={providerIsDemo ? 'report__provider report__provider--demo' : 'report__provider'}>
                      {providerIsDemo ? REPORT_COPY.demoMark : REPORT_COPY.aiMark}
                    </Text>
                    {/* 「最优优先级」绿色 tag 移到 hero 底部（胶片条上方），不再遮挡人脸 */}
                    {report.priority_title ? (
                      <View className="report__priority-tag">
                        <Text className="report__priority-tag-text">{REPORT_COPY.priorityBadge}</Text>
                      </View>
                    ) : null}
                    {/* 左右两列常显标签 + 引导线 + 锚点端点 */}
                    {leftLayout.map((item) => renderTag(item, 'left'))}
                    {rightLayout.map((item) => renderTag(item, 'right'))}
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
