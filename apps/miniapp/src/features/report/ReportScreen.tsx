// 形象报告页：每一条建议都挂在它自己声明的来源照片上。
//
// 与旧报告页的差别：
// 1. 不再从 Storage 读 reportId，也不再写 reportId——路由上的 id 是唯一入口，
//    没有 id 就问服务端「当前报告」；
// 2. 不渲染置信度、严重度、生成器版本之类的内部评估字段——契约里没有的字段连读都不读；
// 3. 证据落不了地的 finding 仍然在文字列表里（文字不依赖照片），
//    但不会出现在照片上指着一个不存在的位置；
// 4. 「查看方案」走 createPlanSet + 内存交接条（方案页在 tabBar 里，switchTab 不带 query）。
//
// 视觉沿用 09-11 旧线：全出血 Swiper hero + 骑缝胶片条 + 上叠内容板 + 固定底部 CTA。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Swiper, SwiperItem, Text, View } from '@tarojs/components'
import {
  CAPTURE_COPY,
  ERROR_COPY,
  REPORT_COPY,
  reportCategoryLabel,
} from '@zsm/core'
import type { ReportFinding, Report as ReportType } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { writePlanSetHandoff } from '../../app/plan-set-handoff'
import { handleBillingError } from '../../services/billing'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import PhotoAnnotationLayer from '../../components/photo-annotation'
import PrimaryButton from '../../components/primary-button'
import Skeleton from '../../components/skeleton'
import SourceImage from '../../components/source-image'
import {
  defaultReportRole,
  findingAnchorPoint,
  reportAvailableRoles,
  reportFindings,
  reportFindingsOnRole,
  reportPhotoForFinding,
  reportPlanSetHandoff,
  reportPlanSetIdempotencyKey,
  reportPlanSetRequest,
  reportSourcePhoto,
  type ReportRole,
} from './model'
import './index.scss'

const CAPTURE_ROUTE = '/pages/capture/index'
const PLANS_ROUTE = '/pages/plans/index'

// 照片按宽铺满、顶部锚定（全身照保留头部，多出的从底部裁）。
// 切换来源照片时只有相框内的图片滑动。hero 高度约 72% 视口：
// 相框高只在这里算一次，标注层 frameW/frameH 与实际渲染框是同一个值。
const HERO_FULL_W = 750
const HERO_H = (() => {
  let windowWidth = 375
  let windowHeight = 667
  try {
    const win = Taro.getWindowInfo()
    windowWidth = win.windowWidth
    windowHeight = win.windowHeight
  } catch {
    /* 开发环境兜底 */
  }
  return Math.round((windowHeight * 0.72 * HERO_FULL_W) / (windowWidth || 375))
})()
const HERO_ASPECT = HERO_FULL_W / HERO_H

interface ReportScreenProps {
  /** 路由上的报告 id；没有就取「当前报告」 */
  reportId?: string
  /** 内容首次上屏（成功/失败/空态都算）时通知外壳，驱动入场动画的 ready 时序 */
  onReady?: () => void
  /** 外壳 usePageShell 的 enter：入场播完返回空串，避免微信重播挂在节点上的 CSS animation */
  enter?: (delay?: 1 | 2 | 3) => string
}

const staticEnter = (delay?: 1 | 2 | 3) => (delay ? `fade-up delay-${delay}` : 'fade-up')

