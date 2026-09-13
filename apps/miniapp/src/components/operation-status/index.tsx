// 公开 Operation 的通用状态条：working 显示服务端文案，failed 显示公开错误
// 与请求编号并给「重试」，ended 说明取消/被取代。succeeded/idle 不渲染——
// 终态去向（跳报告/跳方案）是页面的职责，状态条只负责"还在路上"和"没走成"。
import { Text, View } from '@tarojs/components'
import { ASSESSMENT_COPY, ERROR_COPY } from '@zsm/core'
import { assessmentEndedBody } from '../../features/assessment/model'
import type { OperationView } from '../../app/operations/operation-view'
import './index.scss'

interface OperationStatusProps {
  view: OperationView
  /** failed 且可重试时给出恢复动作 */
  onRetry?: () => void
  className?: string
}

export default function OperationStatus({ view, onRetry, className = '' }: OperationStatusProps) {
  if (view.kind === 'idle' || view.kind === 'succeeded') return null

  if (view.kind === 'working') {
    return (
      <View className={`operation-status operation-status--working ${className}`}>
        <View className="spinner operation-status__spin" />
        <Text className="operation-status__text">{view.message || ASSESSMENT_COPY.stageFallback}</Text>
        {view.retrying ? (
          <Text className="operation-status__note">{ASSESSMENT_COPY.retryingNote}</Text>
        ) : null}
      </View>
    )
  }

  if (view.kind === 'ended') {
    return (
      <View className={`operation-status operation-status--ended ${className}`}>
        <Text className="operation-status__text">{ASSESSMENT_COPY.endedTitle}</Text>
        <Text className="operation-status__note">{assessmentEndedBody(view.reason)}</Text>
      </View>
    )
  }

  return (
    <View className={`operation-status operation-status--failed ${className}`}>
      <Text className="operation-status__text">{view.message || ERROR_COPY.server}</Text>
      {view.requestId ? (
        <Text className="operation-status__note">
          {`${ASSESSMENT_COPY.requestIdLabel} ${view.requestId}`}
        </Text>
      ) : null}
      {onRetry ? (
        <Text className="operation-status__retry pressable" onClick={onRetry}>
          {ERROR_COPY.retryAction}
        </Text>
      ) : null}
    </View>
  )
}
