// 形象助手：会话恢复 + 快捷问 + 乐观发送（失败回滚）+ 动作应用。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Input, ScrollView, Text, View } from '@tarojs/components'
import { ADVISOR_COPY, ApiError, trackEvent, type AdvisorMessage } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import { handleBillingError } from '../../../../services/billing'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import './index.scss'

const SUGGESTIONS = ['明天面试怎么穿？', '只用现有衣橱搭一套', '想比今天更轻松一点', '今天下雨怎么调整？']

export default function Advisor() {
  const [messages, setMessages] = useState<AdvisorMessage[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [scrollKey, setScrollKey] = useState('')
  const { pageClass, enter } = usePageShell(loaded, 'page--advisor', 'advisor')

  const restore = useCallback(async () => {
    const remember = (items: AdvisorMessage[]) => {
      setMessages(items)
      const conversationId = items[0]?.conversation_id || ''
      writeStorage(STORAGE_KEYS.advisorConversationId, conversationId)
    }
    const localId = readStorage(STORAGE_KEYS.advisorConversationId)
    try {
      if (localId) {
        try {
          remember(await api.getAdvisorMessages(localId))
          return
        } catch (error) {
          if (!(error instanceof ApiError) || error.statusCode !== 404) throw error
          writeStorage(STORAGE_KEYS.advisorConversationId, '')
        }
      }
      remember(await api.getLatestAdvisorMessages())
    } catch {
      /* 网络失败保留本地会话，下次再拉 */
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    restore()
  }, [restore])

  useShowOnce(() => {
    restore()
  })

  const scrollToEnd = () => {
    setScrollKey('')
    setTimeout(() => setScrollKey('advisor-end'), 50)
  }

  const send = async (content: string) => {
    const text = content.trim()
    if (!text || busy) return
    setBusy(true)
    const localId = `local-${Date.now()}`
    setMessages((prev) => [
      ...prev,
      { id: localId, conversation_id: '', role: 'user', content: text, actions: [], created_at: new Date().toISOString() },
    ])
    setInput('')
    scrollToEnd()
    try {
      const conversationId = readStorage(STORAGE_KEYS.advisorConversationId) || undefined
      const reply = await api.sendAdvisorMessage({
        conversation_id: conversationId,
        content: text,
        report_id: readStorage(STORAGE_KEYS.reportId) || undefined,
      })
      writeStorage(STORAGE_KEYS.advisorConversationId, reply.conversation_id)
      // 接口只回助手一条；本地用户气泡保留，只补上 conversation_id
      setMessages((prev) =>
        prev
          .map((m) => (m.id === localId ? { ...m, conversation_id: reply.conversation_id } : m))
          .concat(reply),
      )
      trackEvent('advisor_message_send', {})
      scrollToEnd()
    } catch (e) {
      setMessages((prev) => prev.filter((m) => m.id !== localId))
      setInput(text)
      if (handleBillingError(e)) return
      Taro.showToast({ title: (e as Error).message || '发送没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const applyAction = async (message: AdvisorMessage, actionId: string) => {
    try {
      const applied = await api.applyAdvisorAction(actionId)
      setMessages((prev) =>
        prev.map((m) =>
          m.id === message.id
            ? { ...m, actions: (m.actions ?? []).map((a) => (a.id === actionId ? applied : a)) }
            : m,
        ),
      )
      trackEvent('advisor_action_apply', {})
      Taro.showToast({ title: '已应用', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '应用没有成功', icon: 'none' })
    }
  }

  return (
    <View className={pageClass}>
      <AppHeader title={ADVISOR_COPY.title} back />
      <View className="adv">
        <ScrollView className="adv__list" scrollY scrollIntoView={scrollKey || undefined} enhanced showScrollbar={false}>
          {!loaded ? null : messages.length === 0 ? (
            <View className={`adv__empty ${enter()}`}>
              <Text className="adv__empty-title">{ADVISOR_COPY.emptyTitle}</Text>
              <Text className="adv__empty-desc">{ADVISOR_COPY.emptyDesc}</Text>
            </View>
          ) : (
            messages.map((message) => (
              <View key={message.id} className={`adv__msg adv__msg--${message.role}`}>
                <Text className="adv__bubble">{message.content}</Text>
                {(message.actions ?? []).length > 0 ? (
                  <View className="adv__actions">
                    {(message.actions ?? []).map((action) => (
                      <Text
                        key={action.id}
                        className={`adv__action ${action.applied ? 'adv__action--done' : ''} pressable`}
                        onClick={() => !action.applied && applyAction(message, action.id)}
                      >
                        {action.applied ? '已应用' : action.label}
                      </Text>
                    ))}
                  </View>
                ) : null}
              </View>
            ))
          )}
          {busy ? (
            <View className="adv__msg adv__msg--assistant">
              <View className="adv__typing">
                <View className="adv__typing-dot" />
                <View className="adv__typing-dot" />
                <View className="adv__typing-dot" />
              </View>
            </View>
          ) : null}
          <View id="advisor-end" />
        </ScrollView>

        <View className="adv__dock">
          <View className="adv__suggests">
            <ScrollView scrollX enhanced showScrollbar={false}>
              <View className="adv__suggest-row">
                {SUGGESTIONS.map((q) => (
                  <Text key={q} className="adv__suggest pressable" onClick={() => send(q)}>
                    {q}
                  </Text>
                ))}
              </View>
            </ScrollView>
          </View>
          <View className="adv__inputbar">
            <Input
              className="adv__input"
              placeholder={ADVISOR_COPY.placeholder}
              value={input}
              confirmType="send"
              adjustPosition
              onInput={(e: { detail: { value: string } }) => setInput(e.detail.value)}
              onConfirm={() => send(input)}
            />
            <View className={`adv__send ${input && !busy ? '' : 'adv__send--off'}`} onClick={() => send(input)}>
              <Text>{ADVISOR_COPY.send}</Text>
            </View>
          </View>
        </View>
      </View>
    </View>
  )
}
