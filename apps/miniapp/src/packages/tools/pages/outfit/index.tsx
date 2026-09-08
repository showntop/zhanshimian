// 穿搭诊断：场景 3 选 + 单图上传 + 同步诊断（锚点标注）+ 最值得先改的一处。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { userImage, type Diagnosis } from '@zsm/core'
import { api } from '../../../../services/api'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import Pill from '../../../../components/pill'
import ErrorState from '../../../../components/error-state'
import './index.scss'

const CONTEXTS = [
  { key: 'daily', label: '日常' },
  { key: 'interview', label: '面试' },
  { key: 'date', label: '约会' },
] as const

export default function Outfit() {
  const [scene, setScene] = useState<string>('daily')
  const [photoPath, setPhotoPath] = useState('')
  const [photoUrl, setPhotoUrl] = useState('')
  const [demoSlug, setDemoSlug] = useState('')
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) {
          setPhotoPath(file.tempFilePath)
          setPhotoUrl('')
          setDemoSlug('')
          setResult(null)
        }
      },
    })
  }

  const analyze = async (demo = false) => {
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      if (demo) {
        mediaId = (await api.createDemoMedia('outfit')).id
        setDemoSlug('natural')
        setPhotoUrl('')
        setPhotoPath('')
      } else {
        if (!photoPath) {
          Taro.showToast({ title: '先上传一张全身照', icon: 'none' })
          return
        }
        const asset = await api.uploadMedia({ kind: 'outfit', filePath: photoPath })
        mediaId = asset.id
        setPhotoUrl(asset.url)
      }
      const item = await api.diagnose({ kind: 'outfit', media_id: mediaId, scene, report_id: readReportId() })
      setResult(item)
    } catch (e) {
      setError((e as Error).message || '诊断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const readReportId = () => {
    try {
      return Taro.getStorageSync('zsm_report_id') || undefined
    } catch {
      return undefined
    }
  }

  const saveResult = async () => {
    if (!result) return
    try {
      await api.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: '已保存', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const toPlans = () => Taro.switchTab({ url: '/pages/plans/index' })

  const isDemo = Boolean(demoSlug) || (result?.provider_version ?? '').startsWith('demo')
  const shownUrl = userImage(photoUrl) || photoPath

  return (
    <View className="page">
      <AppHeader title="穿搭诊断" back />
      <View className="od">
        <View className="od__hero fade-up">
          {demoSlug ? (
            <ExampleImage className="od__hero-img" slug="natural" variant="full" badgeText="效果示例" />
          ) : shownUrl ? (
            <Image className="od__hero-img" src={shownUrl} mode="aspectFill" />
          ) : (
            <View className="od__upload pressable" onClick={choosePhoto}>
              <Text className="od__upload-plus">＋</Text>
              <Text className="od__upload-hint">上传一张全身照</Text>
            </View>
          )}
          {shownUrl || demoSlug ? (
            <View className="od__hero-actions">
              <Text className="od__hero-alt pressable" onClick={choosePhoto}>重选照片</Text>
            </View>
          ) : null}
          {/* 诊断标注：锚点归位 */}
          {result?.findings?.slice(0, 3).map((finding) =>
            finding.anchor_x != null && finding.anchor_y != null ? (
              <View
                key={`${finding.category}-${finding.label}`}
                className="od__marker"
                style={{
                  left: `${Math.min(90, Math.max(8, finding.anchor_x * 100))}%`,
                  top: `${Math.min(88, Math.max(8, finding.anchor_y * 100))}%`,
                }}
              >
                <View className="od__marker-dot" />
                <Text className="od__marker-chip">{finding.label}</Text>
              </View>
            ) : null,
          )}
          {isDemo ? (
            <View className="od__badge">
              <Text>效果示例</Text>
            </View>
          ) : null}
        </View>

        <View className="od__context fade-up delay-1">
          <Text className="od__context-label">诊断场景</Text>
          <View className="od__context-pills">
            {CONTEXTS.map((c) => (
              <Pill key={c.key} label={c.label} active={scene === c.key} onClick={() => setScene(c.key)} />
            ))}
          </View>
        </View>

        {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}

        {result ? (
          <View className="od__result fade-up delay-2">
            <Text className="od__result-title">{result.priority_title}</Text>
            <Text className="od__result-copy">{result.priority_copy}</Text>
            <View className="od__tags">
              {result.tags.map((tag) => (
                <Text key={tag} className="od__tag">{tag}</Text>
              ))}
            </View>
            <View className="od__result-actions">
              <PrimaryButton text="保存这条建议" onClick={saveResult} />
              <Text className="od__result-alt pressable" onClick={toPlans}>去看三套方案</Text>
            </View>
          </View>
        ) : null}

        {!result ? (
          <View className="od__foot fade-up delay-2">
            <PrimaryButton text={busy ? '正在诊断…' : '开始诊断'} loading={busy} disabled={!photoPath} onClick={() => analyze(false)} />
            <View className="od__foot-row">
              <Text className="od__foot-alt pressable" onClick={choosePhoto}>选择照片</Text>
              <Text className="od__foot-alt pressable" onClick={() => analyze(true)}>用示例照片体验</Text>
            </View>
            <Text className="od__foot-note">只指出最值得调整的一处</Text>
          </View>
        ) : null}
      </View>
    </View>
  )
}
