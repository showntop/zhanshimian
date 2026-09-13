// 发起一次分析：受理 → 落缓存 → 跳进度页。拍摄页与进度页的「重新发起」共用这一条路径。
//
// 之所以只有一份：受理成功之后有两件必须一起做的事——Operation 进缓存（进度页第一帧
// 就有东西可显示）和三张照片进缓存（进度页唯一能拿到照片的途径，契约里 Assessment
// 不带媒体）。两处各写一遍的话，某天只会改其中一处，表现是「偶尔分析页没有照片」，
// 而且只在冷启动时复现。
import Taro from '@tarojs/taro'
import { CAPTURE_COPY } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { toAssessmentInput, type PhotosByRole } from '../capture/model'
import { assessmentRoute } from './model'

/** 受理失败时优先说服务端愿意公开的话，没有就用自己的兜底文案。 */
export function assessmentSubmitErrorText(error: unknown): string {
  if (error instanceof PublicApiError && error.message) return error.message
  return CAPTURE_COPY.submitFailed
}

/**
 * 提交三张照片并进入进度页。`idempotencyKey` 由调用方决定：
 * 首次提交用绑在照片上的那把键（网络重试要复用），失败后的「重新发起」必须换一把
 * （见 `assessmentRetryKey` 的说明）。
 */
export async function submitAssessment(photos: PhotosByRole, idempotencyKey: string): Promise<void> {
  const accepted = await qualityApi.createAssessment(toAssessmentInput(photos), idempotencyKey)
  resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
  resourceCache.write(resourceKey('assessment-photos', accepted.data.id), photos)
  await Taro.redirectTo({ url: assessmentRoute(accepted) })
}
