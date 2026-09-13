// 两条反馈闭环的纯逻辑：请求体构造 + 标签的双向映射。
// 不碰 Taro、不碰网络；所有中文来自 @zsm/core。
//
// 两条铁律：
// 1. tags 就是契约枚举本身——中文标签只活在界面，服务端收到必须是 enum 值；
// 2. 没有上传成功的照片就整段省略 media_asset_id——契约里它是 uuid 字符串，
//    不允许 null，占位符只会换来 400。
import {
  EXECUTION_FEEDBACK_TAGS,
  GENERATION_FEEDBACK_TAGS,
} from '@zsm/core'
import type {
  CreateExecutionFeedbackRequest,
  CreateGenerationFeedbackRequest,
} from '@zsm/core'

type GenerationTag = CreateGenerationFeedbackRequest['tags'][number]
type ExecutionTag = CreateExecutionFeedbackRequest['tags'][number]

/**
 * 生成反馈请求体。`comment` 为空、`mediaAssetId` 为 null 时整段省略——
 * 服务端的注释写得很清楚："上传失败就省略 media_asset_id"。
 */
export function generationFeedbackBody(
  view: { publication_id: string },
  tags: readonly GenerationTag[],
  comment: string,
  mediaAssetId: string | null,
): CreateGenerationFeedbackRequest {
  return {
    publication_id: view.publication_id,
    tags: [...tags],
    ...(comment ? { comment } : {}),
    ...(mediaAssetId ? { media_asset_id: mediaAssetId } : {}),
  }
}

/** 执行反馈请求体。省略规则同上。 */
export function executionFeedbackBody(
  view: { execution_id: string },
  tags: readonly ExecutionTag[],
  comment: string,
  mediaAssetId: string | null,
): CreateExecutionFeedbackRequest {
  return {
    execution_id: view.execution_id,
    tags: [...tags],
    ...(comment ? { comment } : {}),
    ...(mediaAssetId ? { media_asset_id: mediaAssetId } : {}),
  }
}

/** 契约枚举 → 界面中文；表外值显示原值，不抛错也不编标签。 */
export function generationTagLabel(value: string): string {
  const found = GENERATION_FEEDBACK_TAGS.find((tag) => tag.value === value)
  return found?.label ?? value
}

export function executionTagLabel(value: string): string {
  const found = EXECUTION_FEEDBACK_TAGS.find((tag) => tag.value === value)
  return found?.label ?? value
}
