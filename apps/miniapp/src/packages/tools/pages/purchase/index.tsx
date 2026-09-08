// 购买判断：商品图上传 + 同步诊断（结论/注意点/衣橱搭配建议）+ 保存。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { userImage, type Diagnosis } from '@zsm/core'
import { api } from '../../../../services/api'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

export default function Purchase() {
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

  const readReportId = (): string | undefined => {
    try {
      return Taro.getStorageSync('zsm_report_id') || undefined
    } catch {
      return undefined
    }
  }

  const analyze = async (demo = false) => {
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      if (demo) {
        mediaId = (await api.createDemoMedia('product')).id
        setDemoSlug('warm')
        setPhotoUrl('')
        setPhotoPath('')
      } else {
        if (!photoPath) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        const asset = await api.uploadMedia({ kind: 'product', filePath: photoPath })
        mediaId = asset.id
        setPhotoUrl(asset.url)
      }
      const item = await api.diagnose({ kind: 'purchase', media_id: mediaId, report_id: readReportId() })
      setResult(item)
    } catch (e) {
      setError((e as Error).message || '判断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!result) return
    try {
      await api.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: '已保存', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const shownUrl = userImage(photoUrl) || photoPath
  const caution = result?.findings?.find((f) => f.tone === 'caution')
  const positive = result?.findings?.find((f) => f.tone === 'positive')

  return (
    <View className="page">
      <AppHeader title="购买判断" back />
      <View className="pk">
        <View className="pk__hero fade-up">
          {demoSlug ? (
            <ExampleImage className="pk__hero-img" slug="warm" variant="full" badgeText="效果示例" />
          ) : shownUrl ? (
            <Image className="pk__hero-img" src={shownUrl} mode="aspectFill" />
          ) : (
            <View className="pk__upload pressable" onClick={choosePhoto}>
              <Text className="pk__upload-plus">＋</Text>
              <Text className="pk__upload-hint">上传想买的单品图</Text>
            </View>
          )}
          {shownUrl || demoSlug ? (
            <View className="pk__hero-alt-wrap">
              <Text className="pk__hero-alt pressable" onClick={choosePhoto}>重选</Text>
            </View>
          ) : null}
        </View>

        {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}

        {result ? (
          <View className="pk__result fade-up delay-1">
            <Text className="pk__conclusion">{result.conclusion}</Text>
            <View className="pk__priority">
              <Text className="pk__priority-title">{result.priority_title}</Text>
              <Text className="pk__priority-copy">{result.priority_copy}</Text>
            </View>
            {positive ? (
              <View className="pk__finding pk__finding--positive">
                <Text className="pk__finding-tone">适合</Text>
                <Text className="pk__finding-label">{positive.label}</Text>
              </View>
            ) : null}
            {caution ? (
              <View className="pk__finding pk__finding--caution">
                <Text className="pk__finding-tone">注意</Text>
                <Text className="pk__finding-label">{caution.label}</Text>
              </View>
            ) : null}
            {result.tags.length > 0 ? (
              <View className="pk__tags">
                {result.tags.map((tag) => (
                  <Text key={tag} className="pk__tag">{tag}</Text>
                ))}
              </View>
            ) : null}
            <View className="pk__actions">
              <PrimaryButton text="保存这条判断" onClick={save} />
            </View>
          </View>
        ) : (
          <View className="pk__foot fade-up delay-1">
            <PrimaryButton text={busy ? '正在判断…' : '这件适合我吗'} loading={busy} disabled={!photoPath} onClick={() => analyze(false)} />
            <View className="pk__foot-row">
              <Text className="pk__foot-alt pressable" onClick={choosePhoto}>选择商品图</Text>
              <Text className="pk__foot-alt pressable" onClick={() => analyze(true)}>用示例图体验</Text>
            </View>
            <Text className="pk__foot-note">买之前，先看它适不适合你</Text>
          </View>
        )}
      </View>
    </View>
  )
}
