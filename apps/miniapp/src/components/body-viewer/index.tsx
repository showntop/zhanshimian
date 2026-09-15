// body-orbit Lite：舞台上先播一圈，再拖静帧；满 8 帧且非 Demo 才出对比。
import { useMemo, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, Video, View } from '@tarojs/components'
import type { CommonEvent, ITouchEvent } from '@tarojs/components'
import { LAB_COPY, lookImage, lookVideo, type BodyPresentation, type DisplayMedia } from '@zsm/core'
import CompareSlider from '../compare-slider'
import SourceImage from '../source-image'
import './index.scss'

interface BodyViewerProps {
  presentation: BodyPresentation
  /** 对比滑杆底层「原本」：服务端下发的带类型媒体，角标由投影决定。 */
  bodyMedia: DisplayMedia | null
  /** 上层生成帧的说法：demo 供应商 → 效果示例，否则 AI 风格预览。 */
  badgeText: string
}

const FRAME_STEP_PX = 16

function angleLabel(yaw: number): string {
  const y = ((yaw % 360) + 360) % 360
  if (y < 45 || y >= 315) return LAB_COPY.angleFront
  if (y < 135) return LAB_COPY.angleLeft
  if (y < 225) return LAB_COPY.angleBack
  return LAB_COPY.angleRight
}

function wrapIndex(raw: number, length: number): number {
  if (length <= 0) return 0
  return ((raw % length) + length) % length
}

function tapHaptic() {
  try {
    Taro.vibrateShort({ type: 'light' })
  } catch {
    /* 部分基础库无短振 */
  }
}

export default function BodyViewer({ presentation, bodyMedia, badgeText }: BodyViewerProps) {
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
  const [dragging, setDragging] = useState(false)
  const touchRef = useRef({ x: 0, leftover: 0 })

  const setFrame = (next: number, haptic: boolean) => {
    setIndex((prev) => {
      const normalized = wrapIndex(next, frames.length)
      if (normalized === prev) return prev
      if (haptic) tapHaptic()
      return normalized
    })
  }

  const enterTurntable = () => {
    setMode('turntable')
    setDragging(false)
  }

  const onTurntableTouchStart = (e: CommonEvent) => {
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    touchRef.current = { x: touch.clientX, leftover: 0 }
    setDragging(true)
  }

  const onTurntableTouchMove = (e: CommonEvent) => {
    if (frames.length === 0) return
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    const dx = touch.clientX - touchRef.current.x
    touchRef.current.x = touch.clientX
    let leftover = touchRef.current.leftover + dx
    let steps = 0
    while (leftover <= -FRAME_STEP_PX) {
      leftover += FRAME_STEP_PX
      steps += 1
    }
    while (leftover >= FRAME_STEP_PX) {
      leftover -= FRAME_STEP_PX
      steps -= 1
    }
    touchRef.current.leftover = leftover
    if (steps !== 0) {
      setIndex((prev) => {
        const next = wrapIndex(prev + steps, frames.length)
        if (next === prev) return prev
        tapHaptic()
        return next
      })
    }
  }

  const onTurntableTouchEnd = () => {
    setDragging(false)
  }

  // 演示判定只认服务端投影的 source_kind（红线：不按 provider_version 推断）。
  const isDemo = presentation.source_kind === 'demo_example'
  const showVideo = mode === 'video' && Boolean(video)
  const showTurntable = !showVideo && frames.length > 0
  const showCompare = !isDemo && frames.length >= 8
  const showNoCompareHint = Boolean(video) && frames.length === 0
  const canSteer = showTurntable && frames.length > 1
  const activeFrame = showTurntable ? frames[index] : null
  const compareFrame = showCompare ? frames[0] : null

  return (
    <View className="body-viewer">
      <View className={`body-viewer__stage ${dragging ? 'body-viewer__stage--dragging' : ''}`}>
        <View className="body-viewer__glow" />
        <View className="body-viewer__spot" />
        <View className="body-viewer__ring" />
        <View className="body-viewer__floor" />

        <View className="body-viewer__figure">
          {frames.length > 0 ? (
            <View className={`body-viewer__turntable ${showTurntable ? 'body-viewer__turntable--on' : ''}`}>
              {frames.map((frame, i) => (
                <Image
                  key={`${frame.yaw}-${frame.url}`}
                  className={`body-viewer__frame ${showTurntable && i === index ? 'body-viewer__frame--on' : ''}`}
                  src={frame.url}
                  mode="aspectFit"
                  lazyLoad={false}
                />
              ))}
            </View>
          ) : null}
          {showVideo ? (
            <Video
              className="body-viewer__video"
              src={video}
              muted
              autoplay
              controls={false}
              showCenterPlayBtn={false}
              showPlayBtn={false}
              showFullscreenBtn={false}
              showProgress={false}
              enableProgressGesture={false}
              objectFit="contain"
              onEnded={enterTurntable}
              onError={enterTurntable}
              onTouchStart={enterTurntable}
            />
          ) : null}
        </View>

        {showTurntable ? (
          <View
            className="body-viewer__pad"
            onTouchStart={onTurntableTouchStart}
            onTouchMove={onTurntableTouchMove}
            onTouchEnd={onTurntableTouchEnd}
            onTouchCancel={onTurntableTouchEnd}
            catchMove
          />
        ) : null}

        {canSteer ? (
          <View
            className="body-viewer__steer body-viewer__steer--prev pressable"
            ariaRole="button"
            ariaLabel={LAB_COPY.prevAngle}
            onClick={() => setFrame(index - 1, true)}
          >
            <Text className="body-viewer__steer-mark">‹</Text>
          </View>
        ) : null}
        {canSteer ? (
          <View
            className="body-viewer__steer body-viewer__steer--next pressable"
            ariaRole="button"
            ariaLabel={LAB_COPY.nextAngle}
            onClick={() => setFrame(index + 1, true)}
          >
            <Text className="body-viewer__steer-mark">›</Text>
          </View>
        ) : null}

        {badgeText ? <Text className="example-badge body-viewer__badge">{badgeText}</Text> : null}
        {activeFrame ? <Text className="body-viewer__angle">{angleLabel(activeFrame.yaw)}</Text> : null}
        {showTurntable ? (
          <Text className={`body-viewer__hint ${dragging ? 'body-viewer__hint--quiet' : ''}`}>
            {LAB_COPY.dragHint}
          </Text>
        ) : null}
      </View>

      {showCompare && compareFrame ? (
        <View className="body-viewer__compare">
          <CompareSlider
            single={!bodyMedia}
            current={bodyMedia ? <SourceImage className="body-viewer__compare-img" media={bodyMedia} anchor="top" /> : null}
            plan={
              <View className="body-viewer__plan">
                <Image className="body-viewer__compare-img" src={compareFrame.url} mode="aspectFill" />
                <Text className="example-badge">{badgeText}</Text>
              </View>
            }
          />
        </View>
      ) : null}

      {showNoCompareHint ? (
        <Text className="body-viewer__note">{LAB_COPY.noCompare}</Text>
      ) : null}
    </View>
  )
}
