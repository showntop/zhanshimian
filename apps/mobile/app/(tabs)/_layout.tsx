import { Tabs } from 'expo-router'
import { colors } from '../../src/ui/theme'

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.moss,
        tabBarInactiveTintColor: colors.ink3,
        tabBarStyle: { backgroundColor: colors.bg, borderTopColor: colors.line },
      }}
    >
      <Tabs.Screen name="index" options={{ title: '首页' }} />
      <Tabs.Screen name="plans" options={{ title: '方案' }} />
      <Tabs.Screen name="profile" options={{ title: '我的' }} />
    </Tabs>
  )
}
