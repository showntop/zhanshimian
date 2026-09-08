// 形象报告：当前形象标签 + 优先建议 + findings 标注归位到来源照片。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, ScrollView, Text, View } from '@tarojs/components'
import { userImage, type Plan, type Report } from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }
const PHOTO_LABEL: Record<string, string> = { face: '正脸照', side: '侧脸照', body: '全身照' }

export default function Report() {
  const [report, setReport] = useState<Report | null>(null)
  const [photoMap, setPhotoMap] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setFailed(false)
    try {
      let id = readStorage(STORAGE_KEYS.reportId)
      if (!id) {
        const current = await api.getCurrentReport()
        if (!current) {
          setReport(null)
          return
        }
        id = current.id
      }
      const item = await api.getReport(id)
      setReport(item)
      writeStorage(STORAGE_KEYS.reportId, item.id)
      // 三张来源照片：findings.photo 归位 + 对比图需要
      const analysis = await api.getAnalysis(item.analysis_id)
      const map: Record<string, string> = {}
      analysis.media?.forEach((m) => {
        const url = userImage(m.url)
        if (url) map[m.kind] = url
      })
      setPhotoMap(map)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useDidShow(() => {
    trackShow()
  })

  const trackShow = () => {
    // 首次渲染已加载，回到本页时静默刷新一次
    if (report) load()
  }

  const viewPlans = async () => {
    if (!report) return
    // 方案组幂等创建（PUT）：已有则直接查看
    Taro.navigateTo({ url: `/pages/plans/index?from=report` })
    try {
      await api.upsertPlans(report.id, { scene: 'general', answers: {} })
    } catch {
      /* 幂等创建失败不阻塞：方案页会再拉列表 */
    }
  }

  if (loading) {
    return (
      <View className="page">
        <AppHeader title="形象报告" back />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed || !report) {
    return (
      <View className="page">
        <AppHeader title="形象报告" back />
        <ErrorState
          message={failed ? undefined : '还没有形象档案，先拍三张照片开始分析。'}
          retryText={failed ? '重试' : '去建档'}
          onRetry={() =>
            failed ? load() : Taro.redirectTo({ url: '/pages/capture/index' })
          }
        />
      </View>
    )
  }

  const mainPhoto = photoMap.body || report.current_image_url
  const grouped = report.findings ?? []

  return (
    <View className="page">
      <AppHeader title="形象报告" back />
      <View className="report">
        <View className="report__hero fade-up">
          <View className="report__hero-frame">
            {mainPhoto ? <Image className="report__hero-img" src={mainPhoto} mode="aspectFill" /> : null}
            {grouped
              .filter((f) => !f.photo || f.photo === 'body')
              .slice(0, 3)
              .map((finding, i) => (
                <View
                  key={finding.id}
                  className={`report__anchor report__anchor--${i}`}
                  style={{ left: `${Math.min(92, Math.max(6, (finding.anchor_x ?? 0.5) * 100))}%`, top: `${Math.min(90, Math.max(6, (finding.anchor_y ?? 0.5) * 100))}%` }}
                >
                  <View className="report__anchor-dot" />
                  <Text className="report__anchor-chip">{finding.label}</Text>
                </View>
              ))}
            <Text className="report__hero-mark">当前形象</Text>
          </View>
        </View>

        <View className="report__tags fade-up delay-1">
          {(report.impression_tags ?? []).map((tag) => (
            <Text key={tag} className="report__tag">
              {tag}
            </Text>
          ))}
        </View>

        <View className="report__priority fade-up delay-2">
          <Text className="report__priority-title">{report.priority_title}</Text>
          <Text className="report__priority-copy">{report.priority_copy}</Text>
        </View>

        <View className="report__section fade-up delay-2">
          <Text className="section-title">可提升点</Text>
          {grouped.map((finding) => (
            <View key={finding.id} className="report__finding">
              <View className="report__finding-head">
                <Text className="report__finding-cat">{CATEGORY_LABEL[finding.category] || finding.category}</Text>
                {finding.photo ? (
                  <Text className="report__finding-photo">{PHOTO_LABEL[finding.photo] || finding.photo}</Text>
                ) : null}
              </View>
              <Text className="report__finding-detail">{finding.detail || finding.label}</Text>
            </View>
          ))}
        </View>

        <View className="report__foot fade-up delay-3">
          <PrimaryButton text="查看我的 3 套方案" onClick={viewPlans} />
          <Text className="report__foot-note">方案基于你的照片与现实条件生成</Text>
        </View>
      </View>
    </View>
  )
}
