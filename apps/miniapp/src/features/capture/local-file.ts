// 刚拍/刚选的照片 → 上传事务要的 LocalImageFile。
//
// 为什么必须真算 SHA-256：服务端 createIntent 只收 64 位小写 hex（`^[a-f0-9]{64}$`），
// completeUploadIntent 还会拿存储侧元数据里的 sha256 跟 intent 对账，对不上直接拒。
// 所以这里算错 = 上传必然失败，不能图省事塞个假摘要。
import Taro from '@tarojs/taro'
import type { LocalImageFile } from '../../app/api/media-upload'

/**
 * 微信运行时的 `getFileInfo` 支持 `digestAlgorithm: 'sha256'`，但 Taro 4.2.1 的类型
 * 只声明了 `'md5' | 'sha1'`（node_modules/@tarojs/taro/types/api/files/index.d.ts）。
 * 这里做一次窄化断言，并在下面立刻校验摘要形状：断言若失效会当场抛错，
 * 不会带着一条假摘要去换一个必定失败的 intent。
 */
const SHA256_ALGORITHM = 'sha256' as 'md5'

const SHA256_PATTERN = /^[a-f0-9]{64}$/

/** 仅按扩展名判断；判不出来按 jpeg——上传用途里两可的图都允许 jpeg。 */
export function mimeTypeOf(filePath: string): 'image/jpeg' | 'image/png' {
  return /\.png$/i.test(filePath) ? 'image/png' : 'image/jpeg'
}

export async function readLocalImage(filePath: string): Promise<LocalImageFile> {
  const info = await getFileInfo(filePath)
  const sha256 = info.digest.trim().toLowerCase()
  if (!SHA256_PATTERN.test(sha256)) {
    throw new Error('capture: 没有拿到可用的 sha256 摘要')
  }
  if (!(info.size > 0)) {
    throw new Error('capture: 照片是空文件')
  }
  return {
    path: filePath,
    name: basename(filePath),
    mimeType: mimeTypeOf(filePath),
    byteSize: info.size,
    sha256,
  }
}

function getFileInfo(filePath: string): Promise<{ size: number; digest: string }> {
  return new Promise((resolve, reject) => {
    Taro.getFileSystemManager().getFileInfo({
      filePath,
      digestAlgorithm: SHA256_ALGORITHM,
      // Taro 把 digest 声明成可选（GetFileInfoSuccessCallbackResult.digest?: string），
      // 但空摘要不可能通过上面的 SHA256_PATTERN——所以这里归一成空串交给调用方拒掉，
      // 而不是在这里放宽成"没有摘要也算成功"。
      success: (res) => resolve({ size: res.size, digest: res.digest ?? '' }),
      fail: (err) => reject(new Error(err?.errMsg || 'capture: 读取照片信息失败')),
    })
  })
}

function basename(filePath: string): string {
  const parts = filePath.split('/')
  return parts[parts.length - 1] || 'photo'
}
