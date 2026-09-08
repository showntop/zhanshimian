// 分析进度页：轮询分析行(700ms) + 显示进度补间(只追不跳) + 扫描线动效。
// 失败态：照片被拒时逐图展示中文原因（error_message 按分号拆分为逐图原因）。
import { useEffect, useRef, useState } from 'react'
import Taro, { useDidHide, useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { POLL_INTERVALS, useDisplayProgress, useTaskPolling, type Analysis } from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import './index.scss'

function failureReasons(analysis: Analysis): string[] {
  const message = analysis.error_message || ''
  if (!message) return ['请按拍摄指引重新提交']
  // 后端照片拒绝文案形如「正脸照片：…；侧脸照片：…」
  return message
    .split(/[；;]/)
    .map((part) => part.trim())
    .filter(Boolean)
}

export default function Analysis() {
  const [analysisId, setAnalysisId] = useState('')
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [failed, setFailed] = useState<Analysis | null>(null)
  const display = useDisplayProgress()
  const failedRef = useRef(false)

  useLoad((options) => {
    const id = options?.id || readStorage(STORAGE_KEYS.activeTaskAnalysis)
    if (id) {
      setAnalysisId(id)
      writeStorage(STORAGE_KEYS.activeTaskAnalysis, id)
    }
  })

  const { stop } = useTaskPolling({
    fetcher: async () => {
      if (!analysisId) throw new Error('missing id')
      const item = await api.getAnalysis(analysisId)
      // 适配统一任务形状：分析行的 status/progress/stage 即任务投影
      return {
        id: item.id,
        type: 'analysis' as const,
        status: item.status,
        progress: item.progress,
        stage: item.stage,
        created_at: item.created_at,
        updated_at: item.updated_at,
      }
    },
    intervalMs: POLL_INTERVALS.analysis,
    enabled: Boolean(analysisId),
    onDone: (result) => {
      display.set(result.progress)
      if (result.status === 'completed') {
        if (failedRef.current) return
        failedRef.current = true
        writeStorage(STORAGE_KEYS.activeTaskAnalysis, '')
        Taro.redirectTo({ url: '/pages/report/index' })
      } else {
        // 终态 failed：拉全量分析行取逐图原因
        api
          .getAnalysis(analysisId)
          .then(setFailed)
          .catch(() => setFailed({ ...(analysis ?? ({} as Analysis)), status: 'failed' } as Analysis))
      }
    },
  })

  useDidHide(() => stop())

  useEffect(() => {
    if (analysis) display.set(analysis.progress)
  }, [analysis, display])

  const shown = display.get()
  const stageText = analysis?.stage || '正在安全上传照片'

  if (failed) {
    const reasons = failureReasons(failed)
    return (
      <View className="page">
        <AppHeader title="正在分析" back />
        <View className="analysis-fail fade-up">
          <Text className="analysis-fail__title">照片没有通过检查</Text>
          {reasons.map((reason) => (
            <Text key={reason} className="analysis-fail__reason">
              {reason}
            </Text>
          ))}
          <View className="analysis-fail__action">
            <PrimaryButton
              text="重新拍摄"
              onClick={() => Taro.redirectTo({ url: '/pages/capture/index' })}
            />
          </View>
        </View>
      </View>
    )
  }

  return (
    <View className="page">
      <AppHeader title="正在分析" back />
      <View className="analysis">
        <View className="analysis__portrait fade-up">
          <View className="analysis__portrait-frame">
            <View className="analysis__scan" />
            <View className="analysis__focus analysis__focus--1" />
            <View className="analysis__focus analysis__focus--2" />
            <View className="analysis__focus analysis__focus--3" />
            <Text className="analysis__portrait-mark">AI 分析中</Text>
          </View>
        </View>

        <Text className="analysis__stage fade-up delay-1">{stageText}</Text>

        <View className="analysis__progress fade-up delay-2">
          <View className="analysis__progress-track">
            <View className="analysis__progress-fill" style={{ width: `${shown}%` }} />
          </View>
          <Text className="analysis__progress-num">{Math.round(shown)}%</Text>
        </View>

        <View className="analysis__tips fade-up delay-3">
          <Text className="analysis__tip">正在读取面部与头肩比例</Text>
          <Text className="analysis__tip">随后匹配场景与预算</Text>
          <Text className="analysis__tip">三套方案将会准备好</Text>
        </View>
      </View>
    </View>
  )
}
