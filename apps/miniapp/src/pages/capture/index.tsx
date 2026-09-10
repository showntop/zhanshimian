// 三图建档：照片槽优先、逐槽上传/失败/重试，批量与示例只作为替代路径。
// 三张齐 → 进入补充资料（G1），profile 在 createAnalysis 时带上。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import type { MediaAsset } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import './index.scss'

interface Shot {
  kind: 'face' | 'side' | 'body'
  label: string
  desc: string
  placeholder: string
}

interface Slot {
  asset: MediaAsset
  displayUrl: string
  demo: boolean
}

const SHOTS: Shot[] = [
  { kind: 'face', label: '正脸', desc: '自然表情，看清五官与肤色', placeholder: '/assets/capture/face.png' },
  { kind: 'side', label: '45° 侧脸', desc: '头发不挡轮廓，判断发型空间', placeholder: '/assets/capture/side.png' },
  { kind: 'body', label: '正面全身', desc: '全身入镜，看清头肩与比例', placeholder: '/assets/capture/body.png' },
]

type Kind = Shot['kind']
type FlagMap = Partial<Record<Kind, boolean>>
type ErrorMap = Partial<Record<Kind, string>>

export default function Capture() {
  const [scene, setScene] = useState('')
  const [slots, setSlots] = useState<Partial<Record<Kind, Slot>>>({})
  const [uploading, setUploading] = useState<FlagMap>({})
  const [failed, setFailed] = useState<ErrorMap>({})
  const [demoBusy, setDemoBusy] = useState(false)

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
  })

  const uploadOne = (kind: Kind, filePath: string) => {
    setUploading((prev) => ({ ...prev, [kind]: true }))
    setFailed((prev) => ({ ...prev, [kind]: '' }))
    return api
      .uploadMedia({ kind, filePath })
      .then((asset) => {
        setSlots((prev) => ({
          ...prev,
          [kind]: { asset, displayUrl: filePath || asset.url, demo: false },
        }))
        return true
      })
      .catch(() => {
        setFailed((prev) => ({ ...prev, [kind]: '这张没有上传成功，可以重试或换一张。' }))
        return false
      })
      .finally(() => {
        setUploading((prev) => ({ ...prev, [kind]: false }))
      })
  }

  const takeOne = (kind: Kind, source: 'camera' | 'album') => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: [source],
      // 正脸/侧脸自拍更稳；全身照交由系统默认相机，避免强制前置。
      ...(source === 'camera' && kind !== 'body' ? { camera: 'front' as const } : {}),
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) uploadOne(kind, file.tempFilePath)
      },
      fail: (error) => {
        if (!error.errMsg.includes('cancel')) {
          setFailed((prev) => ({ ...prev, [kind]: '没有打开相机或相册，请再试一次。' }))
        }
      },
    })
  }

  const fillMissing = (source: 'camera' | 'album') => {
    const missing = SHOTS.filter((shot) => !slots[shot.kind])
    if (missing.length === 0 || demoBusy) return
    Taro.chooseMedia({
      count: missing.length,
      mediaType: ['image'],
      sourceType: [source],
      success: async (res) => {
        const files = res.tempFiles.slice(0, missing.length)
        if (files.length === 0) return
        setUploading((prev) => {
          const next = { ...prev }
          missing.slice(0, files.length).forEach((shot) => { next[shot.kind] = true })
          return next
        })
        const results = await Promise.all(
          missing.map(async (shot, index) => {
            const file = files[index]
            if (!file) return true
            return uploadOne(shot.kind, file.tempFilePath)
          }),
        )
        if (results.some((ok) => !ok)) {
          Taro.showToast({ title: '部分照片未上传成功，请在对应照片上重试', icon: 'none' })
        }
      },
    })
  }

  const useDemoPhotos = async () => {
    if (demoBusy) return
    setDemoBusy(true)
    setUploading({ face: true, side: true, body: true })
    setFailed({})
    try {
      const entries = await Promise.all(
        SHOTS.map((shot) => api.createDemoMedia(shot.kind).then((asset) => [shot.kind, { asset, displayUrl: asset.url, demo: asset.demo === true }] as const)),
      )
      setSlots((prev) => {
        const next = { ...prev }
        entries.forEach(([kind, slot]) => { next[kind] = slot })
        return next
      })
    } catch {
      Taro.showToast({ title: '示例照片暂时不可用，请重试', icon: 'none' })
    } finally {
      setUploading({})
      setDemoBusy(false)
    }
  }

  const done = SHOTS.filter((shot) => slots[shot.kind]).length
  const ready = done === SHOTS.length
  const working = demoBusy || Object.values(uploading).some(Boolean)
  const next = () => {
    if (!ready) return
    const mediaIds = SHOTS.map((shot) => slots[shot.kind]!.asset.id)
    Taro.navigateTo({
      url: `/pages/profile-setup/index?scene=${scene || 'general'}&media_ids=${mediaIds.join(',')}`,
    })
  }

  return (
    <View className="page">
      <AppHeader title="创建形象档案" back />
      <View className="capture">
        <View className="capture__intro fade-up">
          <Text className="capture__eyebrow">第 1 步 · 三张自然光照片</Text>
          <Text className="capture__title">先有真实照片，再有可靠建议</Text>
          <Text className="capture__lede">不用化妆，也不需要刻意摆姿势；每一张都可以重拍。</Text>
        </View>

        <View className="capture__progress fade-up">
          <View className="capture__progress-head">
            <Text className="capture__progress-label">{done} / {SHOTS.length} 张已完成</Text>
            <Text className="capture__progress-note">{ready ? '可以进入下一步' : `还差 ${SHOTS.length - done} 张`}</Text>
          </View>
          <View className="capture__progress-track">
            <View className="capture__progress-fill" style={{ width: `${(done / SHOTS.length) * 100}%` }} />
          </View>
        </View>

        {SHOTS.map((shot, index) => {
          const slot = slots[shot.kind]
          return (
            <View key={shot.kind} className={`capture__slot-card fade-up delay-${index + 1}`}>
              <View className="capture__photo">
                {slot ? (
                  <ExampleImage
                    className="capture__photo-img"
                    src={slot.displayUrl}
                    user={!slot.demo}
                    mode="aspectFill"
                    badgeText={slot.demo ? '效果示例' : ''}
                  />
                ) : (
                  <Image className="capture__photo-img" src={shot.placeholder} mode="aspectFit" />
                )}
                <Text className="capture__photo-index">{index + 1}</Text>
                {uploading[shot.kind] ? (
                  <View className="capture__photo-mask">
                    <View className="spinner spinner--on-deep capture__spinner" />
                    <Text className="capture__photo-mask-text">上传中</Text>
                  </View>
                ) : null}
                {failed[shot.kind] ? (
                  <View className="capture__photo-mask capture__photo-mask--error">
                    <Text className="capture__photo-mask-text">上传失败</Text>
                  </View>
                ) : null}
              </View>

              <View className="capture__info">
                <View className="capture__info-head">
                  <Text className="capture__label">{shot.label}</Text>
                  {slot ? <Text className="capture__state">{slot.demo ? '示例已选' : '已上传'}</Text> : null}
                </View>
                <Text className="capture__desc">{shot.desc}</Text>
                {failed[shot.kind] ? <Text className="capture__error">{failed[shot.kind]}</Text> : null}
                <View className="capture__actions">
                  <Text className="capture__action capture__action--main pressable" onClick={() => takeOne(shot.kind, 'camera')}>
                    {slot ? '重拍' : '拍摄'}
                  </Text>
                  <Text className="capture__action pressable" onClick={() => takeOne(shot.kind, 'album')}>
                    相册
                  </Text>
                </View>
              </View>
            </View>
          )
        })}

        <View className="capture__foot fade-up delay-3">
          <PrimaryButton
            text={ready ? '下一步：补充资料' : `先补齐缺的 ${SHOTS.length - done} 张`}
            disabled={!ready}
            loading={working}
            onClick={next}
          />
          <View className="capture__alt">
            <Text className="capture__alt-action pressable" onClick={() => fillMissing('camera')}>
              {ready ? '连续重拍三张' : `连拍缺的 ${SHOTS.length - done} 张`}
            </Text>
            <Text className="capture__alt-action pressable" onClick={() => fillMissing('album')}>
              {ready ? '相册换三张' : '从相册批量选择'}
            </Text>
          </View>
          <Text className="capture__demo pressable" onClick={useDemoPhotos}>
            先用「效果示例」体验完整流程 ›
          </Text>
          <Text className="capture__privacy">照片与建议只对你可见，可随时删除；示例照片会明确标注。</Text>
        </View>
      </View>
    </View>
  )
}
