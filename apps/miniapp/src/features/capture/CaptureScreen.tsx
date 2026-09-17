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
import { handleBillingError } from '../../services/billing'
import PrimaryButton from '../../components/primary-button'
import SourceImage from '../../components/source-image'
import { mimeTypeOf, normalizeUploadableImage, readLocalImage, stabilizeLocalPath } from './local-file'
import { useDisplayablePath } from '../../hooks/use-displayable-path'
import {
  CAPTURE_ROLES,
  assessmentIdempotencyKey,
  batchAssignmentRoles,
  batchChooseCount,
  captureReady,
  createSlots,
  demoRestoreSlot,
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
 * 所以本地文件是整条建档流程里唯一能渲染的图源。这里显式声明 `source_kind: 'user_original'`
 * （角标「原本」、不弱化）：这是**声明**，不是从路径/后缀/来源猜出来的，用户刚拍的照片本来就是用户原图。
 *
 * localPath 在 ingest 入口已被 stabilizeLocalPath 沉淀到本地持久目录（真机
 * wxfile://；开发者工具 http://usr，工具的本地文件模拟，投影白名单含这两个
 * 本地主机名——见 media/display.ts 的 isDevtoolsLocalFile）。白名单不对一般
 * http:// 开口：它是挡「服务端塞个 http 图进来」的。
 *
 * displayPath：工具 lib 3.17.1 起渲染层把 http://tmp/、http://usr/ 按 CORS
 * 拦截，投影进 url 的是换出后的可渲染值（data URL），不是本地路径本体。
 */
function slotMedia(role: CaptureRole, slot: CaptureSlot, displayPath: string): DisplayMedia | null {
  if (slot.media) return slot.media
  if (!slot.localPath) return null
  return localPreview(role, displayPath || slot.localPath, `local-${role}`)
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
  // 工具新渲染层按 CORS 拦截 http://tmp/、http://usr/：每个槽位的本地路径
  // 各换出一份可渲染值（真机原样返回）。state 里存的仍是原路径。
  const faceDisplay = useDisplayablePath(slots.face.localPath)
  const sideDisplay = useDisplayablePath(slots.side.localPath)
  const bodyDisplay = useDisplayablePath(slots.body.localPath)
  const displayPath = (role: CaptureRole): string =>
    role === 'face' ? faceDisplay : role === 'side' ? sideDisplay : bodyDisplay

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
      // 先沉淀成 wxfile://：开发者工具的 http://tmp 临时路径过不了投影协议白名单，
      // 且临时文件被微信清理后「重传这一份」会永远失败
      const stablePath = await stabilizeLocalPath(filePath)
      const file = await readLocalImage(stablePath)
      patch(role, { phase: 'uploading', localPath: stablePath })
      const asset = await uploadMedia(mediaUpload, file, role)
      patch(role, { phase: 'ready', media: localPreview(role, stablePath, asset.id), errorText: '' })
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
      // 送微信压缩档：AI 分析不需要原图，上传与下游推理都快一个量级
      sizeType: ['compressed'],
      // 正脸/侧脸自拍更稳；全身照交给默认相机，避免强制前置
      ...(source === 'camera' && role !== 'body' ? { camera: 'front' as const } : {}),
      success: (res) => {
        const file = res.tempFiles[0]
        // HEIC 等非 JPEG/PNG 会被服务端按字节拒：先归一成 JPEG 再 ingest
        if (file) void normalizeUploadableImage(file.tempFilePath).then((path) => ingest(role, path))
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

  // 槽即操作：空槽 → 系统面板；失败槽 → 重传或换一张；已就绪槽 → 重拍 / 换图 / 看大图
  const onSlotTap = (role: CaptureRole) => {
    if (working || busy) return
    setFocusRole(role)
    const slot = slots[role]
    if (slot.phase === 'failed') {
      // 本地文件不在（比如拍照失败）只能重新选
      if (!slot.localPath) {
        pick(role)
        return
      }
      // 重传与换一张两个出口都给：临时文件会被微信清理，
      // 只给「重传同一文件」就是把用户困进永远失败的死循环
      Taro.showActionSheet({
        itemList: [CAPTURE_COPY.actionRetry, CAPTURE_COPY.actionReplace],
        success: (res) => {
          if (res.tapIndex === 0) void ingest(role, slot.localPath)
          else if (res.tapIndex === 1) pick(role)
        },
        fail: () => {
          /* 用户取消 */
        },
      })
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
    const media = slotMedia(role, slots[role], displayPath(role))
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
      // 相机一次只出一张；少选/单拍留下的槽保持原状，不算失败
      count: batchChooseCount(source, missing.length),
      mediaType: ['image'],
      sourceType: [source],
      sizeType: ['compressed'],
      success: (res) => {
        const roles = batchAssignmentRoles(missing, res.tempFiles.length)
        void Promise.all(
          roles.map((role, index) => {
            const file = res.tempFiles[index]
            // 同上：非 JPEG/PNG 先归一成 JPEG（批量选图逐张转换）
            return file
              ? normalizeUploadableImage(file.tempFilePath).then((path) => ingest(role, path))
              : Promise.resolve(false)
          }),
        ).then((outcomes) => {
          if (outcomes.some((ok) => !ok)) {
            Taro.showToast({ title: CAPTURE_COPY.batchPartialFailure, icon: 'none' })
          }
        })
      },
      fail: (error) => {
        // 用户取消是静默；真的打不开相机/相册才告知
        if (error.errMsg.includes('cancel')) return
        Taro.showToast({ title: CAPTURE_COPY.pickerFailed, icon: 'none' })
      },
    })
  }

  /** Demo 体验：三个角色分别取「效果示例」，来源不对就整槽不落（见 qualityApi.createDemoMedia）。 */
  const useDemoPhotos = async () => {
    if (working || busy) return
    setBusy(true)
    // 进 demo 流程前的整页快照：哪个角色失败就恢复哪个槽（与 pick 的 revertTo 同一语义），
    // 绝不整槽清空——用户已有 ready 真实照片时，一次示例拉取失败不能把已有照片一起丢掉
    const before = slots
    const outcomes = await Promise.all(
      CAPTURE_ROLES.map(async (role) => {
        patch(role, { phase: 'uploading', errorText: '' })
        try {
          const media = await qualityApi.createDemoMedia(role, `demo:${role}`)
          patch(role, { phase: 'ready', media, localPath: '', errorText: '' })
          return true
        } catch {
          patch(role, demoRestoreSlot(before[role]))
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
      // 402/429 等计费错误先走购买引导（弹层→标记→profile 购买层），其余才落通用提示
      if (handleBillingError(error)) return
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
          const media = slotMedia(role, slot, displayPath(role))
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
      // 失败原因优先（「可以重试或换一张」正是失败槽的两个出口），没有才退回动作名
      return slot.errorText || CAPTURE_COPY.actionRetry
    case 'hashing':
      return CAPTURE_COPY.phaseHashing
    case 'uploading':
      return CAPTURE_COPY.phaseUploading
    default:
      return CAPTURE_COPY.slotEmptyHint
  }
}
