// 本地照片路径的显示化：开发者工具新渲染层把 http://tmp/、http://usr/ 按
// CORS 拦截（真机 wxfile:// 不受影响），见 features/capture/local-file.ts 的
// displayableImagePath。状态/草稿里存原路径（上传与跨页恢复要用），
// 渲染用这里换出的值；工具模拟路径在换出完成前先给空串，避免闪裂图。
import { useEffect, useState } from 'react'
import { displayableImagePath, isDevtoolsLocalPath } from '../features/capture/local-file'

export function useDisplayablePath(path: string): string {
  const [display, setDisplay] = useState(() => (isDevtoolsLocalPath(path) ? '' : path))

  useEffect(() => {
    if (isDevtoolsLocalPath(path)) {
      setDisplay('')
      let alive = true
      void displayableImagePath(path).then((next) => {
        if (alive) setDisplay(next)
      })
      return () => {
        alive = false
      }
    }
    setDisplay(path)
  }, [path])

  return display
}
