// 上传事务：intent → 直传 → complete。三步都成功才产生 Asset。
// 任何一步失败都向上抛错，绝不落一个本地假 Asset——本地路径只在本次会话里用于预览。
import type { UploadIntent, UploadedMediaAsset } from '@zsm/core'

export type MediaPurpose = 'face' | 'side' | 'body' | 'feedback' | 'wardrobe'

export type LocalImageFile = {
  /** 本地临时文件路径（拍照/选择后的 tempFilePath） */
  path: string
  /** 本地文件名，仅用于日志排查，不上传 */
  name: string
  mimeType: 'image/jpeg' | 'image/png'
  byteSize: number
  sha256: string
}

export type CreateIntentInput = {
  purpose: MediaPurpose
  mime_type: 'image/jpeg' | 'image/png'
  byte_size: number
  sha256: string
}

export interface MediaUploadPort {
  createIntent(input: CreateIntentInput): Promise<UploadIntent>
  putObject(intent: UploadIntent, filePath: string): Promise<void>
  completeIntent(intentId: string): Promise<UploadedMediaAsset>
}

export async function uploadMedia(
  port: MediaUploadPort,
  file: LocalImageFile,
  purpose: MediaPurpose,
): Promise<UploadedMediaAsset> {
  const intent = await port.createIntent({
    purpose,
    mime_type: file.mimeType,
    byte_size: file.byteSize,
    sha256: file.sha256,
  })
  await port.putObject(intent, file.path)
  return port.completeIntent(intent.id)
}
