import { useEffect, useState } from 'react'
import { Stack, useRouter, useSegments } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { GestureHandlerRootView } from 'react-native-gesture-handler'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { hydrateStorage, readStorage, STORAGE_KEYS } from '../src/storage'

export default function RootLayout() {
  const [ready, setReady] = useState(false)
  const router = useRouter()
  const segments = useSegments()

  useEffect(() => {
    hydrateStorage().then(() => setReady(true))
  }, [])

  useEffect(() => {
    if (!ready) return
    const token = readStorage(STORAGE_KEYS.token)
    const inLogin = segments[0] === 'login'
    if (!token && !inLogin) router.replace('/login')
    if (token && inLogin) router.replace('/(tabs)')
  }, [ready, router, segments])

  if (!ready) return null

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <SafeAreaProvider>
        <StatusBar style="dark" />
        <Stack screenOptions={{ headerShown: false, animation: 'fade' }}>
          <Stack.Screen name="login" />
          <Stack.Screen name="(tabs)" />
          <Stack.Screen name="capture" />
          <Stack.Screen name="profile-setup" />
          <Stack.Screen name="analysis" />
          <Stack.Screen name="report" />
          <Stack.Screen name="plan/[id]" />
          <Stack.Screen name="checklist" />
          <Stack.Screen name="feedback" />
          <Stack.Screen name="scene" />
          <Stack.Screen name="today" />
        </Stack>
      </SafeAreaProvider>
    </GestureHandlerRootView>
  )
}
