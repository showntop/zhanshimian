// 三图建档（合并补充资料）：三联槽位 + 选填资料一页完成，提交即进入分析。
// 请求纪律：槽逐个上传；提交 = PUT /v1/me/profile（失败不阻塞）+ createAnalysis。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Slider, Text, View } from '@tarojs/components'
import type { MediaAsset } from '@zsm/core'
import { PROFILE_SETUP_COPY, type UserProfile } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import ImageViewer from '../../components/image-viewer'
import Pill from '../../components/pill'
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

const ROLES = ['产品经理', '设计师', '咨询顾问', '学生', PROFILE_SETUP_COPY.skip]
const BUDGETS = ['500 以内', '500–1500', '1500 以上', PROFILE_SETUP_COPY.skip]
const HEIGHT_MIN = 145
const HEIGHT_MAX = 185

type Kind = Shot['kind']
type FlagMap = Partial<Record<Kind, boolean>>
type ErrorMap = Partial<Record<Kind, string>>

export default function Capture() {
  const [scene, setScene] = useState('')
  const [slots, setSlots] = useState<Partial<Record<Kind, Slot>>>({})
  const [uploading, setUploading] = useState<FlagMap>({})
  const [failed, setFailed] = useState<ErrorMap>({})
  const [justDone, setJustDone] = useState<FlagMap>({})
  const [viewer, setViewer] = useState<{ url: string; label: string } | null>(null)
  const [demoBusy, setDemoBusy] = useState(false)
  // 补充资料（选填）
  const [height, setHeight] = useState(165)
  const [role, setRole] = useState<string>(PROFILE_SETUP_COPY.skip)
  const [budget, setBudget] = useState<string>(PROFILE_SETUP_COPY.skip)
  const [busy, setBusy] = useState(false)

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
  })

  const done = SHOTS.filter((shot) => slots[shot.kind]).length
  const ready = done === SHOTS.length
  const working = demoBusy || Object.values(uploading).some(Boolean)

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
    if (working || busy) return
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
    if (uploading[kind] || busy) return
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
    if (missing.length === 0 || demoBusy || busy) return
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
    if (demoBusy || busy) return
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

  const submit = async () => {
    if (!ready || busy) return
    setBusy(true)
    // 服务端契约要求 role/budget 为非空字符串；跳过时用哨兵值，不显示成默认身份。
    const profile: UserProfile = {
      height_cm: height,
      role: role === PROFILE_SETUP_COPY.skip ? '未填写' : role,
      budget: budget === PROFILE_SETUP_COPY.skip ? '未填写' : budget,
    }
    try {
      // 持久化失败不阻塞：分析仍带当次快照
      await api.updateMyProfile(profile).catch(() => undefined)
      const { data } = await api.createAnalysis({
        scene: scene || 'general',
        media_ids: SHOTS.map((shot) => slots[shot.kind]!.asset.id),
        profile,
      })
      // 建档链路是一次正向流程，reLaunch 清栈直达分析页。
      Taro.reLaunch({ url: `/pages/analysis/index?id=${data.id}` })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '提交没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
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

        <View className="capture__slots fade-up delay-1">
          {SHOTS.map((shot, index) => {
            const slot = slots[shot.kind]
            const isUploading = Boolean(uploading[shot.kind])
            const isFailed = Boolean(failed[shot.kind])
            return (
              <View
                key={shot.kind}
                className={`capture__slot pressable ${justDone[shot.kind] ? 'capture__slot--pop' : ''}`}
                onClick={() => onSlotTap(shot.kind, shot)}
              >
                <View className={`capture__photo ${!slot && !isUploading ? 'capture__photo--empty' : ''}`}>
                  {slot ? (
                    <ExampleImage
                      className="capture__photo-img"
                      src={slot.displayUrl}
                      user={!slot.demo}
                      mode="aspectFill"
                      badgeText={slot.demo ? '效果示例' : ''}
                    />
                  ) : (
                    <Image className="capture__photo-guide" src={shot.placeholder} mode="aspectFill" />
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
                <Text className="capture__slot-label">{shot.label}</Text>
                {slot ? (
                  <Text className="capture__slot-state">{slot.demo ? '示例已选' : '已上传'}</Text>
                ) : (
                  <Text className="capture__slot-hint">轻触拍摄</Text>
                )}
              </View>
            )
          })}
        </View>

        <View className="capture__profile card fade-up delay-2">
          <View className="capture__profile-head">
            <Text className="capture__profile-title">{PROFILE_SETUP_COPY.eyebrow}</Text>
            <Text className="capture__profile-note">全部选填</Text>
          </View>

          <View className="capture__field">
            <View className="capture__field-head">
              <Text className="capture__field-name">{PROFILE_SETUP_COPY.height}</Text>
              <Text className="capture__field-note">{PROFILE_SETUP_COPY.heightNote}</Text>
            </View>
            <Text className="capture__height">{height} cm</Text>
            <Slider
              className="capture__slider"
              min={HEIGHT_MIN}
              max={HEIGHT_MAX}
              step={1}
              value={height}
              activeColor="#587344"
              backgroundColor="#DDE5D7"
              blockSize={24}
              blockColor="#FFFFFF"
              onChange={(event) => setHeight(Number(event.detail.value))}
            />
            <View className="capture__stepper">
              <Text className="capture__step pressable" onClick={() => setHeight((h) => Math.max(HEIGHT_MIN, h - 1))}>
                −
              </Text>
              <Text className="capture__range">{HEIGHT_MIN}–{HEIGHT_MAX} cm</Text>
              <Text className="capture__step pressable" onClick={() => setHeight((h) => Math.min(HEIGHT_MAX, h + 1))}>
                ＋
              </Text>
            </View>
          </View>

          <View className="capture__field">
            <Text className="capture__field-name">{PROFILE_SETUP_COPY.role}</Text>
            <View className="capture__chips">
              {ROLES.map((item) => (
                <Pill key={item} label={item} active={role === item} onClick={() => setRole(item)} />
              ))}
            </View>
          </View>

          <View className="capture__field">
            <Text className="capture__field-name">{PROFILE_SETUP_COPY.budget}</Text>
            <View className="capture__chips">
              {BUDGETS.map((item) => (
                <Pill key={item} label={item} active={budget === item} onClick={() => setBudget(item)} />
              ))}
            </View>
          </View>
        </View>

        <View className="capture__foot fade-up delay-3">
          <PrimaryButton
            text={ready ? '开始形象分析' : `已选 ${done} / ${SHOTS.length} 张`}
            disabled={!ready}
            loading={busy}
            onClick={submit}
          />
          {!ready ? (
            <Text className="capture__alt-action pressable" onClick={fillMissing}>
              补齐剩余 {SHOTS.length - done} 张 ›
            </Text>
          ) : null}
          <Text className="capture__demo pressable" onClick={useDemoPhotos}>
            先用「效果示例」体验完整流程 ›
          </Text>
          <Text className="capture__privacy">{PROFILE_SETUP_COPY.privacy}</Text>
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
