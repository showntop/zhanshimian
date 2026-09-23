import type { ReactNode } from 'react'
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { APP_NAME } from '@zsm/core'
import { colors, radius, space } from './theme'

export function Screen({
  title,
  children,
  onBack,
  scroll = true,
}: {
  title?: string
  children: ReactNode
  onBack?: () => void
  scroll?: boolean
}) {
  const body = scroll ? <ScrollView contentContainerStyle={styles.body}>{children}</ScrollView> : <View style={styles.body}>{children}</View>
  return (
    <SafeAreaView style={styles.safe} edges={['top', 'left', 'right']}>
      <View style={styles.header}>
        {onBack ? (
          <Pressable onPress={onBack} hitSlop={12}>
            <Text style={styles.back}>返回</Text>
          </Pressable>
        ) : (
          <Text style={styles.brand}>{APP_NAME}</Text>
        )}
        {title ? <Text style={styles.title}>{title}</Text> : <View />}
        <View style={styles.spacer} />
      </View>
      {body}
    </SafeAreaView>
  )
}

export function PrimaryButton({
  text,
  onPress,
  loading,
  disabled,
}: {
  text: string
  onPress: () => void
  loading?: boolean
  disabled?: boolean
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled || loading}
      style={({ pressed }) => [
        styles.btn,
        (disabled || loading) && styles.btnOff,
        pressed && !disabled && styles.btnPressed,
      ]}
    >
      {loading ? <ActivityIndicator color="#fff" /> : <Text style={styles.btnText}>{text}</Text>}
    </Pressable>
  )
}

export function ErrorBlock({ message, onRetry, retryText = '重试' }: { message?: string; onRetry: () => void; retryText?: string }) {
  return (
    <View style={styles.state}>
      <Text style={styles.stateTitle}>{message || '刚才没有加载成功'}</Text>
      <PrimaryButton text={retryText} onPress={onRetry} />
    </View>
  )
}

export function EmptyBlock({ title, description, actionText, onAction }: { title: string; description?: string; actionText?: string; onAction?: () => void }) {
  return (
    <View style={styles.state}>
      <Text style={styles.stateTitle}>{title}</Text>
      {description ? <Text style={styles.stateDesc}>{description}</Text> : null}
      {actionText && onAction ? <PrimaryButton text={actionText} onPress={onAction} /> : null}
    </View>
  )
}

export function Pill({ label, active, onPress }: { label: string; active?: boolean; onPress: () => void }) {
  return (
    <Pressable onPress={onPress} style={[styles.pill, active && styles.pillOn]}>
      <Text style={[styles.pillText, active && styles.pillTextOn]}>{label}</Text>
    </Pressable>
  )
}

const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bg },
  header: {
    minHeight: 48,
    paddingHorizontal: space.lg,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  brand: { fontSize: 16, fontWeight: '700', color: colors.ink },
  back: { fontSize: 15, color: colors.moss },
  title: { fontSize: 16, fontWeight: '600', color: colors.ink },
  spacer: { width: 36 },
  body: { paddingHorizontal: space.lg, paddingBottom: 40, gap: space.lg },
  btn: {
    minHeight: 48,
    borderRadius: radius.md,
    backgroundColor: colors.moss,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.xl,
  },
  btnOff: { opacity: 0.45 },
  btnPressed: { opacity: 0.88, transform: [{ scale: 0.985 }] },
  btnText: { color: '#fff', fontSize: 16, fontWeight: '600' },
  state: { gap: space.md, paddingVertical: space.xxxl },
  stateTitle: { fontSize: 16, fontWeight: '600', color: colors.ink },
  stateDesc: { fontSize: 14, lineHeight: 22, color: colors.ink2 },
  pill: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceStrong,
    borderWidth: 1,
    borderColor: colors.line,
  },
  pillOn: { backgroundColor: colors.mossSoft, borderColor: colors.moss },
  pillText: { color: colors.ink2, fontSize: 14 },
  pillTextOn: { color: colors.moss, fontWeight: '600' },
})
