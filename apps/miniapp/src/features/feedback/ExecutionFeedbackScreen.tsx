// 执行反馈页：只有 completed 的执行可以提交；实拍可选，上传失败不阻塞反馈。
//
// 上传失败的三条出路（缺一不可）：
// 1. 已选标签与文字原样保留；
// 2. 明说"照片没有上传成功"，给出「重试上传」和「先提交文字和标签」两条路；
// 3. 后者提交 media 为空的请求——契约不允许 null 占位，就整段省略。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, Textarea, View } from '@tarojs/components'
import {
  CHECKLIST_COPY,
  ERROR_COPY,
  EXECUTION_FEEDBACK_TAGS,
  FEEDBACK_SCREEN_COPY,
  feedbackAcknowledgement,
  type FeedbackAcknowledgementCode,
} from '@zsm/core'
import type { CreateExecutionFeedbackRequest, Execution, ExecutionFeedback } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { mediaUpload } from '../../app/api/client'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { normalizeUploadableImage, readLocalImage } from '../capture/local-file'
import { uploadMedia } from '../../app/api/media-upload'
import { createIdempotencyKey } from '../../app/keys'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import Pill from '../../components/pill'
import PrimaryButton from '../../components/primary-button'
import { canSubmitExecutionFeedback } from '../execution/model'
import { useDisplayablePath } from '../../hooks/use-displayable-path'
import { executionFeedbackBody } from './model'
import './index.scss'

interface ExecutionFeedbackScreenProps {
  executionId: string
}

type ExecutionTag = CreateExecutionFeedbackRequest['tags'][number]

