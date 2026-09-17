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

/**
 * chooseMedia 的产物不保证能上传：iOS 相册的高效格式（HEIC）会原样透传成
 * .heic 临时文件，服务端在 complete 时按字节嗅探（不信扩展名）直接拒——
 * 「仅支持 JPEG 或 PNG」。这里把一切非 jpg/png 的临时文件走一次
 * compressImage 转成 JPEG（微信压缩输出就是 jpg）；转换失败交回原路径，
 * 让服务端的公开错误文案兜底，而不是在客户端猜格式。
 */
export async function normalizeUploadableImage(filePath: string): Promise<string> {
  if (/\.(jpe?g|png)$/i.test(filePath)) return filePath
  try {
    const res = await Taro.compressImage({ src: filePath, quality: 80 })
    return res.tempFilePath || filePath
  } catch {
    return filePath
  }
}

/**
 * 本地路径 → 可渲染路径（显示专用，上传/落盘仍用原路径）。
 *
 * 微信开发者工具 lib 3.17.1 起的新渲染层把 http://tmp/、http://usr/ 这两个
 * 本地文件模拟主机名重定向到 127.0.0.1:PORT/__tmp__/ 再按 CORS 拦截，
 * <image> 直接裂图；真机的 wxfile:// 不走这条路，无需处理。只对工具的
 * 模拟路径读文件转 base64 data URL（工具里文件就在本机，开销可接受），
 * 转换失败交回原路径。同一字符串只转一次。
 */
const displayUrlCache = new Map<string, string>()

export function isDevtoolsLocalPath(filePath: string): boolean {
  return /^http:\/\/(tmp|usr)\//.test(filePath)
}

export async function displayableImagePath(filePath: string): Promise<string> {
  if (!isDevtoolsLocalPath(filePath)) return filePath
  const cached = displayUrlCache.get(filePath)
  if (cached) return cached
  try {
    const data = await new Promise<ArrayBuffer>((resolve, reject) => {
      Taro.getFileSystemManager().readFile({
        filePath,
        success: (res) => resolve(res.data as ArrayBuffer),
        fail: (e) => reject(new Error(e.errMsg || '读取照片失败')),
      })
    })
    const url = `data:${mimeTypeOf(filePath)};base64,${Taro.arrayBufferToBase64(data)}`
    displayUrlCache.set(filePath, url)
    return url
  } catch {
    return filePath
  }
}

/**
 * 把 chooseMedia 的临时文件沉淀到本地持久目录。
 *
 * 开发者工具的临时路径是 `http://tmp/...`：它是工具的本地文件模拟，saveFile 后
 * 变成 `http://usr/...`（对应真机的 wxfile://usr）——两者都在投影白名单内
 * （见 media/display.ts 的 isDevtoolsLocalFile）。真机 chooseMedia 本就返回
 * wxfile://，直通。
 *
 * 顺带解决重传死路：临时文件会被微信清理，保存后的路径在「重传这一份」时仍在。
 * 保存失败返回原路径：上传不受影响，开发者工具里预览维持空态（与改造前一致）。
 */
export async function stabilizeLocalPath(filePath: string): Promise<string> {
  if (filePath.startsWith('wxfile://') || filePath.startsWith('file://')) return filePath
  return new Promise((resolve) => {
    Taro.getFileSystemManager().saveFile({
      tempFilePath: filePath,
      success: (res) => resolve(res.savedFilePath || filePath),
      fail: () => resolve(filePath),
    })
  })
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
