// 三图建档：照片槽优先、逐槽上传/失败/重试，批量与示例只作为替代路径。
// 三张齐 → 进入补充资料（G1），profile 在 createAnalysis 时带上。
// 交互原则：槽即操作——空槽点按弹系统「拍摄/相册」面板；已传槽点按出
// 重拍/换图/看大图；每槽一个主操作，底部只留一个批量补齐入口。
import { useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import type { MediaAsset } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import ImageViewer from '../../components/image-viewer'
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
  const [justDone, setJustDone] = useState<FlagMap>({})
  const [pulse, setPulse] = useState(false)
  const [viewer, setViewer] = useState<{ url: string; label: string } | null>(null)
  const [demoBusy, setDemoBusy] = useState(false)

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
  })

  const done = SHOTS.filter((shot) => slots[shot.kind]).length
  const ready = done === SHOTS.length
  const working = demoBusy || Object.values(uploading).some(Boolean)

  // 三张齐：进度条满格脉冲一次（反馈「完成」时刻）
  useEffect(() => {
    if (!ready) return
    setPulse(true)
    const t = setTimeout(() => setPulse(false), 900)
    return () => clearTimeout(t)
  }, [ready])

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
        // 成功落位 pop（checkPop 250ms 节奏）
        setJustDone((prev) => ({ ...prev, [kind]: true }))
        setTimeout(() => setJustDone((prev) => ({ ...prev, [kind]: false })), 700)
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

  const pickOne = (kind: Kind, source?: 'camera' | 'album') => {
    if (working) return
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      // 不指定 source 时由微信系统面板选择「拍摄 / 从相册选择」
      sourceType: source ? [source] : ['camera', 'album'],
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

  // 槽即操作：空槽 → 系统面板；失败槽 → 直接重选；已传槽 → 重拍/换图/大图
  const onSlotTap = (kind: Kind, shot: Shot) => {
    if (uploading[kind]) return
    const slot = slots[kind]
    if (!slot || failed[kind]) {
      pickOne(kind)
      return
    }
    Taro.showActionSheet({
      itemList: ['重新拍摄', '从相册换一张', '查看大图'],
      success: (res) => {
        if (res.tapIndex === 0) pickOne(kind, 'camera')
        else if (res.tapIndex === 1) pickOne(kind, 'album')
        else if (res.tapIndex === 2) setViewer({ url: slot.displayUrl, label: shot.label })
      },
      fail: () => {
        /* 用户取消 */
      },
    })
  }

  const fillMissing = () => {
    const missing = SHOTS.filter((shot) => !slots[shot.kind])
    if (missing.length === 0 || demoBusy) return
    Taro.showActionSheet({
      itemList: ['连续拍摄', '从相册批量选择'],
      success: (res) => doFillMissing(res.tapIndex === 0 ? 'camera' : 'album', missing),
      fail: () => {
        /* 用户取消 */
      },
    })
  }

  const doFillMissing = (source: 'camera' | 'album', missing: Shot[]) => {
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
            <View
              className={`capture__progress-fill ${pulse ? 'capture__progress-fill--full' : ''}`}
              style={{ width: `${(done / SHOTS.length) * 100}%` }}
            />
          </View>
        </View>

        {SHOTS.map((shot, index) => {
          const slot = slots[shot.kind]
          const isUploading = Boolean(uploading[shot.kind])
          const isFailed = Boolean(failed[shot.kind])
          return (
            <View
              key={shot.kind}
              className={`capture__slot-card fade-up delay-${index + 1} pressable`}
              onClick={() => onSlotTap(shot.kind, shot)}
            >
              <View
                className={`capture__photo ${!slot && !isUploading ? 'capture__photo--empty' : ''} ${justDone[shot.kind] ? 'capture__photo--pop' : ''}`}
              >
                {slot ? (
                  <ExampleImage
                    className="capture__photo-img"
                    src={slot.displayUrl}
                    user={!slot.demo}
                    mode="aspectFill"
                    badgeText={slot.demo ? '效果示例' : ''}
                  />
                ) : (
                  <Image className="capture__photo-img capture__photo-guide" src={shot.placeholder} mode="aspectFit" />
                )}
                {slot && !isUploading ? (
                  <Text className="capture__photo-index capture__photo-index--done">✓</Text>
                ) : (
                  <Text className="capture__photo-index">{index + 1}</Text>
                )}
                {isUploading ? (
                  <View className="capture__photo-mask">
                    <View className="spinner spinner--on-deep capture__spinner" />
                    <Text className="capture__photo-mask-text">上传中</Text>
                  </View>
                ) : null}
                {isFailed ? (
                  <View className="capture__photo-mask capture__photo-mask--error">
                    <Text className="capture__photo-mask-text">上传失败 · 轻触重试</Text>
                  </View>
                ) : null}
              </View>

              <View className="capture__info">
                <View className="capture__info-head">
                  <Text className="capture__label">{shot.label}</Text>
                  {slot ? <Text className="capture__state">{slot.demo ? '示例已选' : '已上传'}</Text> : null}
                </View>
                <Text className="capture__desc">{shot.desc}</Text>
                {isFailed ? <Text className="capture__error">{failed[shot.kind]}</Text> : null}
                <View className="capture__actions">
                  {slot && !isFailed ? (
                    <Text className="capture__redo">重拍 / 换一张 ›</Text>
                  ) : (
                    <Text className="capture__action capture__action--main">
                      {isFailed ? '重试这张' : '拍摄这张'}
                    </Text>
                  )}
                </View>
              </View>
            </View>
          )
        })}

        <View className="capture__foot fade-up delay-3">
          <PrimaryButton
            text={ready ? '下一步：补充资料' : `已选 ${done} / ${SHOTS.length} 张`}
            disabled={!ready}
            loading={working}
            onClick={next}
          />
          {!ready ? (
            <Text className="capture__alt-action pressable" onClick={fillMissing}>
              补齐剩余 {SHOTS.length - done} 张 ›
            </Text>
          ) : null}
          <Text className="capture__demo pressable" onClick={useDemoPhotos}>
            先用「效果示例」体验完整流程 ›
          </Text>
          <Text className="capture__privacy">照片与建议只对你可见，可随时删除；示例照片会明确标注。</Text>
        </View>
      </View>

      <ImageViewer
        open={Boolean(viewer)}
        url={viewer?.url ?? ''}
        caption={viewer?.label}
        onClose={() => setViewer(null)}
      />
    </View>
  )
}