export default function ExecutionFeedbackScreen({ executionId }: ExecutionFeedbackScreenProps) {
  const [execution, setExecution] = useState<Execution | null>(() =>
    resourceCache.read<Execution>(resourceKey('execution', executionId)) ?? null,
  )
  const [failed, setFailed] = useState(false)
  const [tags, setTags] = useState<ExecutionTag[]>([])
  const [comment, setComment] = useState('')
  // 实拍：上传成功才有 asset id；本地路径只用于预览
  const [mediaAssetId, setMediaAssetId] = useState<string | null>(null)
  const [localPreview, setLocalPreview] = useState('')
  // 工具新渲染层按 CORS 拦截 http://tmp/：预览用换出值，上传仍用原路径
  const localPreviewDisplay = useDisplayablePath(localPreview)
  const [photoState, setPhotoState] = useState<'idle' | 'uploading' | 'failed'>('idle')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    setFailed(false)
    try {
      const next = await resourceCache.revalidate(resourceKey('execution', executionId), () =>
        qualityApi.getExecution(executionId),
      )
      setExecution(next)
    } catch {
      setFailed(true)
    }
  }, [executionId])

  useEffect(() => {
    void load()
  }, [load])

  // 反馈只对某一次具体的执行存在：没有 id 就回方案 tab
  useEffect(() => {
    if (!executionId) void Taro.switchTab({ url: '/pages/plans/index' })
  }, [executionId])

  const toggleTag = (value: ExecutionTag) => {
    setTags((prev) =>
      prev.includes(value) ? prev.filter((item) => item !== value) : [...prev, value],
    )
  }

  const pickPhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      sizeType: ['compressed'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (!file) return
        setPhotoState('uploading')
        void (async () => {
          try {
            // HEIC 等非 JPEG/PNG 先归一成 JPEG：预览与上传用同一条转换后的路径
            const path = await normalizeUploadableImage(file.tempFilePath)
            setLocalPreview(path)
            const image = await readLocalImage(path)
            const asset = await uploadMedia(mediaUpload, image, 'feedback')
            setMediaAssetId(asset.id)
            setPhotoState('idle')
          } catch {
            // 保留标签与文字；进入失败态，把两条路都摆在用户面前
            setMediaAssetId(null)
            setPhotoState('failed')
          }
        })()
      },
      fail: () => {
        /* 用户取消 */
      },
    })
  }

  const submit = async () => {
    if (!execution || busy || tags.length === 0) return
    setBusy(true)
    try {
      const result: ExecutionFeedback = await qualityApi.createExecutionFeedback(
        executionFeedbackBody({ execution_id: execution.id }, tags, comment.trim(), mediaAssetId),
        createIdempotencyKey(`exec-feedback:${execution.id}`),
      )
      Taro.showToast({
        title: feedbackAcknowledgement(result.acknowledgement_code as FeedbackAcknowledgementCode),
        icon: 'none',
      })
      // 下一次规划由服务端读 feedback memory；客户端不拼任何 Prompt，回清单即可
      setTimeout(() => {
        void Taro.navigateBack()
      }, 600)
    } catch (error) {
      const message = error instanceof PublicApiError && error.message ? error.message : FEEDBACK_SCREEN_COPY.submitFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  if (failed && !execution) {
    return (
      <ErrorState
        title={FEEDBACK_SCREEN_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => void load()}
      />
    )
  }

  if (!execution || !executionId) return null

  if (!canSubmitExecutionFeedback(execution)) {
    return (
      <EmptyState
        title={FEEDBACK_SCREEN_COPY.needCompleted}
        description={CHECKLIST_COPY.completedNote}
        actionText={ERROR_COPY.backAction}
        onAction={() => void Taro.navigateBack()}
      />
    )
  }

  return (
    <View className="feedback-screen">
      <Text className="feedback-screen__title fade-up">{FEEDBACK_SCREEN_COPY.executionTitle}</Text>

      <View className="feedback-screen__block fade-up delay-1">
        <Text className="feedback-screen__label">{FEEDBACK_SCREEN_COPY.tagsTitle}</Text>
        <View className="feedback-screen__tags">
          {EXECUTION_FEEDBACK_TAGS.map((tag) => (
            <Pill
              key={tag.value}
              label={tag.label}
              active={tags.includes(tag.value)}
              onClick={() => toggleTag(tag.value)}
            />
          ))}
        </View>
      </View>

      <View className="feedback-screen__block fade-up delay-1">
        <Text className="feedback-screen__label">{FEEDBACK_SCREEN_COPY.commentTitle}</Text>
        <Textarea
          className="feedback-screen__comment"
          value={comment}
          maxlength={500}
          placeholder={FEEDBACK_SCREEN_COPY.commentPlaceholder}
          onInput={(event) => setComment(event.detail.value)}
        />
      </View>

      <View className="feedback-screen__block fade-up delay-2">
        <Text className="feedback-screen__label">{FEEDBACK_SCREEN_COPY.photoTitle}</Text>
        {localPreview ? (
          <View className="feedback-screen__photo-wrap">
            <Image className="feedback-screen__photo" src={localPreviewDisplay} mode="aspectFill" />
            {photoState === 'uploading' ? (
              <View className="feedback-screen__photo-mask">
                <View className="spinner spinner--on-deep" />
                <Text className="feedback-screen__photo-mask-text">
                  {FEEDBACK_SCREEN_COPY.photoUploading}
                </Text>
              </View>
            ) : null}
          </View>
        ) : null}
        <View className="feedback-screen__photo-actions">
          <Text className="pressable feedback-screen__photo-action" onClick={pickPhoto}>
            {localPreview ? FEEDBACK_SCREEN_COPY.retakePhoto : FEEDBACK_SCREEN_COPY.addPhoto}
          </Text>
        </View>
        {photoState === 'failed' ? (
          <View className="feedback-screen__photo-failed">
            <Text className="feedback-screen__photo-failed-note">
              {FEEDBACK_SCREEN_COPY.photoFailedNote}
            </Text>
            <Text
              className="pressable feedback-screen__photo-failed-skip"
              onClick={() => {
                // 「先提交文字和标签」：清掉失败的照片，media 段整个省略
                setLocalPreview('')
                setMediaAssetId(null)
                setPhotoState('idle')
              }}
            >
              {FEEDBACK_SCREEN_COPY.submitWithoutPhoto}
            </Text>
          </View>
        ) : null}
      </View>

      <View className="feedback-screen__foot fade-up delay-3">
        <PrimaryButton
          text={FEEDBACK_SCREEN_COPY.submitAction}
          disabled={tags.length === 0}
          loading={busy}
          onClick={() => void submit()}
        />
      </View>
    </View>
  )
}
