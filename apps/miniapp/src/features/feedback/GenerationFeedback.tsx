// 生成反馈：挂在方案详情 ready 形象图上的入口 + 底部弹层。
// render 没发布就没有这张 publication，也就没有反馈入口——反馈必须绑定
// 用户真正看到的那一张图，链条由服务端从 publication 反推。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  FEEDBACK_SCREEN_COPY,
  GENERATION_FEEDBACK_TAGS,
  feedbackAcknowledgement,
  type FeedbackAcknowledgementCode,
} from '@zsm/core'
import type { CreateGenerationFeedbackRequest, GenerationFeedback } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { createIdempotencyKey } from '../../app/keys'
import BottomSheet from '../../components/bottom-sheet'
import Pill from '../../components/pill'
import PrimaryButton from '../../components/primary-button'
import { generationFeedbackBody } from './model'

type GenerationTag = CreateGenerationFeedbackRequest['tags'][number]

interface GenerationFeedbackProps {
  /** 用户看到的那张形象图的 publication id；未发布时不渲染入口 */
  publicationId: string | null
  className?: string
}

export default function GenerationFeedback({ publicationId, className = '' }: GenerationFeedbackProps) {
  const [open, setOpen] = useState(false)
  const [tags, setTags] = useState<GenerationTag[]>([])
  const [busy, setBusy] = useState(false)

  if (!publicationId) return null

  const toggleTag = (value: GenerationTag) => {
    setTags((prev) =>
      prev.includes(value) ? prev.filter((item) => item !== value) : [...prev, value],
    )
  }

  const submit = async () => {
    if (busy || tags.length === 0) return
    setBusy(true)
    try {
      const result: GenerationFeedback = await qualityApi.createGenerationFeedback(
        generationFeedbackBody({ publication_id: publicationId }, tags, '', null),
        createIdempotencyKey(`gen-feedback:${publicationId}`),
      )
      setOpen(false)
      setTags([])
      Taro.showToast({
        title: feedbackAcknowledgement(result.acknowledgement_code as FeedbackAcknowledgementCode),
        icon: 'none',
      })
    } catch {
      Taro.showToast({ title: FEEDBACK_SCREEN_COPY.submitFailed, icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Text className={`pressable ${className}`.trim()} onClick={() => setOpen(true)}>
        {FEEDBACK_SCREEN_COPY.generationEntry}
      </Text>
      <BottomSheet
        open={open}
        title={FEEDBACK_SCREEN_COPY.generationTitle}
        description={FEEDBACK_SCREEN_COPY.generationNote}
        onClose={() => setOpen(false)}
      >
        <View className="feedback-sheet">
          <Text className="feedback-sheet__label">{FEEDBACK_SCREEN_COPY.tagsTitle}</Text>
          <View className="feedback-sheet__tags">
            {GENERATION_FEEDBACK_TAGS.map((tag) => (
              <Pill
                key={tag.value}
                label={tag.label}
                active={tags.includes(tag.value)}
                onClick={() => toggleTag(tag.value)}
              />
            ))}
          </View>
          <PrimaryButton
            text={FEEDBACK_SCREEN_COPY.submitAction}
            disabled={tags.length === 0}
            loading={busy}
            onClick={() => void submit()}
          />
        </View>
      </BottomSheet>
    </>
  )
}
