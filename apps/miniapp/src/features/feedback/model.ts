// 两条反馈闭环的纯逻辑：请求体构造 + 标签的双向映射。
// 不碰 Taro、不碰网络；所有中文来自 @zsm/core。
//
// 两条铁律：
// 1. tags 就是契约枚举本身——中文标签只活在界面，服务端收到必须是 enum 值；
// 2. 没有上传成功的照片就整段省略 media_asset_id——契约里它是 uuid 字符串，
//    不允许 null，占位符只会换来 400。
// 3. 记忆型标签必须映射成结构化 preference 一并发送——服务端的偏好记忆只从
//    preference 派生（从不解析自由文本），只发标签等于白选。
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
type ExecutionPreference = NonNullable<CreateExecutionFeedbackRequest['preference']>

/**
 * 记忆型标签 → 服务端 StructuredPreference 的确定性映射（与服务端
 * NormalizePreference 的四个 case 一一对应）。契约一次只带一条 preference，
 * 多选时按此表顺序取第一条命中的。
 *
 * dislike_color / want_to_keep 的记忆还需要 value（避开哪个颜色、保留哪个做法），
 * 界面目前不收这个信息，只发 kind+category——服务端拿不到 value 就不落记忆，
 * 等界面补了具体输入再带上，不在这里编造。
 */
const MEMORY_TAG_PREFERENCE: ReadonlyArray<readonly [ExecutionTag, ExecutionPreference]> = [
  ['too_formal', { kind: 'less_formal', category: 'overall' }],
  ['too_complex', { kind: 'simplify', category: 'overall' }],
  ['dislike_color', { kind: 'avoid', category: 'color' }],
  ['want_to_keep', { kind: 'preserve', category: 'outfit' }],
]

/** 已选标签里的第一条记忆型标签 → preference；没有记忆型标签返回 null（整段省略）。 */
export function memoryPreferenceOf(tags: readonly ExecutionTag[]): ExecutionPreference | null {
  for (const [tag, preference] of MEMORY_TAG_PREFERENCE) {
    if (tags.includes(tag)) return preference
  }
  return null
}

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

/** 执行反馈请求体。省略规则同上；记忆型标签折算成 preference 一并发送。 */
export function executionFeedbackBody(
  view: { execution_id: string },
  tags: readonly ExecutionTag[],
  comment: string,
  mediaAssetId: string | null,
): CreateExecutionFeedbackRequest {
  const preference = memoryPreferenceOf(tags)
  return {
    execution_id: view.execution_id,
    tags: [...tags],
    ...(comment ? { comment } : {}),
    ...(mediaAssetId ? { media_asset_id: mediaAssetId } : {}),
    ...(preference ? { preference } : {}),
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
