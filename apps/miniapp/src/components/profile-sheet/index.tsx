import { useEffect, useState } from 'react'
import { Input, ScrollView, Slider, Text, View } from '@tarojs/components'
import Taro from '@tarojs/taro'
import { PROFILE_SETUP_COPY, type UserProfile } from '@zsm/core'
import { api } from '../../services/api'
import Pill from '../pill'
import PrimaryButton from '../primary-button'
import './index.scss'

const HEIGHT_MIN = 145
const HEIGHT_MAX = 185

interface ProfileSheetProps {
  profile: UserProfile | null
  onSaved: (profile: UserProfile) => void
}

function optionalNumber(value: string): number | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

function displayNumber(value?: number): string {
  return value ? String(value) : ''
}

function sheetScrollHeight() {
  const sys = Taro.getSystemInfoSync()
  const rpx = sys.windowWidth / 750
  const safeBottom = sys.safeArea ? Math.max(0, sys.screenHeight - sys.safeArea.bottom) : 0
  const chrome = 360 * rpx + safeBottom
  return Math.max(240, Math.round(sys.windowHeight * 0.85 - chrome))
}

export default function ProfileSheet({ profile, onSaved }: ProfileSheetProps) {
  const [height, setHeight] = useState(profile?.height_cm || 165)
  const [role, setRole] = useState(profile?.role && profile.role !== '未填写' ? profile.role : PROFILE_SETUP_COPY.skip)
  const [budget, setBudget] = useState(
    profile?.budget && profile.budget !== '未填写' ? profile.budget : PROFILE_SETUP_COPY.skip,
  )
  const [weight, setWeight] = useState(displayNumber(profile?.weight_kg))
  const [bust, setBust] = useState(displayNumber(profile?.bust_cm))
  const [waist, setWaist] = useState(displayNumber(profile?.waist_cm))
  const [hip, setHip] = useState(displayNumber(profile?.hip_cm))
  const [busy, setBusy] = useState(false)
  const [scrollHeight] = useState(sheetScrollHeight)

  useEffect(() => {
    setHeight(profile?.height_cm || 165)
    setRole(profile?.role && profile.role !== '未填写' ? profile.role : PROFILE_SETUP_COPY.skip)
    setBudget(profile?.budget && profile.budget !== '未填写' ? profile.budget : PROFILE_SETUP_COPY.skip)
    setWeight(displayNumber(profile?.weight_kg))
    setBust(displayNumber(profile?.bust_cm))
    setWaist(displayNumber(profile?.waist_cm))
    setHip(displayNumber(profile?.hip_cm))
  }, [profile])

  const save = async () => {
    if (busy) return
    setBusy(true)
    const next: UserProfile = {
      height_cm: height,
      role: role === PROFILE_SETUP_COPY.skip ? '未填写' : role,
      budget: budget === PROFILE_SETUP_COPY.skip ? '未填写' : budget,
      weight_kg: optionalNumber(weight),
      bust_cm: optionalNumber(bust),
      waist_cm: optionalNumber(waist),
      hip_cm: optionalNumber(hip),
    }
    try {
      const saved = await api.updateMyProfile(next)
      Taro.showToast({ title: PROFILE_SETUP_COPY.saved, icon: 'success' })
      onSaved(saved)
    } catch (error) {
      Taro.showToast({ title: (error as Error).message || '保存没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <View className="profile-sheet">
      <ScrollView className="profile-sheet__scroll" style={{ height: `${scrollHeight}px` }} scrollY enhanced showScrollbar={false}>
      <View className="profile-sheet__fields">
      <View className="profile-sheet__field">
        <Text className="profile-sheet__label">{PROFILE_SETUP_COPY.height}</Text>
        <Text className="profile-sheet__height">{height} cm</Text>
        <Slider
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
        <View className="profile-sheet__stepper">
          <Text className="profile-sheet__step pressable" onClick={() => setHeight((value) => Math.max(HEIGHT_MIN, value - 1))}>
            −
          </Text>
          <Text className="profile-sheet__hint">{HEIGHT_MIN}–{HEIGHT_MAX} cm</Text>
          <Text className="profile-sheet__step pressable" onClick={() => setHeight((value) => Math.min(HEIGHT_MAX, value + 1))}>
            ＋
          </Text>
        </View>
      </View>

      <View className="profile-sheet__field">
        <Text className="profile-sheet__label">{PROFILE_SETUP_COPY.role}</Text>
        <View className="profile-sheet__chips">
          {[...PROFILE_SETUP_COPY.roles, PROFILE_SETUP_COPY.skip].map((item) => (
            <Pill key={item} label={item} active={role === item} onClick={() => setRole(item)} />
          ))}
        </View>
      </View>

      <View className="profile-sheet__field">
        <Text className="profile-sheet__label">{PROFILE_SETUP_COPY.budget}</Text>
        <View className="profile-sheet__chips">
          {[...PROFILE_SETUP_COPY.budgets, PROFILE_SETUP_COPY.skip].map((item) => (
            <Pill key={item} label={item} active={budget === item} onClick={() => setBudget(item)} />
          ))}
        </View>
      </View>

      <View className="profile-sheet__field">
        <Text className="profile-sheet__label">{PROFILE_SETUP_COPY.optional}</Text>
        <View className="profile-sheet__measures">
          <View className="profile-sheet__measure">
            <Text className="profile-sheet__hint">{PROFILE_SETUP_COPY.weight}</Text>
            <Input className="profile-sheet__input" type="digit" placeholder="kg" value={weight} onInput={(event) => setWeight(event.detail.value)} />
          </View>
          <View className="profile-sheet__measure">
            <Text className="profile-sheet__hint">{PROFILE_SETUP_COPY.bust}</Text>
            <Input className="profile-sheet__input" type="digit" placeholder="cm" value={bust} onInput={(event) => setBust(event.detail.value)} />
          </View>
          <View className="profile-sheet__measure">
            <Text className="profile-sheet__hint">{PROFILE_SETUP_COPY.waist}</Text>
            <Input className="profile-sheet__input" type="digit" placeholder="cm" value={waist} onInput={(event) => setWaist(event.detail.value)} />
          </View>
          <View className="profile-sheet__measure">
            <Text className="profile-sheet__hint">{PROFILE_SETUP_COPY.hip}</Text>
            <Input className="profile-sheet__input" type="digit" placeholder="cm" value={hip} onInput={(event) => setHip(event.detail.value)} />
          </View>
        </View>
      </View>
      </View>
      </ScrollView>

      <View className="profile-sheet__foot">
        <PrimaryButton text={PROFILE_SETUP_COPY.save} loading={busy} onClick={save} />
      </View>
    </View>
  )
}
