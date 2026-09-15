// 三图建档：只做拍摄、上传与提交。状态机与提交入参在 ./model，纯逻辑不在这里。
//
// 与旧 capture 页的四处结构性差别：
// 1. 不写 Storage。受理后把 Operation 放进 resourceCache 再 redirectTo，
//    分析页从 cache + 服务端读；本地再存一份"当前任务"就等于造第二个真相。
// 2. 不发补充资料。CreateAssessmentRequest 只有 photos（additionalProperties: false），
//    身高/身份/预算由服务端在受理时自行快照；编辑入口归 pages/profile，不在这里再放一份。
// 3. 不收 ?scene。契约里 assessment 没有场景字段，场景属于规划阶段。
// 4. 不做"检测到已有分析就跳走"的门禁。那依赖本地 Storage 里的任务 id，
//    而恢复能力归分析页自己。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import {
  CAPTURE_COPY,
  IMAGE_BADGE_COPY,
  captureFillMissingText,
  captureSelectedText,
  type DisplayMedia,
} from '@zsm/core'
import { uploadMedia } from '../../app/api/media-upload'
import { mediaUpload } from '../../app/api/client'
import { PublicApiError } from '../../app/api/result'
import { qualityApi } from '../../app/api/quality'
import { submitAssessment, assessmentSubmitErrorText } from '../assessment/start'
import PrimaryButton from '../../components/primary-button'
import SourceImage from '../../components/source-image'
import { mimeTypeOf, readLocalImage } from './local-file'
import {
  CAPTURE_ROLES,
  assessmentIdempotencyKey,
  captureReady,
  createSlots,
  photosByRole,
  updateSlot,
  type CaptureRole,
  type CaptureSlot,
  type CaptureSlots,
} from './model'
import './index.scss'

/**
 * 拍摄姿势指导图。它是"怎么拍"的示意，不是缺图时的结果 fallback：
 * 所以走普通 Image，不进 SourceImage——那是一张内容图的投影器，
 * 给引导图套上「风格参考」角标只会让人以为那是别人拍的效果。
 */
const GUIDE_IMAGE: Record<CaptureRole, string> = {
  face: '/assets/capture/face.jpg',
  side: '/assets/capture/side.jpg',
  body: '/assets/capture/body.jpg',
}

/**
 * 本地预览用的"永不过期"时刻。预览图是本次会话里的临时文件，
 * 真实过期语义没有意义；给远未来常量，投影就不会因为"看起来过期"而拒绝它。
 */
const LOCAL_PREVIEW_EXPIRES_AT = '9999-12-31T00:00:00Z'

/**
 * 槽位当前该渲染的图。
 *
 * 关键事实：上传成功后服务端只回 asset id——MediaAsset 没有 URL，契约里也没有媒体读取接口。
 * 所以本地临时文件是整条建档流程里唯一能渲染的图源。这里显式声明 `source_kind: 'user_original'`
 * （角标「原本」、不弱化）：这是**声明**，不是从路径/后缀/来源猜出来的，用户刚拍的照片本来就是用户原图。
 *
 * 已知边界：微信开发者工具的 chooseMedia 返回 `http://tmp/...`，投影只认 https/wxfile/file，
 * 于是开发者工具里槽位落到空态（真机是 `wxfile://`，正常显示）。这里不放宽协议白名单去迁就它——
 * 那条白名单是挡"服务端塞个 http 图进来"的，放宽的代价远大于开发者工具看不见预览。
 */
function slotMedia(role: CaptureRole, slot: CaptureSlot): DisplayMedia | null {
  if (slot.media) return slot.media
  if (!slot.localPath) return null
  return localPreview(role, slot.localPath, `local-${role}`)
}

/** 本地临时文件的用户原图投影：还没拿到 asset id 时用本地键占位（只参与 React key）。 */
function localPreview(role: CaptureRole, localPath: string, assetId: string): DisplayMedia {
  return {
    asset_id: assetId,
    url: localPath,
    url_expires_at: LOCAL_PREVIEW_EXPIRES_AT,
    mime_type: mimeTypeOf(localPath),
    source_kind: 'user_original',
    display_label: IMAGE_BADGE_COPY.original,
  }
}

