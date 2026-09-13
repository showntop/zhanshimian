// 形象报告页：每一条建议都挂在它自己声明的来源照片上。
//
// 与旧报告页的差别：
// 1. 不再从 Storage 读 reportId，也不再写 reportId——路由上的 id 是唯一入口，
//    没有 id 就问服务端「当前报告」；
// 2. 不渲染置信度、严重度、生成器版本之类的内部评估字段——契约里没有的字段连读都不读；
// 3. 证据落不了地的 finding 仍然在文字列表里（文字不依赖照片），
//    但不会出现在照片上指着一个不存在的位置；
// 4. 「查看方案」走 createPlanSet + 内存交接条（方案页在 tabBar 里，switchTab 不带 query）。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
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
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import PhotoAnnotationLayer from '../../components/photo-annotation'
import PrimaryButton from '../../components/primary-button'
import SourceImage from '../../components/source-image'
import {
  anchorBoxInFrame,
  defaultReportRole,
  reportFindings,
  reportFindingsOnRole,
  reportPhotoForFinding,
  reportPlanSetHandoff,
  reportPlanSetIdempotencyKey,
  reportPlanSetRequest,
  reportSourcePhoto,
  REPORT_ROLE_ORDER,
  type ReportRole,
} from './model'
import './index.scss'

const CAPTURE_ROUTE = '/pages/capture/index'
const PLANS_ROUTE = '/pages/plans/index'

/** 相框尺寸（rpx）：内容区满宽，高度按窗口高度换算，照片以顶对齐铺满。 */
function heroFrame(): { w: number; h: number; aspect: number } {
  let windowWidth = 375
  let windowHeight = 667
  try {
    const win = Taro.getWindowInfo()
    windowWidth = win.windowWidth
    windowHeight = win.windowHeight
  } catch {
    /* 开发环境兜底 */
  }
  const h = Math.round((windowHeight * 0.62 * 750) / windowWidth)
  return { w: 686, h, aspect: 686 / h }
}

interface ReportScreenProps {
  /** 路由上的报告 id；没有就取「当前报告」 */
  reportId?: string
}

export default function ReportScreen({ reportId }: ReportScreenProps) {
  const frame = heroFrame()
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

  const findings = reportFindings(report)
  const activeFinding = findings.find((finding) => finding.id === activeFindingId) ?? null

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
        <View className="report-screen report-screen--loading">
          <View className="spinner" />
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

  const annotated = activeRole ? reportFindingsOnRole(report, activeRole) : []
  const annotationItems = annotated.map((finding) => ({
    id: finding.id,
    label: finding.label,
    categoryLabel: reportCategoryLabel(finding.category),
    detail: finding.visible_observation,
    // 锚框中心：服务端给的是归一化矩形，标注层只认一个点
    anchorX: finding.anchor.x + finding.anchor.w / 2,
    anchorY: finding.anchor.y + finding.anchor.h / 2,
  }))
  const activeRoleMedia = activeRole ? reportSourcePhoto(report, activeRole) : null

  return (
    <View className="report-screen">
      {/* ---------- 来源照片：这张照片是所有锚点的坐标系 ---------- */}
      <View className="report-screen__hero fade-up">
        <View className="report-screen__hero-frame">
          {activeRoleMedia ? (
            <>
              <SourceImage
                className="report-screen__hero-img"
                media={activeRoleMedia}
                anchor="top"
                frameAspect={frame.aspect}
                onLoad={(event) => {
                  const w = Number(event.detail.width)
                  const h = Number(event.detail.height)
                  if (w > 0 && h > 0 && activeRole) {
                    setPhotoDims((prev) => ({ ...prev, [activeRole]: { w, h } }))
                  }
                }}
              />
              {annotationItems.length > 0 ? (
                <PhotoAnnotationLayer
                  items={annotationItems}
                  activeId={activeFindingId ?? ''}
                  frameW={frame.w}
                  frameH={frame.h}
                  photoDims={activeRole ? photoDims[activeRole] : undefined}
                  objectPosition="top"
                  onTap={(item) => setActiveFindingId((prev) => (prev === item.id ? null : item.id))}
                />
              ) : null}
            </>
          ) : (
            <View className="report-screen__hero-empty">
              <Text className="report-screen__hero-empty-title">{REPORT_COPY.evidenceEmpty}</Text>
              <Text className="report-screen__hero-empty-hint">{REPORT_COPY.evidenceEmptyHint}</Text>
            </View>
          )}
          {activeRole ? (
            <Text className="report-screen__hero-label">
              {CAPTURE_COPY.shots[activeRole].label}
            </Text>
          ) : null}
        </View>

        {/* 换片胶卷：三个角色固定出现，缺照片的是空格，不是别的照片 */}
        <View className="report-screen__film">
          {REPORT_ROLE_ORDER.map((role) => {
            const media = reportSourcePhoto(report, role)
            const isActive = activeRole === role
            return (
              <View
                key={role}
                className={[
                  'report-screen__film-item pressable',
                  isActive ? 'report-screen__film-item--active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => pickRole(role)}
              >
                {media ? (
                  <SourceImage className="report-screen__film-img" media={media} mode="aspectFill" />
                ) : (
                  <View className="report-screen__film-empty" />
                )}
                <Text className="report-screen__film-label">
                  {CAPTURE_COPY.shots[role].label}
                </Text>
              </View>
            )
          })}
        </View>
      </View>

      {/* ---------- 综合印象与综合建议 ---------- */}
      {report.impression_tags.length > 0 ? (
        <View className="report-screen__tags fade-up delay-1">
          <Text className="report-screen__section-title">{REPORT_COPY.tagsTitle}</Text>
          <View className="report-screen__tag-row">
            {report.impression_tags.map((tag) => (
              <Text key={tag} className="report-screen__tag">
                {tag}
              </Text>
            ))}
          </View>
        </View>
      ) : null}

      <View className="report-screen__priority fade-up delay-1">
        <Text className="report-screen__section-title">{REPORT_COPY.priorityTitle}</Text>
        <Text className="report-screen__priority-title">{report.priority_title}</Text>
        <Text className="report-screen__priority-copy">{report.priority_copy}</Text>
      </View>

      {/* ---------- 可提升点：文字永远完整；照片标注只是它的一个视图 ---------- */}
      <View className="report-screen__findings fade-up delay-2">
        <Text className="report-screen__section-title">{REPORT_COPY.findingsTitle}</Text>
        {findings.length === 0 ? (
          <Text className="report-screen__empty-findings">{REPORT_COPY.emptyFindings}</Text>
        ) : (
          findings.map((finding) => {
            const expanded = finding.id === activeFindingId
            return (
              <View
                key={finding.id}
                className={[
                  'report-screen__finding',
                  expanded ? 'report-screen__finding--active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => pickFinding(finding)}
              >
                <View className="report-screen__finding-head">
                  <Text className="report-screen__finding-cat">
                    {reportCategoryLabel(finding.category)}
                  </Text>
                  <Text className="report-screen__finding-label">{finding.label}</Text>
                </View>
                {expanded ? (
                  <View className="report-screen__finding-detail">
                    <Text className="report-screen__finding-line">
                      {`${REPORT_COPY.findingsObservationTitle}：${finding.visible_observation}`}
                    </Text>
                    <Text className="report-screen__finding-line">
                      {`${REPORT_COPY.findingsAdviceTitle}：${finding.recommendation}`}
                    </Text>
                  </View>
                ) : null}
              </View>
            )
          })
        )}
      </View>

      {/* ---------- 下一步 ---------- */}
      <View className="report-screen__cta fade-up delay-3">
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
