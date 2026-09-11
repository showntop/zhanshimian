// 实拍反馈：上传今天实拍（必传，不静默丢弃）+ 感受多选 +
// G3 成功态：服务端返回的个性化承诺文案（message 引用方案名）。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { FEEDBACK_WORDS, lookImage } from '@zsm/core'
import type { Plan } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Pill from '../../components/pill'
import './index.scss'

export default function Feedback() {
  const [planId, setPlanId] = useState('')
  const [plan, setPlan] = useState<Plan | null>(null)
  const [photoPath, setPhotoPath] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [doneMessage, setDoneMessage] = useState('')
  const pageClass = usePageClass(true)

  useLoad(async (options) => {
    const id = options?.plan_id
    if (!id) return
    setPlanId(id)
    try {
      setPlan(await api.getPlan(id))
    } catch {
      /* 参考图缺失时保持空态 */
    }
  })

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) setPhotoPath(file.tempFilePath)
      },
    })
  }

  const toggleWord = (word: string) => {
    setSelected((prev) => (prev.includes(word) ? prev.filter((w) => w !== word) : [...prev, word]))
  }

  const submit = async () => {
    if (!planId) return
    if (!photoPath) {
      Taro.showToast({ title: '先上传今天的实拍', icon: 'none' })
      return
    }
    setBusy(true)
    try {
      // 实拍必须真实上传：失败即终止（不静默丢弃）
      const asset = await api.uploadMedia({ kind: 'feedback', filePath: photoPath })
      const ack = await api.sendPlanFeedback(planId, {
        tags: selected,
        comment: '',
        media_id: asset.id,
      })
      setDoneMessage(ack.message || '我们记住了什么对你有效')
      Taro.vibrateShort({ type: 'light' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '提交没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const referenceImage = lookImage(plan?.generated_image_url || plan?.image_url)

  if (doneMessage) {
    return (
      <View className={pageClass}>
        <AppHeader title="实际反馈" back />
        <View className="fb-success fade-up">
          <View className="fb-success__ring">
            <Text className="fb-success__mark">✓</Text>
          </View>
          <Text className="fb-success__title">第一次闭环完成</Text>
          <Text className="fb-success__message">{doneMessage}</Text>
          <View className="fb-success__action">
            <PrimaryButton
              text="回到我的方案"
              onClick={() => Taro.switchTab({ url: '/pages/plans/index' })}
            />
          </View>
        </View>
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title="实际反馈" back />
      <View className="fb">
        <View className="fb__hero fade-up">
          <View className="fb__shot pressable" onClick={choosePhoto}>
            {photoPath ? (
              <Image className="fb__shot-img" src={photoPath} mode="aspectFill" />
            ) : (
              <View className="fb__shot-empty">
                <Text className="fb__shot-plus">＋</Text>
                <Text className="fb__shot-hint">上传今天的实拍</Text>
              </View>
            )}
          </View>
          <View className="fb__arrow">→</View>
          <View className="fb__ref">
            {referenceImage ? (
              <Image className="fb__shot-img" src={referenceImage} mode="aspectFill" />
            ) : (
              <View className="fb__shot-empty">
                <Text className="fb__shot-hint">方案参考图暂不可用</Text>
              </View>
            )}
            <Text className="fb__ref-mark">方案</Text>
          </View>
        </View>

        <Text className="fb__lede fade-up delay-1">你的感受比 AI 判断更重要</Text>

        <View className="fb__words fade-up delay-2">
          {FEEDBACK_WORDS.map((word) => (
            <Pill key={word} label={word} active={selected.includes(word)} onClick={() => toggleWord(word)} />
          ))}
        </View>

        <View className="fb__foot fade-up delay-3">
          <PrimaryButton text="提交反馈" loading={busy} disabled={!photoPath} onClick={submit} />
        </View>
      </View>
    </View>
  )
}
