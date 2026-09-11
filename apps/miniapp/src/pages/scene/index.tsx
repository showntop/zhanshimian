// 场合 Brief：单页四问（约 30 秒），复用档案不重复收集照片。
// 提交 = PUT /v1/reports/{id}/plans（幂等创建-或-刷新该场景三方案组）。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { SCENES } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Pill from '../../components/pill'
import './index.scss'

interface Field {
  key: string
  label: string
  options: { value: string; label: string }[]
}

// 与服务端 scene_plans.go 字段字典一致
const FIELDS: Record<string, Field[]> = {
  interview: [
    { key: 'when', label: '什么时候需要', options: [{ value: 'today', label: '今天' }, { value: 'three-days', label: '3 天内' }, { value: 'week', label: '1 周后' }, { value: 'later', label: '还没确定' }] },
    { key: 'format', label: '面试形式', options: [{ value: 'onsite', label: '线下面试' }, { value: 'video', label: '视频面试' }, { value: 'final', label: '终面 / 见客户' }] },
    { key: 'preparation', label: '准备方式', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '补一件关键单品' }, { value: 'complete', label: '可完整准备' }] },
    { key: 'impression', label: '最想呈现', options: [{ value: 'energetic', label: '更有精神' }, { value: 'reliable', label: '更可信' }, { value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }] },
  ],
  wedding: [
    { key: 'role', label: '你的角色', options: [{ value: 'guest', label: '普通宾客' }, { value: 'bridal-party', label: '伴娘 / 伴郎' }, { value: 'family', label: '重要亲友' }, { value: 'speaker', label: '需要上台' }] },
    { key: 'timing', label: '婚礼时段', options: [{ value: 'lunch', label: '午间' }, { value: 'afternoon', label: '下午' }, { value: 'dinner', label: '晚宴' }, { value: 'unknown', label: '还没确定' }] },
    { key: 'dress-code', label: '婚礼风格', options: [{ value: 'relaxed', label: '轻松婚礼' }, { value: 'elegant', label: '得体优雅' }, { value: 'formal', label: '正式礼服' }] },
    { key: 'impression', label: '最想呈现', options: [{ value: 'energetic', label: '更有精神' }, { value: 'reliable', label: '更可信' }, { value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }] },
  ],
  date: [
    { key: 'activity', label: '约会活动', options: [{ value: 'coffee', label: '咖啡 / 散步' }, { value: 'meal', label: '正餐' }, { value: 'movie', label: '电影 / 展览' }, { value: 'outdoor', label: '户外' }] },
    { key: 'timing', label: '什么时候', options: [{ value: 'today', label: '今天' }, { value: 'three-days', label: '3 天内' }, { value: 'week', label: '1 周后' }] },
    { key: 'preparation', label: '准备方式', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '补一件关键单品' }] },
    { key: 'impression', label: '最想呈现', options: [{ value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }, { value: 'energetic', label: '更有精神' }] },
  ],
  daily: [
    { key: 'activity', label: '今天主要做', options: [{ value: 'commute', label: '上班' }, { value: 'wfh', label: '居家办公' }, { value: 'errand', label: '外出办事' }, { value: 'meetup', label: '朋友小聚' }] },
    { key: 'weather', label: '所处环境', options: [{ value: 'office', label: '室内为主' }, { value: 'mixed', label: '室内外都有' }, { value: 'outdoor', label: '户外为主' }] },
    { key: 'preparation', label: '准备方式', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '补一件关键单品' }] },
    { key: 'impression', label: '最想呈现', options: [{ value: 'natural', label: '更自然' }, { value: 'energetic', label: '更有精神' }, { value: 'reliable', label: '更可靠' }] },
  ],
  gathering: [
    { key: 'activity', label: '聚会类型', options: [{ value: 'friends', label: '朋友局' }, { value: 'dinner', label: '聚餐' }, { value: 'birthday', label: '生日 / 庆祝' }, { value: 'drinks', label: '酒会 / 酒吧' }] },
    { key: 'timing', label: '什么时候', options: [{ value: 'afternoon', label: '下午' }, { value: 'evening', label: '傍晚' }, { value: 'night', label: '晚上' }, { value: 'unknown', label: '还没确定' }] },
    { key: 'preparation', label: '准备方式', options: [{ value: 'closet', label: '只用现有衣橱' }, { value: 'key-piece', label: '补一件关键单品' }, { value: 'complete', label: '可完整准备' }] },
    { key: 'impression', label: '最想呈现', options: [{ value: 'natural', label: '更自然' }, { value: 'memorable', label: '有记忆点' }, { value: 'energetic', label: '更有精神' }] },
  ],
}

export default function Scene() {
  const [scene, setScene] = useState('interview')
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const pageClass = usePageClass(true)

  useLoad((options) => {
    if (options?.scene && options.scene in FIELDS) setScene(options.scene)
  })

  const fields = FIELDS[scene] ?? FIELDS.interview!
  const sceneMeta = SCENES.find((s) => s.id === scene)
  const answered = fields.filter((f) => answers[f.key]).length

  const submit = async () => {
    setBusy(true)
    try {
      let reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) {
        const current = await api.getCurrentReport()
        if (!current) {
          // 无档案：先建档（brief 存本地，analysis 完成后 plans 页取用）
          writeStorage(STORAGE_KEYS.scenePending, scene)
          Taro.navigateTo({ url: `/pages/capture/index?scene=${scene}` })
          return
        }
        reportId = current.id
        writeStorage(STORAGE_KEYS.reportId, reportId)
      }
      await api.upsertPlans(reportId, { scene, answers })
      writeStorage(STORAGE_KEYS.sceneBrief, JSON.stringify({ scene, answers }))
      Taro.switchTab({ url: '/pages/plans/index' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '生成没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <View className={pageClass}>
      <AppHeader title="场合需求" back />
      <View className="scene">
        <View className="scene__intro fade-up">
          <View className="scene__intro-head">
            <Text className="scene__title">{sceneMeta?.label ?? '场景'}</Text>
            <Text className="scene__badge">复用档案</Text>
          </View>
          <Text className="scene__lede">补充 {fields.length} 个选择，约 30 秒。不会重复索要照片和身体数据。</Text>
        </View>

        {fields.map((field, i) => (
          <View key={field.key} className={`scene__field fade-up delay-${Math.min(i + 1, 3)}`}>
            <Text className="scene__field-label">
              {field.label}
            </Text>
            <View className="scene__options">
              {field.options.map((opt) => (
                <Pill
                  key={opt.value}
                  label={opt.label}
                  active={answers[field.key] === opt.value}
                  onClick={() => setAnswers((prev) => ({ ...prev, [field.key]: opt.value }))}
                />
              ))}
            </View>
          </View>
        ))}

        <View className="scene__foot fade-up delay-3">
          <PrimaryButton
            text={`生成${sceneMeta?.label ?? ''}方案`}
            loading={busy}
            disabled={answered < fields.length}
            onClick={submit}
          />
          <Text className="scene__foot-note">
            {answered} / {fields.length} 已选择
          </Text>
        </View>
      </View>
    </View>
  )
}
