// body-orbit Lite：先播环绕视频，再拖转盘看静帧；满 8 帧才出对比滑杆。
import { useMemo, useRef, useState } from 'react'
import { Image, Text, Video, View } from '@tarojs/components'
import type { CommonEvent, ITouchEvent } from '@tarojs/components'
import { LAB_COPY, lookImage, lookVideo, userImage, type BodyPresentation } from '@zsm/core'
import CompareSlider from '../compare-slider'
import ExampleImage from '../example-image'
import './index.scss'

interface BodyViewerProps {
  presentation: BodyPresentation
  bodyImageURL: string
  badgeText: string
}

const FRAME_STEP_PX = 18

export default function BodyViewer({ presentation, bodyImageURL, badgeText }: BodyViewerProps) {
  const video = lookVideo(presentation.orbit.video_url)
  const frames = useMemo(
    () =>
      presentation.orbit.frames
        .map((f) => ({ ...f, url: lookImage(f.url) }))
        .filter((f) => f.url),
    [presentation.orbit.frames],
  )

  const [mode, setMode] = useState<'video' | 'turntable'>(() =>
    video ? 'video' : frames.length >= 1 ? 'turntable' : 'turntable',
  )
  const [index, setIndex] = useState(0)
  const touchStartRef = useRef({ x: 0, index: 0 })

  const onVideoTouchStart = () => {
    setMode('turntable')
  }

  const onTurntableTouchStart = (e: CommonEvent) => {
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    touchStartRef.current = { x: touch.clientX, index }
  }

  const onTurntableTouchMove = (e: CommonEvent) => {
    if (frames.length === 0) return
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    const deltaX = touch.clientX - touchStartRef.current.x
    const steps = Math.floor(Math.abs(deltaX) / FRAME_STEP_PX)
    if (steps === 0) return
    const dir = deltaX >= 0 ? -1 : 1
    const raw = touchStartRef.current.index + dir * steps
    const normalized = ((raw % frames.length) + frames.length) % frames.length
    if (normalized !== index) setIndex(normalized)
  }

  const showVideo = mode === 'video' && Boolean(video)
  const showTurntable = !showVideo && frames.length > 0
  const showCompare = frames.length >= 8
  const showNoCompareHint = Boolean(video) && frames.length === 0
  const activeFrame = showTurntable ? frames[index] : null
  const compareFrame = showCompare ? frames[0] : null

  return (
    <View className="body-viewer">
      <View className="body-viewer__stage">
        {showVideo ? (
          <Video
            className="body-viewer__video"
            src={video}
            muted
            autoplay
            controls={false}
            showCenterPlayBtn={false}
            objectFit="contain"
            onEnded={() => setMode('turntable')}
            onTouchStart={onVideoTouchStart}
          />
        ) : null}
        {showTurntable && activeFrame ? (
          <View
            className="body-viewer__turntable"
            onTouchStart={onTurntableTouchStart}
            onTouchMove={onTurntableTouchMove}
            catchMove
          >
            <Image
              key={`${activeFrame.yaw}-${activeFrame.url}`}
              className="body-viewer__frame"
              src={activeFrame.url}
              mode="aspectFit"
              lazyLoad={false}
            />
          </View>
        ) : null}
      </View>

      {showCompare && compareFrame ? (
        <View className="body-viewer__compare">
          <CompareSlider
            current={<ExampleImage user src={userImage(bodyImageURL)} anchor="top" />}
            plan={<ExampleImage src={compareFrame.url} badgeText={badgeText} anchor="top" />}
          />
        </View>
      ) : null}

      {showNoCompareHint ? (
        <Text className="body-viewer__hint">{LAB_COPY.noCompare}</Text>
      ) : null}
    </View>
  )
}
