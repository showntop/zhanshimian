// G1 补充资料：一卡三段、可跳过，避免「说可选但实际有默认值」的矛盾。
// 提交 = PUT /v1/me/profile 持久化 + 带快照进分析（createAnalysis.profile）。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Slider, Text, View } from '@tarojs/components'
import { PROFILE_SETUP_COPY, type UserProfile } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Pill from '../../components/pill'
import './index.scss'

const ROLES = ['产品经理', '设计师', '咨询顾问', '学生', PROFILE_SETUP_COPY.skip]
const BUDGETS = ['500 以内', '500–1500', '1500 以上', PROFILE_SETUP_COPY.skip]
const HEIGHT_MIN = 145
const HEIGHT_MAX = 185

export default function ProfileSetup() {
  const [scene, setScene] = useState('general')
  const [mediaIds, setMediaIds] = useState<string[]>([])
  const [height, setHeight] = useState(165)
  const [role, setRole] = useState<string>(PROFILE_SETUP_COPY.skip)
  const [budget, setBudget] = useState<string>(PROFILE_SETUP_COPY.skip)
  const [busy, setBusy] = useState(false)
  const photosReady = mediaIds.length === 3

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
    if (options?.media_ids) setMediaIds(options.media_ids.split(',').filter(Boolean))
  })

  const submit = async (skip = false) => {
    if (!photosReady || busy) {
      Taro.showToast({ title: '请先回到上一步补齐三张照片', icon: 'none' })
      return
    }
    setBusy(true)
    // 当前服务端契约要求 role/budget 为非空字符串；跳过时用明确哨兵值，不在 UI 显示成默认身份。
    const profile: UserProfile = {
      height_cm: height,
      role: skip || role === PROFILE_SETUP_COPY.skip ? '未填写' : role,
      budget: skip || budget === PROFILE_SETUP_COPY.skip ? '未填写' : budget,
    }
    try {
      // 持久化失败不阻塞：分析仍带当次快照
      await api.updateMyProfile(profile).catch(() => undefined)
      const { data } = await api.createAnalysis({ scene, media_ids: mediaIds, profile })
      // 建档链路是一次正向流程，重开分析页可避免失败后回到过期资料页。
      Taro.reLaunch({ url: `/pages/analysis/index?id=${data.id}` })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '提交没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <View className="page">
      <AppHeader
        title="补充资料"
        back
        right={
          <Text className="psetup__header-skip pressable" onClick={() => void submit(true)}>
            跳过
          </Text>
        }
      />
      <View className="psetup">
        <View className="psetup__intro fade-up">
          <Text className="psetup__eyebrow">{PROFILE_SETUP_COPY.eyebrow}</Text>
          <Text className="psetup__title">{PROFILE_SETUP_COPY.title}</Text>
          <Text className="psetup__lede">{PROFILE_SETUP_COPY.lede}</Text>
        </View>

        <View className="psetup__card fade-up delay-1">
          <View className="psetup__photos">
            <Text className="psetup__photos-text">{PROFILE_SETUP_COPY.photosReady}</Text>
            <Text className={photosReady ? 'psetup__photos-state' : 'psetup__photos-state psetup__photos-state--missing'}>
              {photosReady ? '已就绪' : `缺 ${3 - mediaIds.length} 张`}
            </Text>
          </View>

          <View className="psetup__field">
            <View className="psetup__field-head">
              <Text className="psetup__field-name">{PROFILE_SETUP_COPY.height}</Text>
              <Text className="psetup__field-note">{PROFILE_SETUP_COPY.heightNote}</Text>
            </View>
            <Text className="psetup__height">{height} cm</Text>
            <Slider
              className="psetup__slider"
              min={HEIGHT_MIN}
              max={HEIGHT_MAX}
              step={1}
              value={height}
              activeColor="#587344"
              backgroundColor="#DDE5D7"
              blockSize={24}
              blockColor="#FFFFFF"
              onChange={(event) => setHeight(Number(event.detail.value))}
            />
            <View className="psetup__stepper">
              <Text
                className="psetup__step pressable"
                onClick={() => setHeight((h) => Math.max(HEIGHT_MIN, h - 1))}
              >
                −
              </Text>
              <Text className="psetup__range">{HEIGHT_MIN}–{HEIGHT_MAX} cm</Text>
              <Text
                className="psetup__step pressable"
                onClick={() => setHeight((h) => Math.min(HEIGHT_MAX, h + 1))}
              >
                ＋
              </Text>
            </View>
          </View>

          <View className="psetup__field">
            <Text className="psetup__field-name">{PROFILE_SETUP_COPY.role}</Text>
            <View className="psetup__chips">
              {ROLES.map((item) => (
                <Pill key={item} label={item} active={role === item} onClick={() => setRole(item)} />
              ))}
            </View>
          </View>

          <View className="psetup__field">
            <Text className="psetup__field-name">{PROFILE_SETUP_COPY.budget}</Text>
            <View className="psetup__chips">
              {BUDGETS.map((item) => (
                <Pill key={item} label={item} active={budget === item} onClick={() => setBudget(item)} />
              ))}
            </View>
          </View>
        </View>

        <View className="psetup__why fade-up delay-2">
          <Text className="psetup__why-title">{PROFILE_SETUP_COPY.whyTitle}</Text>
          <Text className="psetup__why-body">{PROFILE_SETUP_COPY.whyBody}</Text>
        </View>

        <View className="psetup__foot fade-up delay-3">
          <PrimaryButton
            text={PROFILE_SETUP_COPY.primaryAction}
            disabled={!photosReady}
            loading={busy}
            onClick={submit}
          />
          <Text className="psetup__skip pressable" onClick={() => void submit(true)}>
            {PROFILE_SETUP_COPY.skipAction} ›
          </Text>
          <Text className="psetup__privacy">{PROFILE_SETUP_COPY.privacy}</Text>
        </View>
      </View>
    </View>
  )
}
