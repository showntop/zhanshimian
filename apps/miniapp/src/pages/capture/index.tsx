// 三图建档：三个并行照片槽（连拍/相册/单槽重拍/示例体验）。
// 三张齐 → 进入补充资料（G1），profile 在 createAnalysis 时带上。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import type { MediaAsset } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ErrorState from '../../components/error-state'
import './index.scss'

interface Shot {
  kind: 'face' | 'side' | 'body'
  label: string
  desc: string
  placeholder: string
}

const SHOTS: Shot[] = [
  { kind: 'face', label: '正脸', desc: '看清五官与肤色', placeholder: '/assets/capture/face.png' },
  { kind: 'side', label: '45° 侧脸', desc: '判断轮廓与发型', placeholder: '/assets/capture/side.png' },
  { kind: 'body', label: '正面全身', desc: '分析头肩与身材比例', placeholder: '/assets/capture/body.png' },
]

export default function Capture() {
  const [scene, setScene] = useState('')
  const [assets, setAssets] = useState<Partial<Record<Shot['kind'], MediaAsset>>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
  })

  const takeOne = (kind: Shot['kind'], source: 'camera' | 'album') => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: [source],
      camera: 'front',
      success: (res) => {
        const file = res.tempFiles[0]
        if (!file) return
        setBusy(true)
        api
          .uploadMedia({ kind, filePath: file.tempFilePath })
          .then((asset) =>
            setAssets((prev) => ({
              ...prev,
              [kind]: { ...asset, url: file.tempFilePath || asset.url },
            })),
          )
          .catch((e: Error) => setError(e.message || '上传没有成功，请重试'))
          .finally(() => setBusy(false))
      },
    })
  }

  const fillMissing = (source: 'camera' | 'album') => {
    const missing = SHOTS.filter((s) => !assets[s.kind])
    if (missing.length === 0) return
    Taro.chooseMedia({
      count: missing.length,
      mediaType: ['image'],
      sourceType: [source],
      success: (res) => {
        const files = res.tempFiles.slice(0, missing.length)
        setBusy(true)
        Promise.all(
          missing.map((shot, i) =>
            files[i]
              ? api
                  .uploadMedia({ kind: shot.kind, filePath: files[i]!.tempFilePath })
                  .then((asset) =>
                    [shot.kind, { ...asset, url: files[i]!.tempFilePath || asset.url }] as const,
                  )
              : Promise.resolve(null),
          ),
        )
          .then((pairs) => {
            const next = { ...assets }
            pairs.forEach((pair) => {
              if (pair) next[pair[0]] = pair[1]
            })
            setAssets(next)
          })
          .catch((e: Error) => setError(e.message || '上传没有成功，请重试'))
          .finally(() => setBusy(false))
      },
    })
  }

  const useDemoPhotos = async () => {
    setBusy(true)
    setError('')
    try {
      const entries = await Promise.all(
        SHOTS.map((shot) => api.createDemoMedia(shot.kind).then((asset) => [shot.kind, asset] as const)),
      )
      const next: Partial<Record<Shot['kind'], MediaAsset>> = { ...assets }
      entries.forEach(([kind, asset]) => {
        next[kind] = asset
      })
      setAssets(next)
    } catch (e) {
      setError((e as Error).message || '示例照片创建失败')
    } finally {
      setBusy(false)
    }
  }

  const done = SHOTS.filter((s) => assets[s.kind]).length
  const ready = done === SHOTS.length

  const next = () => {
    const mediaIds = SHOTS.map((s) => assets[s.kind]!.id)
    Taro.navigateTo({
      url: `/pages/profile-setup/index?scene=${scene || 'interview'}&media_ids=${mediaIds.join(',')}`,
    })
  }

  return (
    <View className="page">
      <AppHeader title="创建形象档案" back />
      <View className="capture">
        <View className="capture__intro fade-up">
          <Text className="capture__eyebrow">三张自然光照片</Text>
          <Text className="capture__title">让建议真正像你</Text>
          <Text className="capture__lede">不用化妆，也不需要刻意摆姿势。</Text>
        </View>

        {SHOTS.map((shot, i) => (
          <View key={shot.kind} className={`capture__row fade-up delay-${i + 1}`}>
            <View className="capture__row-main">
              <Text className="capture__row-label">{shot.label}</Text>
              <Text className="capture__row-desc">{shot.desc}</Text>
              <View className="capture__row-actions">
                <Text className="capture__row-action pressable" onClick={() => takeOne(shot.kind, 'camera')}>
                  拍摄
                </Text>
                <Text className="capture__row-action pressable" onClick={() => takeOne(shot.kind, 'album')}>
                  相册
                </Text>
              </View>
            </View>
            {assets[shot.kind] ? (
              <View className="capture__done pressable" onClick={() => takeOne(shot.kind, 'camera')}>
                <Image className="capture__done-img" src={assets[shot.kind]!.url} mode="aspectFill" />
                <Text className="capture__done-hint">重拍</Text>
              </View>
            ) : (
              <View className="capture__slot" onClick={() => takeOne(shot.kind, 'camera')}>
                <Image className="capture__slot-img" src={shot.placeholder} mode="aspectFit" />
              </View>
            )}
          </View>
        ))}

        {error ? <ErrorState message={error} onRetry={() => setError('')} retryText="知道了" /> : null}

        <View className="capture__foot fade-up delay-3">
          <PrimaryButton text={ready ? '继续补充资料' : `连拍缺的 ${SHOTS.length - done} 张`} disabled={!ready} loading={busy} onClick={next} />
          <Text className="capture__count">
            {done} / {SHOTS.length} 张已完成
          </Text>
          <View className="capture__alt">
            <Text className="capture__alt-action pressable" onClick={() => fillMissing('album')}>
              从相册批量选择
            </Text>
            <Text className="capture__alt-action pressable" onClick={useDemoPhotos}>
              使用示例照片体验
            </Text>
          </View>
          <Text className="capture__privacy">照片与建议只对你可见，可随时删除</Text>
        </View>
      </View>
    </View>
  )
}
