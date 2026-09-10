// 形象报告：来源照片可切换，findings 按真实 photo 归位；不展示评分，只给可提升点。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
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

function findingPhoto(finding: Finding): PhotoKind {
  return finding.photo === 'face' || finding.photo === 'side' ? finding.photo : 'body'
}

export default function Report() {
  const [report, setReport] = useState<Report | null>(null)
  const [photoMap, setPhotoMap] = useState<Record<string, string>>({})
  const [photoDemoMap, setPhotoDemoMap] = useState<Record<string, boolean>>({})
  const [activePhoto, setActivePhoto] = useState<PhotoKind>('body')
  const [activeFindingId, setActiveFindingId] = useState('')
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
  const currentPhotoUrl =
    photoMap[currentPhotoKind] || (currentPhotoKind === 'body' ? fallbackBodyPhoto : '')
  const providerIsDemo = (report.provider_version ?? '').startsWith('demo')
  const currentPhotoIsDemo =
    photoDemoMap[currentPhotoKind] === true ||
    isBundledAsset(currentPhotoUrl) ||
    (currentPhotoKind === 'body' && currentPhotoUrl === fallbackBodyPhoto && providerIsDemo)
  const visibleFindings = findings.filter((finding) => findingPhoto(finding) === currentPhotoKind)

  return (
    <View className="page">
      <AppHeader title={REPORT_COPY.title} back />
      <View className="report">
        <View className="report__hero fade-up">
          <View className="report__hero-frame">
            <ExampleImage
              className="report__hero-img"
              src={currentPhotoUrl}
              user={!currentPhotoIsDemo}
              mode="aspectFill"
              badgeText={currentPhotoIsDemo ? REPORT_COPY.demoMark : ''}
            />
            <Text className="report__hero-mark">
              {REPORT_COPY.sourceTitle} · {PHOTO_LABEL[currentPhotoKind]}
            </Text>
            <Text className={providerIsDemo ? 'report__provider report__provider--demo' : 'report__provider'}>
              {providerIsDemo ? REPORT_COPY.demoMark : REPORT_COPY.aiMark}
            </Text>
            {visibleFindings.map((finding, index) => (
              <View
                key={finding.id}
                className={`report__anchor ${activeFindingId === finding.id ? 'report__anchor--active' : ''}`}
                style={{
                  left: `${Math.min(91, Math.max(9, (finding.anchor_x ?? 0.5) * 100))}%`,
                  top: `${Math.min(89, Math.max(11, (finding.anchor_y ?? 0.5) * 100))}%`,
                }}
                onClick={() => setActiveFindingId(finding.id)}
              >
                <Text className="report__anchor-index">{index + 1}</Text>
                <Text className="report__anchor-chip">{finding.label}</Text>
              </View>
            ))}
          </View>

          {photoTabs.length > 1 ? (
            <View className="report__tabs">
              {photoTabs.map((kind) => (
                <Text
                  key={kind}
                  className={`report__tab ${kind === currentPhotoKind ? 'report__tab--active' : ''}`}
                  onClick={() => {
                    setActivePhoto(kind)
                    setActiveFindingId('')
                  }}
                >
                  {PHOTO_LABEL[kind]}
                </Text>
              ))}
            </View>
          ) : null}
        </View>

        {(report.impression_tags ?? []).length > 0 ? (
          <View className="report__section fade-up delay-1">
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

        <View className="report__section fade-up delay-2">
          <View className="report__section-head">
            <Text className="section-title">
              {REPORT_COPY.findingsTitle} · {findings.length}
            </Text>
            <View className="section-rule" />
          </View>
          {visibleFindings.length > 0 ? (
            visibleFindings.map((finding, index) => (
              <View
                key={finding.id}
                className={`report__finding ${activeFindingId === finding.id ? 'report__finding--active' : ''}`}
                onClick={() => setActiveFindingId(finding.id)}
              >
                <View className="report__finding-head">
                  <View className="report__finding-title">
                    <Text className="report__finding-index">{index + 1}</Text>
                    <Text className="report__finding-cat">
                      {CATEGORY_LABEL[finding.category] || finding.category}
                    </Text>
                  </View>
                  <Text className="report__finding-photo">{PHOTO_LABEL[findingPhoto(finding)]}</Text>
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

        <View className="report__foot fade-up delay-3">
          <PrimaryButton text={REPORT_COPY.viewPlans} loading={plansBusy} onClick={viewPlans} />
          <Text className="report__foot-note">{REPORT_COPY.viewPlansNote}</Text>
        </View>
      </View>
    </View>
  )
}