export default function ReportScreen({ reportId, onReady, enter = staticEnter }: ReportScreenProps) {
  // 有路由 id 时先吃缓存立刻上屏，再向服务端对一次账
  const [report, setReport] = useState<ReportType | null>(() =>
    reportId ? resourceCache.read<ReportType>(resourceKey('report', reportId)) ?? null : null,
  )
  const [loading, setLoading] = useState(!report)
  const [failed, setFailed] = useState(false)
  const [activeRole, setActiveRole] = useState<ReportRole | null>(null)
  const [activeFindingId, setActiveFindingId] = useState<string | null>(null)
  const [photoDims, setPhotoDims] = useState<Partial<Record<ReportRole, { w: number; h: number }>>>({})
  const [planning, setPlanning] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setFailed(false)
    try {
      const result = reportId
        ? await qualityApi.getReport(reportId)
        : await qualityApi.getCurrentReport()
      if (result) resourceCache.write(resourceKey('report', result.id), result)
      setReport(result)
      setActiveRole(defaultReportRole(result))
      setActiveFindingId(null)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [reportId])

  useEffect(() => {
    void load()
  }, [load])

  // ready 时序：内容（或失败/空态）首次到达才点亮。外壳恒 ready 会让
  // page--settled 提前钉住，数据晚到时 fade-up 被 animation:none 压掉，
  // 入场动画形同虚设——这里只发一次，重绘/对账不重复触发。
  const arrived = Boolean(report) || failed || !loading
  const readyFiredRef = useRef(false)
  useEffect(() => {
    if (!arrived || readyFiredRef.current) return
    readyFiredRef.current = true
    onReady?.()
  }, [arrived, onReady])

  const pickRole = (role: ReportRole) => {
    setActiveRole(role)
    // 换照片时把跟着旧照片选中的建议一并收起：它不属于这一格
    setActiveFindingId(null)
  }

  const pickFinding = (finding: ReportFinding) => {
    setActiveFindingId((prev) => (prev === finding.id ? null : finding.id))
    if (reportPhotoForFinding(report, finding) !== null) setActiveRole(finding.source_photo.role)
  }

  /** 「查看方案」：受理 → 交接条 → 方案 tab。幂等键绑报告 id，重复点击复用同一份方案集。 */
  const viewPlans = async () => {
    if (!report || planning) return
    setPlanning(true)
    try {
      const start = await qualityApi.createPlanSet(
        reportPlanSetRequest(report.id),
        reportPlanSetIdempotencyKey(report.id),
      )
      writePlanSetHandoff(reportPlanSetHandoff(start))
      await Taro.switchTab({ url: PLANS_ROUTE })
    } catch (error) {
      // 402/429 等计费错误先走购买引导（弹层→标记→profile 购买层），其余才落通用提示
      if (handleBillingError(error)) return
      const message = error instanceof PublicApiError && error.message ? error.message : REPORT_COPY.planFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setPlanning(false)
    }
  }

  if (failed) {
    return (
      <ErrorState
        title={REPORT_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => void load()}
      />
    )
  }

  if (!report) {
    if (loading) {
      return (
        <View className="report-screen">
          <Skeleton rows={5} />
        </View>
      )
    }
    return (
      <EmptyState
        title={REPORT_COPY.noReportTitle}
        description={REPORT_COPY.noReportBody}
        actionText={REPORT_COPY.noReportAction}
        onAction={() => void Taro.redirectTo({ url: CAPTURE_ROUTE })}
      />
    )
  }

  const findings = reportFindings(report)
  // 胶片条与 hero 只出现有照片的角色：空格可点但没有内容是死交互
  const photoRoles = reportAvailableRoles(report)
  const currentRole: ReportRole | null =
    activeRole && photoRoles.includes(activeRole) ? activeRole : photoRoles[0] ?? null
  // 当前照片名下的建议按 finding 自己声明的角色归位；证据落不了地的留在文字列表。
  // 一张照片都没有时文字列表兜底全量——报告宁可有缺口，不可有假的对应，但文字不丢。
  const roleFindings = currentRole
    ? findings.filter((finding) => finding.source_photo.role === currentRole)
    : findings

  const toAnnotationItem = (finding: ReportFinding) => ({
    id: finding.id,
    label: finding.label,
    categoryLabel: reportCategoryLabel(finding.category),
    detail: finding.visible_observation,
    // 锚点：report.v2 是 AI 直接给的语义关键点；v1 是区域矩形，取语义边
    // （见 model.findingAnchorPoint——客户端不拼装服务端没给的东西）
    ...findingAnchorPoint(finding, report.schema_version),
  })

  return (
    <View className="report-screen">
      {/* ---------- 来源照片：这张照片是所有锚点的坐标系 ---------- */}
      <View className={`report-screen__hero ${enter()}`}>
        {photoRoles.length === 0 ? (
          <View className="report-screen__hero-empty" style={{ height: `${HERO_H}rpx` }}>
            <Text className="report-screen__hero-empty-title">{REPORT_COPY.evidenceEmpty}</Text>
            <Text className="report-screen__hero-empty-hint">{REPORT_COPY.evidenceEmptyHint}</Text>
          </View>
        ) : (
          <Swiper
            className="report-screen__swiper"
            style={{ height: `${HERO_H}rpx` }}
            current={Math.max(0, photoRoles.indexOf(currentRole ?? 'body'))}
            onChange={(event) => {
              const role = photoRoles[event.detail.current]
              if (role) pickRole(role)
            }}
          >
            {photoRoles.map((role) => {
              const annotated = reportFindingsOnRole(report, role)
              return (
                <SwiperItem key={role} className="report-screen__slide">
                  <View className="report-screen__hero-frame">
                    <SourceImage
                      className="report-screen__hero-img"
                      media={reportSourcePhoto(report, role)}
                      anchor="top"
                      frameAspect={HERO_ASPECT}
                      onLoad={(event) => {
                        const w = Number(event.detail.width)
                        const h = Number(event.detail.height)
                        if (w > 0 && h > 0) {
                          setPhotoDims((prev) =>
                            prev[role]?.w === w && prev[role]?.h === h
                              ? prev
                              : { ...prev, [role]: { w, h } },
                          )
                        }
                      }}
                    />
                    {annotated.length > 0 ? (
                      <PhotoAnnotationLayer
                        items={annotated.map(toAnnotationItem)}
                        activeId={role === currentRole ? activeFindingId ?? '' : ''}
                        frameW={HERO_FULL_W}
                        frameH={HERO_H}
                        photoDims={photoDims[role]}
                        objectPosition="top"
                        onTap={(item) => {
                          const finding = annotated.find((entry) => entry.id === item.id)
                          if (finding) pickFinding(finding)
                        }}
                      />
                    ) : null}
                  </View>
                </SwiperItem>
              )
            })}
          </Swiper>
        )}
      </View>

      {/* 内容板：向上叠住照片底边，胶片条骑跨接缝作为照片与报告的铰链 */}
      <View className={`report-screen__sheet ${enter(1)}`}>
        {photoRoles.length > 1 ? (
          <View className="report-screen__film">
            {photoRoles.map((role) => {
              const active = role === currentRole
              return (
                <View
                  key={role}
                  className={[
                    'report-screen__film-item pressable',
                    active ? 'report-screen__film-item--active' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  onClick={() => pickRole(role)}
                >
                  <View className="report-screen__film-thumb">
                    <SourceImage
                      className="report-screen__film-img"
                      media={reportSourcePhoto(report, role)}
                      anchor="top"
                    />
                  </View>
                  <Text className="report-screen__film-label">
                    {CAPTURE_COPY.shots[role].label}
                  </Text>
                </View>
              )
            })}
          </View>
        ) : null}

        {/* ---------- 综合印象与综合建议 ---------- */}
        {report.impression_tags.length > 0 ? (
          <View className="report-screen__summary-row">
            <Text className="report-screen__summary-label">{REPORT_COPY.tagsTitle}</Text>
            <View className="report-screen__summary-chips">
              {report.impression_tags.slice(0, 3).map((tag) => (
                <Text key={tag} className="report-screen__chip">
                  {tag}
                </Text>
              ))}
            </View>
          </View>
        ) : null}

        <View className="report-screen__priority">
          <Text className="report-screen__priority-copy">
            {report.priority_title ? (
              <Text className="report-screen__priority-lead">{report.priority_title}</Text>
            ) : null}
            {report.priority_copy}
          </Text>
        </View>

        {/* ---------- 可提升点：文字永远完整；照片标注只是它的一个视图 ---------- */}
        <View className="report-screen__section">
          <View className="report-screen__section-head">
            <Text className="section-title">
              {currentRole
                ? `${CAPTURE_COPY.shots[currentRole].label} · ${roleFindings.length} 个${REPORT_COPY.findingsTitle}`
                : REPORT_COPY.findingsTitle}
            </Text>
            <View className="section-rule" />
          </View>
          {roleFindings.length > 0 ? (
            roleFindings.map((finding, index) => {
              const active = finding.id === activeFindingId
              return (
                <View
                  key={finding.id}
                  id={`finding-${finding.id}`}
                  className={[
                    'report-screen__finding pressable',
                    active ? 'report-screen__finding--active' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  onClick={() => pickFinding(finding)}
                >
                  <View className="report-screen__finding-head">
                    <View className="report-screen__finding-title">
                      <Text className="report-screen__finding-index">{index + 1}</Text>
                      <Text className="report-screen__finding-cat">
                        {reportCategoryLabel(finding.category)}
                      </Text>
                    </View>
                  </View>
                  <Text className="report-screen__finding-label">{finding.label}</Text>
                  <Text className="report-screen__finding-detail">
                    {finding.visible_observation || finding.label}
                  </Text>
                  {finding.recommendation ? (
                    <Text className="report-screen__finding-detail">{finding.recommendation}</Text>
                  ) : null}
                </View>
              )
            })
          ) : (
            <View className="report-screen__empty-findings">
              <Text>{REPORT_COPY.emptyFindings}</Text>
            </View>
          )}
        </View>
      </View>

      {/* ---------- 下一步 ---------- */}
      <View className={`report-screen__cta ${enter(2)}`}>
        <PrimaryButton
          text={REPORT_COPY.viewPlans}
          loading={planning}
          onClick={() => void viewPlans()}
        />
        <Text className="report-screen__cta-note">{REPORT_COPY.viewPlansNote}</Text>
      </View>
    </View>
  )
}