export default function CaptureScreen() {
  const [slots, setSlots] = useState<CaptureSlots>(createSlots)
  const [focusRole, setFocusRole] = useState<CaptureRole>('face')
  const [busy, setBusy] = useState(false)

  const photos = photosByRole(slots)
  const done = CAPTURE_ROLES.filter((role) => slots[role].phase === 'ready').length
  const ready = captureReady(photos)
  const working = CAPTURE_ROLES.some((role) => {
    const { phase } = slots[role]
    return phase === 'selecting' || phase === 'hashing' || phase === 'uploading'
  })

  const patch = (role: CaptureRole, next: Partial<Omit<CaptureSlot, 'role'>>) => {
    setSlots((prev) => updateSlot(prev, role, next))
  }

  /**
   * 单张照片的完整事务：读取摘要 → 直传 → 落槽。
   * 返回是否成功，调用方据此报错——不要回头去读 state，批量补拍时那是上一轮的值。
   */
  const ingest = async (role: CaptureRole, filePath: string): Promise<boolean> => {
    patch(role, { phase: 'hashing', localPath: filePath, media: null, errorText: '' })
    try {
      const file = await readLocalImage(filePath)
      patch(role, { phase: 'uploading' })
      const asset = await uploadMedia(mediaUpload, file, role)
      patch(role, { phase: 'ready', media: localPreview(role, filePath, asset.id), errorText: '' })
      return true
    } catch (error) {
      // 真机排障依赖 vConsole：保留原始错误（域名 600002 / 签名 403 / sha256 不支持在此区分）。
      // Error 对象在 vConsole 只显示 message，PublicApiError 的 code/statusCode 要展开打印。
      const detail = error instanceof PublicApiError
        ? `${error.code} status=${error.statusCode} req=${error.requestId || '-'}`
        : error
      console.warn('[capture] ingest failed', role, detail)
      // 保留 localPath 与 errorText：用户点"重试"是重传这一份，不必再拍一次
      patch(role, { phase: 'failed', errorText: CAPTURE_COPY.slotFailed })
      return false
    }
  }

  const pick = (role: CaptureRole, source?: 'camera' | 'album') => {
    if (working || busy) return
    setFocusRole(role)
    // 取消选择时要回到选之前的样子：老照片还在就回到 ready，否则回到空槽
    const revertTo = slots[role].media ? 'ready' : 'empty'
    patch(role, { phase: 'selecting' })
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      // 不指定 source 时由系统面板提供「拍摄 / 从相册选择」
      sourceType: source ? [source] : ['camera', 'album'],
      // 正脸/侧脸自拍更稳；全身照交给默认相机，避免强制前置
      ...(source === 'camera' && role !== 'body' ? { camera: 'front' as const } : {}),
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) void ingest(role, file.tempFilePath)
        else patch(role, { phase: revertTo })
      },
      fail: (error) => {
        if (error.errMsg.includes('cancel')) {
          patch(role, { phase: revertTo })
          return
        }
        patch(role, { phase: 'failed', errorText: CAPTURE_COPY.pickerFailed })
      },
    })
  }

  // 槽即操作：空槽/失败槽 → 系统面板或重传；已就绪槽 → 重拍 / 换图 / 看大图
  const onSlotTap = (role: CaptureRole) => {
    if (working || busy) return
    setFocusRole(role)
    const slot = slots[role]
    if (slot.phase === 'failed') {
      // 本地文件还在就重传那一份，不在（比如拍照失败）就重新选
      if (slot.localPath) void ingest(role, slot.localPath)
      else pick(role)
      return
    }
    if (slot.phase !== 'ready') {
      pick(role)
      return
    }
    Taro.showActionSheet({
      itemList: [
        CAPTURE_COPY.actionShootAgain,
        CAPTURE_COPY.actionFromAlbum,
        CAPTURE_COPY.actionViewLarge,
      ],
      success: (res) => {
        if (res.tapIndex === 0) pick(role, 'camera')
        else if (res.tapIndex === 1) pick(role, 'album')
        else if (res.tapIndex === 2) previewLarge(role)
      },
      fail: () => {
        /* 用户取消 */
      },
    })
  }

  const previewLarge = (role: CaptureRole) => {
    const media = slotMedia(role, slots[role])
    if (media) Taro.previewImage({ current: media.url, urls: [media.url] })
  }

  const fillMissing = () => {
    const missing = CAPTURE_ROLES.filter((role) => slots[role].phase !== 'ready')
    if (missing.length === 0 || working || busy) return
    Taro.showActionSheet({
      itemList: [CAPTURE_COPY.actionContinuousShoot, CAPTURE_COPY.actionBatchPick],
      success: (res) => batchFill(res.tapIndex === 0 ? 'camera' : 'album', missing),
      fail: () => {
        /* 用户取消 */
      },
    })
  }

  const batchFill = (source: 'camera' | 'album', missing: readonly CaptureRole[]) => {
    Taro.chooseMedia({
      count: missing.length,
      mediaType: ['image'],
      sourceType: [source],
      success: (res) => {
        const files = res.tempFiles.slice(0, missing.length)
        void Promise.all(
          missing.map((role, index) => {
            const file = files[index]
            return file ? ingest(role, file.tempFilePath) : Promise.resolve(false)
          }),
        ).then((outcomes) => {
          if (outcomes.some((ok) => !ok)) {
            Taro.showToast({ title: CAPTURE_COPY.batchPartialFailure, icon: 'none' })
          }
        })
      },
    })
  }

  /** Demo 体验：三个角色分别取「效果示例」，来源不对就整槽不落（见 qualityApi.createDemoMedia）。 */
  const useDemoPhotos = async () => {
    if (working || busy) return
    setBusy(true)
    const outcomes = await Promise.all(
      CAPTURE_ROLES.map(async (role) => {
        patch(role, { phase: 'uploading', errorText: '' })
        try {
          const media = await qualityApi.createDemoMedia(role, `demo:${role}`)
          patch(role, { phase: 'ready', media, localPath: '', errorText: '' })
          return true
        } catch {
          patch(role, { phase: 'empty', media: null, localPath: '' })
          return false
        }
      }),
    )
    setBusy(false)
    if (outcomes.some((ok) => !ok)) {
      Taro.showToast({ title: CAPTURE_COPY.demoUnavailable, icon: 'none' })
    }
  }

  /**
   * 提交固定流程：createAssessment → 落缓存 → redirectTo 分析页，
   * 全在 features/assessment/start 的 submitAssessment 里——进度页的「重新发起」
   * 走同一条路径，两处不会只改一处。
   * 建档是一次正向流程，redirectTo 清栈，返回不会回到拍摄页。
   */
  const submit = async () => {
    if (!ready || busy) return
    setBusy(true)
    try {
      await submitAssessment(photos, assessmentIdempotencyKey(photos))
    } catch (error) {
      Taro.showToast({ title: assessmentSubmitErrorText(error), icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <View className="capture-screen">
      <View className="capture-screen__intro fade-up">
        <Text className="capture-screen__eyebrow">{CAPTURE_COPY.eyebrow}</Text>
        <Text className="capture-screen__title">{CAPTURE_COPY.headline}</Text>
        <Text className="capture-screen__lede">{CAPTURE_COPY.lede}</Text>
      </View>

      <View className="capture-screen__slots fade-up delay-1">
        {CAPTURE_ROLES.map((role, index) => {
          const slot = slots[role]
          const copy = CAPTURE_COPY.shots[role]
          const media = slotMedia(role, slot)
          const masked = slot.phase === 'hashing' || slot.phase === 'uploading'
          return (
            <View
              key={role}
              className={[
                'capture-screen__slot pressable',
                focusRole === role ? 'capture-screen__slot--focus' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              onClick={() => onSlotTap(role)}
            >
              <View
                className={`capture-screen__photo ${media ? '' : 'capture-screen__photo--empty'}`}
              >
                {media ? (
                  <SourceImage
                    className="capture-screen__photo-img"
                    media={media}
                    mode="aspectFit"
                    emptyActionText={CAPTURE_COPY.actionReplace}
                    onEmptyAction={() => pick(role)}
                  />
                ) : (
                  <Image
                    className="capture-screen__photo-guide"
                    src={GUIDE_IMAGE[role]}
                    mode="aspectFit"
                  />
                )}
                <Text className="capture-screen__photo-index">{index + 1}</Text>
                {masked ? (
                  <View className="capture-screen__photo-mask">
                    <View className="spinner spinner--on-deep capture-screen__spinner" />
                    <Text className="capture-screen__photo-mask-text">
                      {slot.phase === 'hashing'
                        ? CAPTURE_COPY.phaseHashing
                        : CAPTURE_COPY.phaseUploading}
                    </Text>
                  </View>
                ) : null}
                {slot.phase === 'failed' ? (
                  <View className="capture-screen__photo-mask capture-screen__photo-mask--error">
                    <Text className="capture-screen__photo-mask-text">
                      {CAPTURE_COPY.slotFailedLabel}
                    </Text>
                  </View>
                ) : null}
              </View>

              <Text className="capture-screen__slot-label">{copy.label}</Text>
              <Text className="capture-screen__slot-desc">{copy.desc}</Text>
              <Text
                className={
                  slot.phase === 'ready'
                    ? 'capture-screen__slot-state'
                    : 'capture-screen__slot-hint'
                }
              >
                {slotStateText(slot)}
              </Text>
            </View>
          )
        })}
      </View>

      <View
        className={`capture-screen__foot fade-up delay-2 ${ready ? 'capture-screen__foot--ready' : ''}`}
      >
        <PrimaryButton
          text={ready ? CAPTURE_COPY.submitAction : captureSelectedText(done, CAPTURE_ROLES.length)}
          disabled={!ready}
          loading={busy}
          onClick={submit}
        />
        {ready ? null : (
          <Text className="capture-screen__alt-action pressable" onClick={fillMissing}>
            {captureFillMissingText(CAPTURE_ROLES.length - done)}
          </Text>
        )}
        <Text className="capture-screen__demo pressable" onClick={useDemoPhotos}>
          {CAPTURE_COPY.demoAction}
        </Text>
        <Text className="capture-screen__privacy">{CAPTURE_COPY.privacy}</Text>
      </View>
    </View>
  )
}

/** 槽位下方那一行状态字。Demo 落槽没有本地文件，所以"已上传/示例已选"按 localPath 区分。 */
function slotStateText(slot: CaptureSlot): string {
  switch (slot.phase) {
    case 'ready':
      return slot.localPath ? CAPTURE_COPY.slotReadyHint : CAPTURE_COPY.slotDemoHint
    case 'failed':
      return CAPTURE_COPY.actionRetry
    case 'hashing':
      return CAPTURE_COPY.phaseHashing
    case 'uploading':
      return CAPTURE_COPY.phaseUploading
    default:
      return CAPTURE_COPY.slotEmptyHint
  }
}
